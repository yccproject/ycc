package pos33

import (
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/33cn/chain33/common"
	"github.com/33cn/chain33/common/address"
	"github.com/33cn/chain33/common/crypto"
	"github.com/33cn/chain33/common/log/log15"
	"github.com/33cn/chain33/types"
	"github.com/golang/protobuf/proto"
	pt "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

var plog = log15.New("module", "pos33")

const pos33Topic = "ycc-pos33"

type alterBlock struct {
	ok bool
	n  int
	bs []*types.Block
}
type voter struct {
	mymvss []*pt.Pos33SortMsg
	mybvss []*pt.Pos33SortMsg
	mss    map[string]*pt.Pos33SortMsg
	ab     *alterBlock
}

type maker struct {
	my     *pt.Pos33SortMsg
	mvss   map[string]*pt.Pos33SortMsg
	bvss   map[string]*pt.Pos33SortMsg
	mvs    map[string][]*pt.Pos33VoteMsg
	bvs    map[string][]*pt.Pos33VoteMsg
	ok     bool
	makeok bool
}

type node struct {
	*Client
	gss *gossip2

	vmp map[int64]map[int]*voter
	mmp map[int64]map[int]*maker
	bch chan *types.Block

	// for new block incoming to add
	// already make block height and round
	lheight int64
	lround  int

	maxSortHeight int64
	pid           string
}

func newNode(conf *subConfig) *node {
	return &node{
		mmp: make(map[int64]map[int]*maker),
		vmp: make(map[int64]map[int]*voter),
		bch: make(chan *types.Block, 16),
	}
}

func (n *node) lastBlock() *types.Block {
	b, err := n.RequestLastBlock()
	if err != nil {
		panic(err)
	}
	return b
}

func (n *node) minerTx(height int64, sm *pt.Pos33SortMsg, vs []*pt.Pos33VoteMsg, priv crypto.PrivKey) (*types.Transaction, error) {
	vsc := len(vs)
	if len(vs) > pt.Pos33RewardVotes {
		sort.Sort(pt.Votes(vs))
		vs = vs[:pt.Pos33RewardVotes]
	}

	act := &pt.Pos33TicketAction{
		Value: &pt.Pos33TicketAction_Miner{
			Miner: &pt.Pos33TicketMiner{
				Votes: vs,
				Sort:  sm,
			},
		},
		Ty: pt.Pos33TicketActionMiner,
	}

	cfg := n.GetAPI().GetConfig()
	tx, err := types.CreateFormatTx(cfg, "pos33", types.Encode(act))
	if err != nil {
		return nil, err
	}

	tx.Sign(types.SECP256K1, priv)
	plog.Debug("make a minerTx", "nvs", vsc, "height", height, "fee", tx.Fee, "from", tx.From())
	return tx, nil
}

func (n *node) blockDiff(lb *types.Block, w int) uint32 {
	powLimitBits := n.GetAPI().GetConfig().GetP(lb.Height).PowLimitBits
	return powLimitBits
}

func (n *node) makeBlock(height int64, round int, sort *pt.Pos33SortMsg, vs []*pt.Pos33VoteMsg) error {
	lb := n.lastBlock()
	if height != lb.Height+1 {
		return fmt.Errorf("makeBlock height error")
	}
	if n.lheight == height && n.lround == round {
		return fmt.Errorf("makeBlock already made error")
	}

	priv := n.getPriv()
	if priv == nil {
		panic("can't go here")
	}

	tx, err := n.minerTx(height, sort, vs, priv)
	if err != nil {
		return err
	}

	nb, err := n.newBlock(lb, []*Tx{tx}, height)
	if err != nil {
		return err
	}
	n.lheight = height
	n.lround = round

	nb.Difficulty = n.blockDiff(lb, len(vs))
	plog.Info("block make", "height", height, "round", round, "ntx", len(nb.Txs), "nvs", len(vs), "hash", common.HashHex(nb.Hash(n.GetAPI().GetConfig()))[:16])

	pub := priv.PubKey().Bytes()
	n.sendBlockToPeer(pub, nb)
	return nil
}
func (n *node) sendBlockToPeer(pub []byte, b *types.Block) {
	m := &pt.Pos33BlockMsg{B: b, Pid: n.pid}
	if n.conf.TrubleMaker {
		time.AfterFunc(time.Millisecond*3000, func() {
			n.handleBlockMsg(m, true)
		})
	} else {
		n.handleBlockMsg(m, true)
	}
	pm := &pt.Pos33Msg{Data: types.Encode(m), Ty: pt.Pos33Msg_B}
	n.gss.gossip(pos33Topic, types.Encode(pm))
}

func (n *node) addBlock(b *types.Block) {
	lastHeight := n.lastBlock().Height
	if b.Height != lastHeight {
		plog.Error("addBlock height error", "height", b.Height, "lastHeight", lastHeight)
		return
	}

	plog.Info("block add", "height", b.Height, "hash", common.ToHex(b.Hash(n.GetAPI().GetConfig()))[:16])
	if b.BlockTime-n.lastBlock().BlockTime < 1 {
		time.AfterFunc(time.Millisecond*500, func() {
			n.pushBlock(b)
		})
	} else {
		n.pushBlock(b)
	}
}

func (n *node) pushBlock(b *types.Block) {
	select {
	case n.bch <- b:
	default:
		<-n.bch
		n.bch <- b
	}
}

// delete old data
func (n *node) clear(height int64) {
	for h := range n.mmp {
		if h <= height-10 {
			delete(n.mmp, h)
		}
	}

	for h := range n.vmp {
		if h <= height-10 {
			delete(n.vmp, h)
		}
	}
}

func saddr(sig *types.Signature) string {
	if sig == nil {
		return ""
	}
	return address.PubKeyToAddress(sig.Pubkey).String()
}

func (n *node) prepareOK(height int64) bool {
	if height < 10 {
		return true
	}
	return n.IsCaughtUp() && /*n.allCount(height) > 0 &&*/ n.myCount() > 0
}

func (n *node) checkBlock(b, pb *types.Block) error {
	plog.Debug("checkBlock", "height", b.Height, "pbheight", pb.Height)
	if b.Height <= pb.Height {
		return fmt.Errorf("check block height error")
	}
	if !n.prepareOK(b.Height) {
		return nil
	}
	if len(b.Txs) == 0 {
		return fmt.Errorf("nil block error")
	}
	err := n.blockCheck(b)
	if err != nil {
		plog.Error("blockCheck error", "err", err, "height", b.Height)
		return err
	}
	return nil
}

func (n *node) blockCheck(b *types.Block) error {
	act, err := getMiner(b)
	if err != nil {
		return err
	}
	if act.Sort == nil || act.Sort.Proof == nil || act.Sort.Proof.Input == nil {
		return fmt.Errorf("miner tx error")
	}
	round := int(act.Sort.Proof.Input.Round)
	plog.Info("block check", "height", b.Height, "round", round, "from", b.Txs[0].From()[:16])

	sortHeight := b.Height - pt.Pos33SortBlocks
	seed, err := n.getSortSeed(sortHeight)
	if err != nil {
		return err
	}
	err = n.verifySort(b.Height, 0, seed, act.GetSort())
	if err != nil {
		return err
	}
	return nil
}

func getMinerSeed(b *types.Block) ([]byte, error) {
	seed := zeroHash[:]
	if b.Height > pt.Pos33SortBlocks {
		m, err := getMiner(b)
		if err != nil {
			return nil, err
		}
		sort := m.Sort
		seed = sort.SortHash.Hash
	}
	return seed, nil
}

var zeroHash [32]byte

func (n *node) reSortition(height int64, round int) bool {
	b, err := n.RequestBlock(height - pt.Pos33SortBlocks)
	if err != nil {
		plog.Debug("requestBlock error", "height", height-pt.Pos33SortBlocks, "error", err)
		return false
	}
	seed, err := getMinerSeed(b)
	if err != nil {
		plog.Error("reSortition error", "height", height, "round", round, "err", err)
		return false
	}
	n.sortMaker(seed, height, round)
	n.sortVoter(seed, height, round)
	return true
}

func (n *node) sortition(b *types.Block, round int) {
	seed, err := getMinerSeed(b)
	height := b.Height + pt.Pos33SortBlocks
	if err != nil {
		plog.Error("reSortition error", "height", height, "round", round, "err", err)
		return
	}
	n.sortMaker(seed, height, round)
	n.sortVoter(seed, height, round)
}

func (n *node) firstSortition() {
	seed := zeroHash[:]
	for i := 0; i < pt.Pos33SortBlocks; i++ {
		height := int64(i + 1)
		n.sortMaker(seed, height, 0)
		n.sortVoter(seed, height, 0)
	}
}

func (n *node) getMaker(height int64, round int) *maker {
	rmp, ok := n.mmp[height]
	if !ok {
		rmp = make(map[int]*maker)
		n.mmp[height] = rmp
	}
	m, ok := rmp[round]
	if !ok {
		m = &maker{
			bvs:  make(map[string][]*pt.Pos33VoteMsg),
			mvs:  make(map[string][]*pt.Pos33VoteMsg),
			mvss: make(map[string]*pt.Pos33SortMsg),
			bvss: make(map[string]*pt.Pos33SortMsg),
		}
		rmp[round] = m
	}
	return m
}

func (n *node) getVoter(height int64, round int) *voter {
	rmp, ok := n.vmp[height]
	if !ok {
		rmp = make(map[int]*voter)
		n.vmp[height] = rmp
	}
	v, ok := rmp[round]
	if !ok {
		v = &voter{
			mss: make(map[string]*pt.Pos33SortMsg),
			ab:  &alterBlock{},
		}
		rmp[round] = v
	}
	return v
}

func (n *node) sortVoter(seed []byte, height int64, round int) {
	n.sortBlockVoter(seed, height, round)
	n.sortMakerVoter(seed, height, round)
}

func (n *node) sortBlockVoter(seed []byte, height int64, round int) {
	ss := n.voterSort(seed, height, round, BlockVoter)
	v := n.getVoter(height, round)
	v.mybvss = ss
	n.sendVoterSort(ss, height, round, int(pt.Pos33Msg_BVS))
}

func (n *node) sortMakerVoter(seed []byte, height int64, round int) {
	ss := n.voterSort(seed, height, round, MakerVoter)
	v := n.getVoter(height, round)
	v.mymvss = ss
	n.sendVoterSort(ss, height, round, int(pt.Pos33Msg_MVS))
}

func (n *node) sortMaker(seed []byte, height int64, round int) {
	plog.Debug("sortition", "height", height, "round", round)
	if n.conf.OnlyVoter {
		return
	}
	s := n.makerSort(seed, height, round)
	if s == nil {
		return
	}
	m := n.getMaker(height, round)
	m.my = s
	n.sendMakerSort(m.my, height, round)
}

func (n *node) getSortSeed(height int64) ([]byte, error) {
	if height < pt.Pos33SortBlocks {
		return zeroHash[:], nil
	}
	sb, err := n.RequestBlock(height)
	if err != nil {
		plog.Debug("request block error", "height", height, "err", err)
		return nil, err
	}
	return getMinerSeed(sb)
}

func (n *node) getDiff(height int64, round int) float64 {
	height -= pt.Pos33SortBlocks
	w := n.allCount(height)
	diff := float64(pt.Pos33MakerSize) / float64(w)
	diff *= math.Pow(1.1, float64(round))
	return diff
}

func (n *node) handleVoterSort(m *pt.Pos33Sorts, myself bool, ty int) {
	ss := m.Sorts
	if len(ss) == 0 {
		return
	}
	s0 := ss[0]
	if s0.Proof == nil || s0.Proof.Input == nil || s0.SortHash == nil {
		plog.Error("handleSortitionMsg error, input msg is nil")
		return
	}

	height := s0.Proof.Input.Height
	round := int(s0.Proof.Input.Round)
	maker := n.getMaker(height, round)

	sty := BlockVoter
	if ty == int(pt.Pos33Msg_MVS) {
		sty = MakerVoter
	}

	plog.Info("handleVoterSort", "nvs", len(m.Sorts), "height", height, "round", round, "ty", ty)

	for _, s := range ss {
		if !myself {
			seed, err := n.getSortSeed(height - pt.Pos33SortBlocks)
			if err != nil {
				plog.Error("getSeed error", "err", err)
				return
			}
			err = n.checkSort(s, seed, sty)
			if err != nil {
				plog.Error("checkSort error", "err", err)
				return
			}
		}
		if ty == int(pt.Pos33Msg_MVS) {
			maker.mvss[string(s.SortHash.Hash)] = s
		} else if ty == int(pt.Pos33Msg_BVS) {
			maker.bvss[string(s.SortHash.Hash)] = s
		}
	}
}

func (n *node) handleVoteMsg(ms []*pt.Pos33VoteMsg, myself bool, ty int) {
	if len(ms) == 0 {
		return
	}
	m0 := ms[0]
	if m0.Sort == nil || m0.Sort.Proof == nil || m0.Sort.Proof.Input == nil {
		return
	}

	height := m0.Sort.Proof.Input.Height
	round := int(m0.Sort.Proof.Input.Round)

	maker := n.getMaker(height, round)
	if maker.ok {
		return
	}

	plog.Info("handleVoteMsg", "nvs", len(ms), "height", height, "round", round, "ty", ty)

	for _, m := range ms {
		if m.Sort == nil || m.Sort.Proof == nil || m.Sort.Proof.Input == nil {
			return
		}
		if m.Sort.Proof.Input.Height != height || int(m.Sort.Proof.Input.Round) != round {
			return
		}

		if !myself {
			if !m.Verify() {
				plog.Info("block voter verify error")
				return
			}
		}

		if ty == int(pt.Pos33Msg_BV) {
			_, ok := maker.bvss[string(m.Sort.SortHash.Hash)]
			if !ok {
				continue
			}
			maker.bvs[string(m.Hash)] = append(maker.bvs[string(m.Hash)], m)
		} else if ty == int(pt.Pos33Msg_MV) {
			_, ok := maker.mvss[string(m.Sort.SortHash.Hash)]
			if !ok {
				continue
			}
			maker.mvs[string(m.Hash)] = append(maker.mvs[string(m.Hash)], m)
		}
	}

	if ty == int(pt.Pos33Msg_BV) {
		n.trySetBlock(height, round)
		// } else if ty == int(pt.Pos33Msg_MV) {
		// n.tryMakeBlock(height, round)
	}
}

func (n *node) tryMakeBlock(height int64, round int) {
	maker := n.getMaker(height, round)
	if maker.my == nil {
		return
	}
	if maker.makeok {
		return
	}
	mh := string(maker.my.SortHash.Hash)
	ss := maker.mvs[mh]

	plog.Info("try make block", "height", height, "round", round, "mbss", len(maker.mvss), "nss", len(ss))
	if len(ss) < len(maker.mvss)/2+1 {
		return
	}
	var vs []*pt.Pos33VoteMsg
	for _, v := range ss {
		_, ok := maker.mvss[string(v.Sort.SortHash.Hash)]
		if ok {
			vs = append(vs, v)
		}
	}
	if len(vs) < len(maker.mvss)/2+1 {
		return
	}
	err := n.makeBlock(height, round, maker.my, vs)
	if err != nil {
		plog.Error("make block err", "err", err, "height", height, "round", round)
		return
	}
	maker.makeok = true
}

func (n *node) trySetBlock(height int64, round int) bool {
	maker := n.getMaker(height, round)
	// 收集到足够的block投票
	bh := ""
	sum := 0
	max := 0
	for h, v := range maker.bvs {
		l := len(v)
		sum += l
		if l > max {
			max = l
			bh = h
		}
	}

	nvss := len(maker.bvss)
	plog.Info("try set block", "height", height, "round", round, "nbss", nvss, "nbvs", len(maker.bvs), "sum", sum, "max", max)
	if sum < nvss*2/3+1 {
		return false
	}
	if max < nvss/2+1 {
		return false
	}

	// last block
	voter := n.getVoter(height, round)
	// 把相应的block写入链
	for _, b := range voter.ab.bs {
		h := string(b.Hash(n.GetAPI().GetConfig()))
		if bh == h {
			maker.ok = true
			n.setBlock(b)
			return true
		}
	}
	return false
}

func (n *node) handleBlockMsg(m *pt.Pos33BlockMsg, myself bool) {
	pb := n.lastBlock()
	if pb == nil {
		return
	}
	height := m.B.Height

	if !myself {
		if height != pb.Height+1 {
			plog.Error("handleBlock error", "err", "height not match", "height", height, "pheight", pb.Height)
			return
		}
		if string(m.B.ParentHash) != string(pb.Hash(n.GetAPI().GetConfig())) {
			plog.Error("handleBlock error", "err", "parentHash not match")
			return
		}
		err := n.blockCheck(m.B)
		if err != nil {
			plog.Error("handleBlock error", "height", m.B.Height, "err", err)
			return
		}
	}

	miner, err := getMiner(m.B)
	if err != nil {
		plog.Error("getMiner error", "err", err)
		return
	}
	round := int(miner.Sort.Proof.Input.Round)

	v := n.getVoter(height, round)
	v.ab.bs = append(v.ab.bs, m.B)

	hash := m.B.Hash(n.GetAPI().GetConfig())
	plog.Info("handleBlock", "height", height, "round", round, "bh", common.HashHex(hash)[:16])
	if miner.Sort.SortHash.Num == 0 {
		v.ab.ok = true
		n.voteBlock(hash, height, round)
	}
}

func (n *node) voteMaker(height int64, round int) {
	voter := n.getVoter(height, round)
	if len(voter.mymvss) == 0 {
		return
	}

	var mss []*pt.Pos33SortMsg
	for _, s := range voter.mss {
		mss = append(mss, s)
	}

	sort.Sort(pt.Sorts(mss))
	for i := 0; i < 3; i++ {
		for _, s := range mss {
			if i == int(s.SortHash.Num) {
				var vs []*pt.Pos33VoteMsg
				for _, mys := range voter.mymvss {
					v := &pt.Pos33VoteMsg{
						Hash: s.SortHash.Hash,
						Sort: mys,
					}
					v.Sign(n.priv)
					vs = append(vs, v)
				}
				plog.Info("vote maker", "nvs", len(vs), "height", height, "round", round, "num", i)
				n.sendVote(vs, int(pt.Pos33Msg_MV))
				break
			}
		}
	}
}

func (n *node) sendVote(vs []*pt.Pos33VoteMsg, ty int) {
	m := &pt.Pos33Votes{Vs: vs}
	n.handleVoteMsg(vs, true, ty)
	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_Ty(ty),
	}
	n.gss.gossip(pos33Topic, types.Encode(pm))
}

