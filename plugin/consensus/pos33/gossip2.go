package pos33

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"sync"
	"time"

	ccrypto "github.com/33cn/chain33/common/crypto"
	"github.com/33cn/chain33/types"
	pt "github.com/yccproject/ycc/plugin/dapp/pos33/types"

	"github.com/libp2p/go-libp2p"
	autonat "github.com/libp2p/go-libp2p-autonat"
	circuit "github.com/libp2p/go-libp2p-circuit"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	protocol "github.com/libp2p/go-libp2p-core/protocol"
	discovery "github.com/libp2p/go-libp2p-discovery"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	routing "github.com/libp2p/go-libp2p-core/routing"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"

	"github.com/multiformats/go-multiaddr"
)

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
	bootPeers []string

	mu         sync.Mutex
	streams    map[peer.ID]stream
	incoming   chan *pt.Pos33Msg
	outgoing   chan *smsg
	raddrPid   string
	peersTopic string
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

		g.h.Peerstore().AddAddrs(targetInfo.ID, targetInfo.Addrs, peerstore.AddressTTL)
		err = g.h.Connect(context.Background(), *targetInfo)
		if err != nil {
			plog.Error("bootstrap error", "err", err)
			continue
		}
		plog.Info("connect boot peer", "bootpeer", targetAddr.String())
		s, err := g.h.NewStream(context.Background(), targetInfo.ID, protocol.ID(g.raddrPid))
		if err != nil {
			plog.Error("bootstrap error", "err", err)
			continue
		}
		s.Write([]byte(g.h.ID()))
		s.Close()
	}
	return nil
}

type stream WriteCloser

const defaultMaxSize = 1024 * 1024 * 128

func newGossip2(priv ccrypto.PrivKey, port int, ns string, fs []string, forwardPeers bool, topics ...string) *gossip2 {
	ctx := context.Background()
	pr, err := crypto.UnmarshalSecp256k1PrivateKey(priv.Bytes())
	if err != nil {
		panic(err)
	}
	h := newHost(ctx, pr, port, ns)
	ps, err := pubsub.NewGossipSub(
		ctx,
		h,
		// pubsub.WithPeerOutboundQueueSize(128),
		pubsub.WithMaxMessageSize(pubsub.DefaultMaxMessageSize*10),
		pubsub.WithMessageSigning(false),
		pubsub.WithStrictSignatureVerification(false),
		pubsub.WithFloodPublish(true),
	)
	if err != nil {
		panic(err)
	}

	g := &gossip2{
		h:          h,
		tmap:       make(map[string]*pubsub.Topic),
		streams:    make(map[peer.ID]stream),
		incoming:   make(chan *pt.Pos33Msg, 16),
		outgoing:   make(chan *smsg, 16),
		C:          make(chan []byte, 1024),
		raddrPid:   ns + "/" + remoteAddrID,
		peersTopic: ns + "-" + pos33Peerstore,
	}
	g.setHandler()
	topics = append(topics, g.peersTopic)
	go g.run(ps, topics, fs, forwardPeers)
	return g
}

func (g *gossip2) setHandler() {
	h := g.h
	h.SetStreamHandler(protocol.ID(g.raddrPid), func(s network.Stream) {
		maddr := s.Conn().RemoteMultiaddr()
		pid := s.Conn().RemotePeer()
		plog.Info("remote peer", "peer", pid, "addr", maddr)
		h.Peerstore().AddAddrs(pid, []multiaddr.Multiaddr{maddr}, peerstore.AddressTTL)
	})

	h.SetStreamHandler(pos33MsgID, g.handleIncoming)
}

func (g *gossip2) handlePeers(data []byte) {
	var ais []peer.AddrInfo
	err := json.Unmarshal(data, &ais)
	if err != nil {
		plog.Error("pid unmarshal error", "err", err)
		return
	}
	for _, ai := range ais {
		if ai.ID != g.h.ID() {
			plog.Info("add remote peer", "addr", ai.String())
			g.h.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.AddressTTL)
			err = g.h.Connect(context.Background(), ai)
			if err != nil {
				plog.Error("connect error", "err", err)
			}
		}
	}
}

