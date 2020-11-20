package pos33

import (
	"context"
	"fmt"
	"io/ioutil"
	"time"

	ccrypto "github.com/33cn/chain33/common/crypto"

	"github.com/libp2p/go-libp2p"
	autonat "github.com/libp2p/go-libp2p-autonat"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	discovery "github.com/libp2p/go-libp2p-discovery"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	peerstore "github.com/libp2p/go-libp2p-peerstore"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	routing "github.com/libp2p/go-libp2p-routing"
	disc "github.com/libp2p/go-libp2p/p2p/discovery"
	"github.com/multiformats/go-multiaddr"
)

// var _ = libp2pquic.NewTransport

type mdnsNotifee struct {
	h   host.Host
	ctx context.Context
}

func (m *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if m.h.Network().Connectedness(pi.ID) != network.Connected {
		plog.Info("mdns peer found", "pid", pi.ID.String())
		m.h.Connect(m.ctx, pi)
	}
}

type gossip2 struct {
	C         chan []byte
	h         host.Host
	tmap      map[string]*pubsub.Topic
	ctx       context.Context
	bootPeers []string
}

const remoteAddrID = "yccxaddr"
const sendtoID = "yccxsendto"

func (g *gossip2) bootstrap(addrs ...string) error {
	g.bootPeers = addrs
	for _, addr := range addrs {
		targetAddr, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			plog.Error("bootstrap error", "err", err)
			return err
		}

		targetInfo, err := peer.AddrInfoFromP2pAddr(targetAddr)
		if err != nil {
			plog.Error("bootstrap error", "err", err)
			return err
		}

		g.h.Peerstore().AddAddrs(targetInfo.ID, targetInfo.Addrs, peerstore.PermanentAddrTTL)
		err = g.h.Connect(g.ctx, *targetInfo)
		if err != nil {
			plog.Error("bootstrap error", "err", err)
			return err
		}
		plog.Info("connect boot peer", "bootpeer", targetAddr.String())
		s, err := g.h.NewStream(g.ctx, targetInfo.ID, remoteAddrID)
		if err != nil {
			plog.Error("bootstrap error", "err", err)
			return err
		}
		s.Write([]byte(g.h.ID()))
		s.Close()
	}
	return nil
}

func newGossip2(priv ccrypto.PrivKey, port int, ns string, topics ...string) *gossip2 {
	ctx := context.Background()
	pr, err := crypto.UnmarshalSecp256k1PrivateKey(priv.Bytes())
	if err != nil {
		panic(err)
	}
	h := newHost(ctx, pr, port, ns)
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		panic(err)
	}
	g := &gossip2{C: make(chan []byte, 16), h: h, tmap: make(map[string]*pubsub.Topic), ctx: ctx}
	g.setHandler()
	go g.run(ps, topics)
	return g
}

func (g *gossip2) setHandler() {
	h := g.h
	h.SetStreamHandler(remoteAddrID, func(s network.Stream) {
		maddr := s.Conn().RemoteMultiaddr()
		pid := s.Conn().RemotePeer()
		plog.Info("remote peer", "peer", pid, "addr", maddr)
		h.Peerstore().AddAddrs(pid, []multiaddr.Multiaddr{maddr}, peerstore.PermanentAddrTTL)
		s.Close()
	})

	h.SetStreamHandler(sendtoID, func(s network.Stream) {
		data, err := ioutil.ReadAll(s)
		if err != nil {
			plog.Error("recv remote error", "err", err)
			return
		}
		plog.Info("recv from remote peer", "protocolID", sendtoID, "remote peer", s.Conn().RemotePeer())
		g.C <- data
	})
}