func (n *node) voteBlock(blockHash []byte, height int64, round int) {
	voter := n.getVoter(height, round)
	if len(voter.mybvss) == 0 {
		return
	}
	var vs []*pt.Pos33VoteMsg
	for _, mys := range voter.mybvss {
		v := &pt.Pos33VoteMsg{
			Hash: blockHash,
			Sort: mys,
		}
		v.Sign(n.priv)
		vs = append(vs, v)
	}
	plog.Info("vote block", "height", height, "round", round, "bh", common.HashHex(blockHash)[:16], "nvs", len(vs))
	n.sendVote(vs, int(pt.Pos33Msg_BV))
}

func (n *node) makeNextBlock(height int64, round int) {
	plog.Debug("makeNextBlock", "height", height)
	if n.lastBlock().Height+1 != height {
		return
	}
	// n.voteMaker(height, round)
}

func (n *node) handleMakerSort(m *pt.Pos33SortMsg, myself bool) {
	if m == nil || m.Proof == nil || m.Proof.Input == nil || m.SortHash == nil {
		plog.Error("handleSortitionMsg error, input msg is nil")
		return
	}
	height := m.Proof.Input.Height
	if !myself {
		if height > n.lastBlock().Height+pt.Pos33SortBlocks*2 {
			plog.Debug("handleSort height too hight", "height", height)
			return
		}
		if n.lastBlock().Height >= height {
			err := fmt.Errorf("sort msg too late, lbHeight=%d, sortHeight=%d", n.lastBlock().Height, height)
			plog.Debug("handleSort error", "err", err)
			return
		}
	}
	round := int(m.Proof.Input.Round)
	v := n.getVoter(height, round)
	v.mss[string(m.SortHash.Hash)] = m
	if round > 0 && height > n.maxSortHeight {
		n.maxSortHeight = height
	}
	plog.Info("handleMakerSort", "height", height, "round", round)
}