func (g *gossip2) run(ps *pubsub.PubSub, topics, fs []string, forwardPeers bool) {
	for _, t := range topics {
		t := t
		tp, err := ps.Join(t)
		if err != nil {
			panic(err)
		}
		g.tmap[t] = tp
		sb, err := tp.Subscribe()
		if err != nil {
			panic(err)
		}
		go func(s *pubsub.Subscription) {
			for {
				m, err := s.Next(context.Background())
				if err != nil {
					panic(err)
				}
				if g.h.ID() == m.ReceivedFrom {
					continue
				}
				if t == g.peersTopic {
					go g.handlePeers(m.Data)
				} else {
					g.C <- m.Data
				}
			}
		}(sb)
	}
	go g.handleOutgoing()
	go func() {
		for range time.NewTicker(time.Second * 60).C {
			np := ps.ListPeers(topics[0])
			plog.Info("pos33 peers ", "len", len(np), "peers", np)
			if len(np) < 3 {
				g.bootstrap(g.bootPeers...)
			}
		}
	}()
	if forwardPeers {
		go g.sendPeerstore(g.h)
	}
}

func (g *gossip2) gossip(topic string, data []byte) error {
	t, ok := g.tmap[topic]
	if !ok {
		return fmt.Errorf("%s topic NOT match", topic)
	}
	return t.Publish(context.Background(), data)
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

func (g *gossip2) newStream(pid peer.ID) (stream, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	st, ok := g.streams[pid]
	if !ok {
		s, err := g.h.NewStream(context.Background(), pid, pos33MsgID)
		if err != nil {
			plog.Error("newStream error", "err", err)
			return nil, err
		}
		w := NewDelimitedWriter(s)
		g.streams[pid] = w
		st = w
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
	r := NewDelimitedReader(s, defaultMaxSize)
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
		go func(msg *smsg) {
			s, err := g.newStream(msg.pid)
			if err != nil {
				plog.Error("new stream error", "err", err)
				return
			}
			err = s.WriteMsg(msg.msg)
			if err != nil {
				plog.Error("write msg error", "err", err)
				if err != nil {
					s.Close()
				}
				g.mu.Lock()
				delete(g.streams, msg.pid)
				g.mu.Unlock()
			}
		}(m)
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
		libp2p.EnableRelay(circuit.OptHop),
	)

	if err != nil {
		panic(err)
	}

	addrInfo := peer.AddrInfo{
		ID:    h.ID(),
		Addrs: h.Addrs(),
	}
	paddr, err := peer.AddrInfoToP2pAddrs(&addrInfo)
	if err != nil {
		panic(err)
	}
	err = ioutil.WriteFile(peerAddrFile, []byte(paddr[0].String()+"\n"), 0644)
	if err != nil {
		panic(err)
	}
	plog.Info("host inited", "host", paddr)

	discover(ctx, h, idht, ns)

	return h
}

func (g *gossip2) sendPeerstore(h host.Host) {
	for range time.NewTicker(time.Second * 60).C {
		peers := h.Peerstore().PeersWithAddrs()
		var ais []*peer.AddrInfo
		for _, id := range peers {
			maddr := h.Peerstore().Addrs(id)
			ai := &peer.AddrInfo{Addrs: maddr, ID: id}
			ais = append(ais, ai)
			plog.Info("peer:", "pid", id.String()[:16], "addr", maddr)
		}
		data, err := json.Marshal(ais)
		if err != nil {
			plog.Error("pid marshal error", "err", err)
			return
		}
		g.gossip(g.peersTopic, data)
	}
}

func printPeerstore(h host.Host) {
	for range time.NewTicker(time.Second * 60).C {
		peers := h.Peerstore().PeersWithAddrs()
		plog.Info("peersstore len", "len", peers.Len(), "pids", peers)
		for _, id := range peers {
			plog.Info("peer:", "pid", id.String()[:16], "addr", h.Peerstore().Addrs(id))
		}
	}
}

func discover(ctx context.Context, h host.Host, idht *dht.IpfsDHT, ns string) {
	_, err := autonat.New(ctx, h)
	if err != nil {
		panic(err)
	}
	mdns := mdns.NewMdnsService(h, ns)
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

	peerChan, err := routingDiscovery.FindPeers(ctx, ns)
	if err != nil {
		panic(err)
	}

	go func() {
		host := h
		for peer := range peerChan {
			if peer.ID == host.ID() {
				continue
			}

			stream, err := host.NewStream(ctx, peer.ID, protocol.ID(ns+"/"+remoteAddrID))
			if err != nil {
				plog.Error("NewStream error:", "err", err)
				return
			}

			time.AfterFunc(time.Second*3, func() { stream.Close() })
			plog.Info("Connected to:", "peer", peer)
		}
	}()
}