func (g *gossip2) run(ps *pubsub.PubSub, topics []string) {
	for _, t := range topics {
		tp, err := ps.Join(t)
		if err != nil {
			panic(err)
		}
		g.tmap[t] = tp
		sb, err := tp.Subscribe()
		go func(s *pubsub.Subscription) {
			for {
				m, err := s.Next(g.ctx)
				if err != nil {
					panic(err)
				}
				if g.h.ID() == m.ReceivedFrom {
					continue
				}
				g.C <- m.Data
			}
		}(sb)
	}
	go func() {
		for range time.NewTicker(time.Minute).C {
			np := ps.ListPeers(topics[0])
			plog.Info("pos33 peers ", "len", len(np), "peers", np)
			if len(np) < 3 {
				g.bootstrap(g.bootPeers...)
			}
		}
	}()
}

func (g *gossip2) gossip(topic string, data []byte) error {
	t, ok := g.tmap[topic]
	if !ok {
		return fmt.Errorf("%s topic NOT match", topic)
	}
	return t.Publish(g.ctx, data)
}

func (g *gossip2) sendto(pub, data []byte) error {
	p, err := crypto.UnmarshalSecp256k1PublicKey(pub)
	if err != nil {
		plog.Error("sendto error", "err", err)
		return err
	}

	pid, err := peer.IDFromPublicKey(p)
	if err != nil {
		plog.Error("sendto error", "err", err)
		return err
	}

	s, err := g.h.NewStream(g.ctx, pid, sendtoID)
	if err != nil {
		plog.Error("sendto error", "err", err)
		return err
	}
	defer s.Close()
	l := len(data)
	for {
		n, err := s.Write(data)
		if err != nil {
			plog.Error("sendto error", "err", err)
			return err
		}
		l -= n
		if l == 0 {
			break
		}
		data = data[n:]
	}
	return nil
}

func newHost(ctx context.Context, priv crypto.PrivKey, port int, ns string) host.Host {
	var idht *dht.IpfsDHT
	h, err := libp2p.New(ctx,
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port), // regular tcp connections
		),
		libp2p.EnableNATService(),
		libp2p.DefaultTransports,
		libp2p.NATPortMap(),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			dht, err := dht.New(ctx, h)
			idht = dht
			return idht, err
		}),
		// libp2p.EnableRelay(relay.OptHop),
		libp2p.EnableAutoRelay(),
	)

	if err != nil {
		panic(err)
	}

	paddr := peerAddr(h)
	err = ioutil.WriteFile("yccpeeraddr.txt", []byte(paddr.String()+"\n"), 0644)
	if err != nil {
		panic(err)
	}

	discover(ctx, h, idht, ns)
	plog.Info("host inited", "host", paddr)

	// go printPeerstore(h)
	return h
}

func printPeerstore(h host.Host) {
	for range time.NewTicker(time.Second * 60).C {
		pids := h.Peerstore().PeersWithAddrs()
		plog.Debug("peersstore len", "len", pids.Len(), "pids", pids)
		for _, id := range pids {
			plog.Debug("peer:", "pid", id.String()[:16], "addr", h.Peerstore().Addrs(id))
		}
	}
}

func discover(ctx context.Context, h host.Host, idht *dht.IpfsDHT, ns string) {
	_, err := autonat.New(ctx, h)
	if err != nil {
		panic(err)
	}
	mdns, err := disc.NewMdnsService(ctx, h, time.Second*10, ns)
	if err != nil {
		panic(err)
	}

	mn := &mdnsNotifee{h: h, ctx: ctx}
	mdns.RegisterNotifee(mn)

	err = idht.Bootstrap(ctx)
	if err != nil {
		panic(err)
	}
	routingDiscovery := discovery.NewRoutingDiscovery(idht)
	discovery.Advertise(ctx, routingDiscovery, ns)
}

func peerAddr(h host.Host) multiaddr.Multiaddr {
	peerInfo := &peerstore.PeerInfo{
		ID:    h.ID(),
		Addrs: h.Addrs(),
	}
	addrs, err := peerstore.InfoToP2pAddrs(peerInfo)
	if err != nil {
		panic(err)
	}
	return addrs[0]
}