func (n *node) checkSort(s *pt.Pos33SortMsg, seed []byte, ty int) error {
	if s == nil {
		return fmt.Errorf("sortMsg error")
	}
	if s.Proof == nil || s.Proof.Input == nil || s.SortHash == nil {
		return fmt.Errorf("sortMsg error")
	}

	height := s.Proof.Input.Height
	err := n.verifySort(height, ty, seed, s)
	if err != nil {
		return err
	}
	return nil
}

func unmarshal(b []byte) (*pt.Pos33Msg, error) {
	var pm pt.Pos33Msg
	err := proto.Unmarshal(b, &pm)
	if err != nil {
		return nil, err
	}
	return &pm, nil
}

func (n *node) handlePos33Msg(pm *pt.Pos33Msg) bool {
	if pm == nil {
		return false
	}
	switch pm.Ty {
	case pt.Pos33Msg_MS:
		var m pt.Pos33SortMsg
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleMakerSort(&m, false)
	case pt.Pos33Msg_BVS, pt.Pos33Msg_MVS:
		var m pt.Pos33Sorts
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleVoterSort(&m, false, int(pm.Ty))
	case pt.Pos33Msg_MV, pt.Pos33Msg_BV:
		var m pt.Pos33Votes
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleVoteMsg(m.Vs, false, int(pm.Ty))
	case pt.Pos33Msg_B:
		var m pt.Pos33BlockMsg
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleBlockMsg(&m, false)
	default:
		panic("not support this message type")
	}

	return true
}

