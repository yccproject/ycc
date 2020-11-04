package pos33

import (
	"context"
	"fmt"
	"io/ioutil"
	"time"

	ccrypto "github.com/33cn/chain33/common/crypto"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	discovery "github.com/libp2p/go-libp2p-discovery"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	peerstore "github.com/libp2p/go-libp2p-peerstore"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	routing "github.com/libp2p/go-libp2p-routing"
	"github.com/multiformats/go-multiaddr"
)

type mdnsNotifee struct {
	h   host.Host
	ctx context.Context
}

func (m *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if m.h.Network().Connectedness(pi.ID) != network.Connected {
		plog.Debug("peer mdns found", "pid", pi.ID.String())
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

		err = g.h.Connect(g.ctx, *targetInfo)
		if err != nil {
			plog.Error("bootstrap error", "err", err)
			return err
		}
		plog.Debug("connect boot peer", "bootpeer", targetAddr.String())
	}
	return nil
}

func newGossip2(priv ccrypto.PrivKey, port int, ns string, topics ...string) *gossip2 {
	ctx := context.Background()
	pr, err := crypto.UnmarshalSecp256k1PrivateKey(priv.Bytes())
	if err != nil {
		panic(err)
	}
	h, idht := newHost(ctx, pr, port)
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		panic(err)
	}
	tmap := make(map[string]*pubsub.Topic)
	for _, t := range topics {
		topic, err := ps.Join(t)
		if err != nil {
			panic(err)
		}
		tmap[t] = topic
	}
	g := &gossip2{C: make(chan []byte, 16), h: h, tmap: tmap, ctx: ctx}
	go g.run(ps, topics, idht, ns)
	return g
}

func (g *gossip2) run(ps *pubsub.PubSub, topics []string, idht *dht.IpfsDHT, ns string) {
	smap := make(map[string]*pubsub.Subscription)
	for t, topic := range g.tmap {
		s, err := topic.Subscribe()
		if err != nil {
			panic(err)
		}
		smap[t] = s
	}
	for _, sb := range smap {
		go func(s *pubsub.Subscription) {
			for {
				m, err := s.Next(g.ctx)
				if err != nil {
					panic(err)
				}
				id, err := peer.IDFromBytes(m.From)
				if err != nil {
					panic(err)
				}

				if g.h.ID().String() == id.String() {
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
	discover(g.ctx, g.h, idht, ns)
}

func (g *gossip2) gossip(topic string, data []byte) error {
	t, ok := g.tmap[topic]
	if !ok {
		return fmt.Errorf("%s topic NOT match", topic)
	}
	return t.Publish(g.ctx, data)
}

func newHost(ctx context.Context, priv crypto.PrivKey, port int) (host.Host, *dht.IpfsDHT) {
	var idht *dht.IpfsDHT
	h, err := libp2p.New(ctx,
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port), // regular tcp connections
		),
		libp2p.DefaultTransports,
		libp2p.NATPortMap(),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			dht, err := dht.New(ctx, h)
			idht = dht
			return idht, err
		}),
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

	plog.Info("host inited", "host", paddr)
	return h, idht
}

func discover(ctx context.Context, h host.Host, idht *dht.IpfsDHT, ns string) {
	err := idht.Bootstrap(ctx)
	if err != nil {
		panic(err)
	}
	mn := &mdnsNotifee{h: h, ctx: ctx}
	routingDiscovery := discovery.NewRoutingDiscovery(idht)
	discovery.Advertise(ctx, routingDiscovery, ns)
	peerCh, err := routingDiscovery.FindPeers(ctx, ns)
	for peer := range peerCh {
		mn.HandlePeerFound(peer)
	}
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
