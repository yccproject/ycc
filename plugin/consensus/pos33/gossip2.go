package pos33

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"time"

	ccrypto "github.com/33cn/chain33/common/crypto"
	"github.com/33cn/chain33/types"
	pt "github.com/yccproject/ycc/plugin/dapp/pos33/types"

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
	pio "github.com/libp2p/go-msgio/protoio"
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
	streams   map[peer.ID]*stream
	incoming  chan *pt.Pos33Msg
	outgoing  chan *smsg
}

const remoteAddrID = "ycc-pos33-addr"
const sendtoID = "ycc-pos33-sendto"
const pos33MsgID = "ycc-pos33-msg"

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

type stream struct {
	s  network.Stream
	w  *bufio.Writer
	wc pio.WriteCloser
}

const defaultMaxSize = 1024 * 1024

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

	g := &gossip2{
		ctx:      ctx,
		h:        h,
		tmap:     make(map[string]*pubsub.Topic),
		streams:  make(map[peer.ID]*stream),
		incoming: make(chan *pt.Pos33Msg, 16),
		outgoing: make(chan *smsg, 16),
		C:        make(chan []byte, 16),
	}
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

	h.SetStreamHandler(pos33MsgID, g.handleIncoming)
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
	go g.handleOutgoing()
	go func() {
		for range time.NewTicker(time.Minute).C {
			np := ps.ListPeers(topics[0])
			plog.Info("pos33 peers ", "len", len(np), "peers", np)
			g.bootstrap(g.bootPeers...)
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

func pub2pid(pub []byte) (peer.ID, error) {
	p, err := crypto.UnmarshalSecp256k1PublicKey(pub)
	if err != nil {
		plog.Error("pub2pid error", "err", err)
		return "", err
	}

	pid, err := peer.IDFromPublicKey(p)
	if err != nil {
		plog.Error("pub2pid2 error", "err", err)
		return "", err
	}
	return pid, nil
}

func (s *stream) writeMsg(msg types.Message) error {
	err := s.wc.WriteMsg(msg)
	if err != nil {
		return err
	}

	return s.w.Flush()
}

func (g *gossip2) newStream(pid peer.ID) (*stream, error) {
	st, ok := g.streams[pid]
	if !ok {
		s, err := g.h.NewStream(g.ctx, pid, pos33MsgID)
		if err != nil {
			return nil, err
		}
		w := bufio.NewWriter(s)
		st = &stream{s: s, w: w, wc: pio.NewDelimitedWriter(w)}
		g.streams[pid] = st
	}
	return st, nil
}

type smsg struct {
	pid peer.ID
	msg types.Message
}

func (g *gossip2) sendMsg(pub []byte, msg types.Message) error {
	pid, err := pub2pid(pub)
	if err != nil {
		return err
	}
	g.outgoing <- &smsg{pid, msg}
	return nil
}

func (g *gossip2) handleIncoming(s network.Stream) {
	r := pio.NewDelimitedReader(s, defaultMaxSize)
	for {
		m := new(pt.Pos33Msg)
		err := r.ReadMsg(m)
		if err != nil {
			if err != io.EOF {
				plog.Error("recv remote error", "err", err)
				s.Reset()
				return
			}
			s.Close()
			return
		}
		plog.Debug("recv from remote peer", "protocolID", s.Protocol(), "remote peer", s.Conn().RemotePeer())
		g.incoming <- m
	}
}

func (g *gossip2) handleOutgoing() {
	for {
		m := <-g.outgoing
		s, err := g.newStream(m.pid)
		if err != nil {
			plog.Error("new stream error", "err", err)
			continue
		}
		err = s.writeMsg(m.msg)
		if err != nil {
			plog.Error("write msg error", "err", err)
			if err != io.EOF {
				s.s.Reset()
			} else {
				s.s.Close()
			}
			delete(g.streams, m.pid)
		}
	}
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