// handleGossipMsg multi-goroutine verify pos33 message
func (n *node) handleGossipMsg() chan *pt.Pos33Msg {
	num := 4
	ch := make(chan *pt.Pos33Msg, num*16)
	for i := 0; i < num; i++ {
		go func() {
			for {
				pm, err := unmarshal(<-n.gss.C)
				if err != nil {
					plog.Error(err.Error())
					continue
				}
				ch <- pm
			}
		}()
	}
	return ch
}

func (n *node) synced() bool {
	return n.IsCaughtUp() || n.lastBlock().Height+3 > n.maxSortHeight
}

func (n *node) getPID() {
	// get my pid
	list, err := n.GetAPI().PeerInfo(&types.P2PGetPeerReq{})
	if err != nil {
		panic(err)
	}
	self := list.Peers[len(list.Peers)-1]
	n.pid = self.Name
}

type hr struct {
	h int64
	r int
}

func (n *node) runLoop() {
	lb, err := n.RequestLastBlock()
	if err != nil {
		panic(err)
	}

	n.getPID()
	priv := n.getPriv()
	if priv == nil {
		panic("can't go here")
	}

	title := n.GetAPI().GetConfig().GetTitle()
	ns := fmt.Sprintf("%s-%d", title, n.conf.ListenPort)
	n.gss = newGossip2(priv, n.conf.ListenPort, ns, pos33Topic)
	msgch := n.handleGossipMsg()
	if len(n.conf.BootPeers) > 0 {
		go n.gss.bootstrap(n.conf.BootPeers...)
	}

	n.updateTicketCount(lb.Height)

	if lb.Height > 0 {
		time.AfterFunc(time.Second, func() {
			n.addBlock(lb)
		})
	}

	plog.Info("pos33 running...", "last block height", lb.Height)

	isSync := false
	syncTick := time.NewTicker(time.Second)
	tch := make(chan int64, 1)
	nch := make(chan int64, 1)
	ach := make(chan hr, 1)
	round := 0
	blockTimeout := time.Second * 3
	resortTimeout := time.Second * 2

	for {
		select {
		case <-n.done:
			plog.Debug("pos33 consensus run loop stoped")
			return
		case msg := <-msgch:
			n.handlePos33Msg(msg)
		case msg := <-n.gss.incoming:
			n.handlePos33Msg(msg)
		case <-syncTick.C:
			isSync = n.synced()
		default:
		}
		if !isSync {
			time.Sleep(time.Millisecond * 1000)
			plog.Info("NOT sync .......")
			continue
		}

		select {
		case height := <-tch:
			if height == n.lastBlock().Height+1 {
				round++
				plog.Info("block timeout", "height", height, "round", round)
				n.reSortition(height, round)
				time.AfterFunc(resortTimeout, func() {
					nh := n.lastBlock().Height + 1
					if height == nh {
						nch <- height
					} else if height > nh {
						plog.Info("block height reduce 1", "height", height, "lastHeight", nh)
						tch <- nh
					}
				})
			}
		case height := <-nch:
			nh := n.lastBlock().Height + 1
			if height == nh {
				n.makeNewBlock(height, round)
				time.AfterFunc(blockTimeout, func() {
					nh := n.lastBlock().Height + 1
					if height == nh {
						tch <- height
					} else if height > nh {
						plog.Info("block height reduce 2", "height", height, "lastHeight", nh)
						tch <- nh
					}
				})
			} else if height > nh {
				time.AfterFunc(time.Millisecond, func() {
					tch <- nh
				})
			}
			time.AfterFunc(time.Millisecond*700, func() {
				ach <- hr{height, round}
			})
		case b := <-n.bch: // new block add to chain
			round = 0
			n.handleNewBlock(b)
			time.AfterFunc(time.Millisecond, func() {
				nch <- b.Height + 1
			})
		case t := <-ach:
			lb := n.lastBlock()
			if lb.Height < t.h {
				ok := n.handleAlterBlock(t.h, t.r)
				if !ok {
					time.AfterFunc(time.Millisecond*700, func() {
						ach <- t
					})
				}
			}
		default:
			time.Sleep(time.Millisecond * 10)
		}
	}
}

func (n *node) handleAlterBlock(h int64, r int) bool {
	ab := n.getVoter(h, r).ab
	if ab.ok {
		return true
	}
	if ab.n >= 3 {
		return true
	}
	plog.Info("handleAlterBlock", "height", h, "round", r, "n", ab.n)
	for _, b := range ab.bs {
		m, err := getMiner(b)
		if err != nil {
			panic("can't go here")
		}
		if ab.n >= int(m.Sort.SortHash.Num) {
			n.voteBlock(b.Hash(n.GetAPI().GetConfig()), h, r)
			ab.ok = true
			return true
		}
	}
	ab.n++
	return false
}

const calcuDiffN = pt.Pos33SortBlocks * 1

func (n *node) handleNewBlock(b *types.Block) {
	if b.Height < n.lastBlock().Height {
		return
	}
	round := 0
	if b.Height == 0 {
		n.firstSortition()
	} else {
		n.sortition(b, round)
	}
	if b.Height < pt.Pos33SortBlocks/2+1 {
		n.voteMaker(b.Height+1, round)
	}
	n.voteMaker(b.Height+pt.Pos33SortBlocks/2, round)
	n.clear(b.Height)
}

func (n *node) makeNewBlock(height int64, round int) {
	// n.checkSorts(height, round)
	if round > 0 {
		// if timeout, only vote, handle vote will make new block
		n.voteMaker(height, round)
		return
	}
	// n.makeNextBlock(height, round)
	n.tryMakeBlock(height, round)
}

func (n *node) sendVoterSort(ss []*pt.Pos33SortMsg, height int64, round, ty int) {
	m := &pt.Pos33Sorts{
		Sorts: ss,
	}
	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_Ty(ty),
	}
	n.gss.gossip(pos33Topic, types.Encode(pm))
	n.handleVoterSort(m, true, ty)
}

func (n *node) sendMakerSort(m *pt.Pos33SortMsg, height int64, round int) {
	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_MS,
	}
	n.gss.gossip(pos33Topic, types.Encode(pm))
	n.handleMakerSort(m, true)
}

func hexs(b []byte) string {
	s := hex.EncodeToString(b)
	if len(s) <= 16 {
		return s
	}
	return s[:16]
}
