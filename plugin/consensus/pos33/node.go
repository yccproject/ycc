package pos33

import (
	"encoding/hex"
	"fmt"
	"math/rand"
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

const onlineDeration = 60

type onlineInfo struct {
	o int64
	w int64
	h int64
}

func (n *node) addOnline(addr string, o, h int64) bool {
	r, err := n.queryDeposit(addr)
	if err != nil {
		plog.Info("queryDeposit error", "err", err, "addr", addr)
		return false
	}
	plog.Info("online recv", "addr", addr[:16], "w", r.Count, "height", h, "o", o)

	n.olMap[addr] = &onlineInfo{o: o, w: r.Count, h: h}
	return true
}

type node struct {
	*Client
	gss *gossip2

	// I'm candidate maker in these blocks
	ims map[int64]map[int]*pt.Pos33SortMsg
	// receive candidate proposers
	cms map[int64]map[int]map[int]map[string]*pt.Pos33SortMsg
	// receive candidate votes
	cvs map[int64]map[int]map[string][]*pt.Pos33VoteMsg

	olMap       map[string]*onlineInfo
	onlines     map[int64]int64
	startHeight int64
	cch         chan bool

	// for new block incoming to add
	bch chan *types.Block
	// already make block height and round
	lheight int64
	lround  int

	maxSortHeight int64

	pid string
	abs map[int64]map[int]alterBlock
}

type alterBlock struct {
	voted bool
	bs    []*pt.Pos33BlockMsg
}

type tmt struct {
	h int64
	r int
}

// whether sortition or vote, the same height and round, only 1 time
//
func (n *node) findCps(height int64, round, num int, pub string) bool {
	a, ok := n.cms[height][round]
	if !ok {
		return false
	}
	for _, v := range a[num] {
		if string(v.Proof.Pubkey) == pub {
			return true
		}
	}
	return false
}

func (n *node) findCvs(height int64, round int, pub string) bool {
	mp, ok := n.cvs[height][round]
	if !ok {
		return false
	}
	for _, vs := range mp {
		for _, v := range vs {
			if string(v.Sig.Pubkey) == pub {
				return true
			}
		}
	}
	return false
}

// New create pos33 consensus client
func newNode(conf *subConfig) *node {
	n := &node{
		ims:     make(map[int64]map[int]*pt.Pos33SortMsg),
		cms:     make(map[int64]map[int]map[int]map[string]*pt.Pos33SortMsg),
		cvs:     make(map[int64]map[int]map[string][]*pt.Pos33VoteMsg),
		olMap:   make(map[string]*onlineInfo),
		onlines: make(map[int64]int64),
		bch:     make(chan *types.Block, 16),
		abs:     make(map[int64]map[int]alterBlock),
		cch:     make(chan bool, 1),
	}

	plog.Debug("@@@@@@@ node start:", "conf", conf)
	return n
}

func (n *node) changeMyCount() {
	n.cch <- true
}

func (n *node) lastBlock() *types.Block {
	b, err := n.RequestLastBlock()
	if err != nil {
		panic(err)
	}
	return b
}

const stopedBlocks = 60

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

func (n *node) mySort(height int64, round int) *pt.Pos33SortMsg {
	mp, ok := n.ims[height]
	if !ok {
		return nil
	}

	sort, ok := mp[round]
	if !ok {
		return nil
	}

	return sort
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
	// this code ONLY for TEST
	if n.conf.TrubleMaker {
		plog.Info("aha, I'am trubleMaker.", "height", height, "round", round)
		time.AfterFunc(time.Millisecond*time.Duration(1500+rand.Intn(500)),
			func() {
				n.sendBlockToPeer(pub, nb, vs[0].Order)
			})

	} else {
		n.sendBlockToPeer(pub, nb, vs[0].Order)
	}
	return nil
}
func (n *node) sendBlockToPeer(pub []byte, b *types.Block, order uint32) {
	m := &pt.Pos33BlockMsg{B: b, Pid: n.pid, Order: order}
	n.handleBlockMsg(m, true)
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
	for h := range n.cvs {
		if h <= height {
			delete(n.cvs, h)
		}
	}

	for h := range n.cms {
		if h <= height {
			delete(n.cms, h)
		}
	}
	for h := range n.ims {
		if h <= height {
			delete(n.ims, h)
		}
	}
	for h := range n.abs {
		if h <= height {
			delete(n.abs, h)
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

	// check votes
	if len(act.Votes) < pt.Pos33MustVotes {
		return fmt.Errorf("the block less Must vote, height=%d", b.Height)
	}
	height := b.Height
	if !checkVotesEnough(act.Votes, height, round) {
		return fmt.Errorf("the block NOT enough vote, height=%d", b.Height)
	}
	hash := act.Sort.SortHash.Hash
	for _, v := range act.Votes {
		err = n.checkVote(v, height, round, seed, string(hash))
		if err != nil {
			return err
		}
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
	n.makerSort(seed, height, round)
	n.sendSorts(height, round)
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
}

func (n *node) firstSortition() {
	seed := zeroHash[:]
	for i := 0; i < pt.Pos33SortBlocks; i++ {
		height := int64(i + 1)
		n.sortMaker(seed, height, 0)
		n.sendSorts(height, 0)
	}
}

func (n *node) sortMaker(seed []byte, height int64, round int) {
	plog.Debug("sortition", "height", height, "round", round)
	if n.conf.OnlyVoter {
		return
	}
	mp := n.ims[height]
	if mp == nil {
		n.ims[height] = make(map[int]*pt.Pos33SortMsg)
	}
	sms := n.makerSort(seed, height, round)
	if sms != nil {
		n.ims[height][round] = sms[0]
	}
	n.sendSorts(height, 0)
}

func (n *node) checkVote(vm *pt.Pos33VoteMsg, height int64, round int, seed []byte, minHash string) error {
	if !vm.Verify() {
		return fmt.Errorf("votemsg verify false")
	}
	if vm.Sort == nil || vm.Sort.Proof == nil || vm.Sort.Proof.Input == nil || vm.Sort.SortHash == nil {
		return fmt.Errorf("votemsg error, vm.Sort==nil or vm.Sort.Input==nil")
	}
	if height != vm.Sort.Proof.Input.Height {
		return fmt.Errorf("vote height is NOT consistent")
	}
	if round != int(vm.Sort.Proof.Input.Round) {
		return fmt.Errorf("vote round is NOT consistent: %d != %d", round, vm.Sort.Proof.Input.Round)
	}
	if minHash != string(vm.Hash) {
		return fmt.Errorf("vote Tid is NOT consistent")
	}
	if string(vm.Sig.Pubkey) != string(vm.Sort.Proof.Pubkey) {
		return fmt.Errorf("vote pubkey is NOT consistent")
	}

	err := n.verifySort(height, 1, seed, vm.Sort)
	if err != nil {
		plog.Error("check vote error", "height", height, "round", round, "err", err, "addr", address.PubKeyToAddr(vm.Sig.Pubkey))
		return err
	}
	return nil
}

type tvm = map[int64]map[int]map[string][]*pt.Pos33VoteMsg

func (n *node) addVote(vm *pt.Pos33VoteMsg, height int64, round int, minHash string) {
	if n.cvs[height] == nil {
		n.cvs[height] = make(map[int]map[string][]*pt.Pos33VoteMsg)
	}

	if n.cvs[height][round] == nil {
		mp := make(map[string][]*pt.Pos33VoteMsg)
		n.cvs[height][round] = mp
	}

	vs := n.cvs[height][round][minHash]
	for i, v := range vs {
		if v.Equal(vm) {
			// delete previous
			vs[i] = vs[len(vs)-1]
			vs = vs[:len(vs)-1]
			break
		}
	}
	vs = append(vs, vm)
	n.cvs[height][round][minHash] = vs
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

func (n *node) sendBlockToChain(m *pt.Pos33BlockMsg, self bool) {
	plog.Info("sendBlockToChain", "height", m.B.Height, "hash", common.HashHex(m.B.Hash(n.GetAPI().GetConfig()))[:16])
	n.setBlock(m.B)
}

func (n *node) handleOnlineMsg(m *pt.Pos33Online, myself bool) {
	if !m.Verify() {
		return
	}
	height := n.lastBlock().Height
	addr := address.PubKeyToAddr(m.Sig.Pubkey)
	if !m.Onlined && !myself {
		n.sendOnline(true)
	} else {
		n.addOnline(addr, m.Online, height)
	}
}

func (n *node) setOnline(height int64) {
	mp := make(map[int64]int)
	for _, o := range n.olMap {
		mp[o.o] += 1
	}
	max := 0
	online := int64(0)
	for o, e := range mp {
		if max < e {
			max = e
			online = o
		}
	}
	n.onlines[height] = online
}

func (n *node) getOnline(height int64) int64 {
	l := len(n.onlines)
	o, ok := n.onlines[height]
	if l < pt.Pos33SortBlocks*2 || !ok {
		allw := int64(n.allCount(height))
		o = allw
		plog.Info("getOnline", "height", height, "allw", allw)
	}
	return o
}

func (n *node) updateOnline(height int64) {
	if height-n.startHeight < onlineDeration/10 {
		n.setOnline(height)
		return
	}

	allw := int64(n.allCount(height))
	online := int64(0)
	for _, o := range n.olMap {
		online += o.w
	}
	if allw < online {
		online = allw
	}

	for k, v := range n.olMap {
		d := height - v.h
		if d >= onlineDeration {
			t := v.w / 10 * (d / onlineDeration)
			online -= t
			plog.Info("offline", "height", height, "addr", k[:16], "allw", allw, "w", v.w)
			if d/onlineDeration == 10 {
				delete(n.olMap, k)
			}
		}
	}
	plog.Info("updateOnline", "height", height, "allw", allw, "online", online)
	n.onlines[height] = online
	delete(n.onlines, height-pt.Pos33SortBlocks*2)
}

func (n *node) sendOnline(onlined bool) {
	height := n.GetCurrentHeight()
	m := &pt.Pos33Online{Onlined: onlined, Online: n.getOnline(height - 1)}
	m.Sign(n.priv)

	n.handleOnlineMsg(m, true)

	if n.gss == nil {
		return
	}

	pm := &pt.Pos33Msg{Data: types.Encode(m), Ty: pt.Pos33Msg_O}
	data := types.Encode(pm)
	n.gss.gossip(pos33Topic, data)
}

func (n *node) handleBlockMsg(m *pt.Pos33BlockMsg, myself bool) {
	pb := n.lastBlock()
	if pb == nil {
		return
	}
	height := m.B.Height
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

	miner, err := getMiner(m.B)
	if err != nil {
		plog.Error("getMiner error", "err", err)
		return
	}

	plog.Info("handleBlock", "height", height)
	bmp, ok := n.abs[height]
	if !ok {
		bmp = make(map[int]alterBlock)
		n.abs[height] = bmp
	}
	round := int(miner.Sort.Proof.Input.Round)
	bs := bmp[round]
	bs.bs = append(bs.bs, m)
	n.abs[height][round] = bs
}

func (n *node) handleVotesMsg(vms *pt.Pos33Votes, myself bool) {
	if len(vms.Vs1) > pt.Pos33RewardVotes {
		return
	}
	if len(vms.Vs2) > pt.Pos33RewardVotes {
		return
	}
	if len(vms.Vs3) > pt.Pos33RewardVotes {
		return
	}
	n.handleVotes(vms.Vs1, myself, 1)
	n.handleVotes(vms.Vs2, myself, 2)
	n.handleVotes(vms.Vs3, myself, 3)
}

func (n *node) handleVotes(vs []*pt.Pos33VoteMsg, myself bool, index int) {
	if len(vs) == 0 {
		// plog.Error("votemsg sortition is 0", "index", index)
		return
	}

	vm := vs[0]
	if vm.Sort == nil || vm.Sort.Proof == nil || vm.Sort.Proof.Input == nil {
		return
	}
	height := vm.Sort.Proof.Input.Height
	lb := n.lastBlock()
	if height <= lb.Height {
		plog.Debug("vote too late")
		return
	}
	if height > lb.Height+pt.Pos33SortBlocks*2 {
		plog.Debug("vote too hight")
		return
	}

	round := int(vm.Sort.Proof.Input.Round)
	minHash := string(vm.Hash)

	sort := n.checkMySort(minHash, height, round)
	if sort == nil {
		return // if not vote me, no use for me
	}

	if n.lheight == height && n.lround == round {
		return // if already make, vote is late, no use
	}

	if n.findCvs(height, round, string(vm.Sig.Pubkey)) {
		plog.Debug("repeat vote msg", "height", height, "round", round, "addr", address.PubKeyToAddr(vm.Sig.Pubkey))
		return
	}

	sortHeight := height - pt.Pos33SortBlocks
	seed, err := n.getSortSeed(sortHeight)
	if err != nil {
		plog.Error("getMinerSeed error", "err", err, "height", height)
		return
	}

	for _, vm := range vs {
		if !myself {
			err := n.checkVote(vm, height, round, seed, minHash)
			if err != nil {
				continue
			}
		}
		n.addVote(vm, height, round, minHash)
	}
	vs = n.cvs[height][round][minHash]
	plog.Debug("handleVotesMsg", "height", height, "round", round, "voter", saddr(vm.GetSig()), "votes", len(vs))

	if round == 0 || height < pt.Pos33SortBlocks {
		return
	}

	// 如果 block(height, round) 超时，收到票后，检查并 make block
	n.checkAndMakeBlock(height, round, sort, vs)
}

func (n *node) checkMySort(minHash string, height int64, round int) *pt.Pos33SortMsg {
	sort := n.mySort(height, round)
	if sort == nil {
		plog.Debug("mysort is nil", "height", height, "round", round)
		return nil
	}
	if minHash == "" {
		return sort
	}
	if string(sort.SortHash.Hash) != minHash {
		plog.Debug("mysort is NOT minHash", "height", height, "round", round)
		return nil
	}
	return sort
}

func (n *node) makeNextBlock(height int64, round int) {
	plog.Debug("makeNextBlock", "height", height)
	if n.lastBlock().Height+1 != height {
		return
	}
	rmp, ok := n.cvs[height]
	if !ok {
		plog.Debug("makeNextBlock error: NOT enought votes", "height", height)
		return
	}
	mp, ok := rmp[round]
	if !ok {
		plog.Debug("makeNextBlock error: NOT enought votes", "height", height)
		return
	}

	sort := n.checkMySort("", height, round)
	if sort == nil {
		return
	}
	vs := mp[string(sort.SortHash.Hash)]
	n.checkAndMakeBlock(height, round, sort, vs)
}

func (n *node) checkAndMakeBlock(height int64, round int, sort *pt.Pos33SortMsg, vs []*pt.Pos33VoteMsg) {
	if checkVotesEnough(vs, height, round) {
		err := n.makeBlock(height, round, sort, vs)
		if err != nil {
			plog.Debug("can't make block", "err", err, "height", height, "round", round)
		}
	}
}

type votes pt.Votes

func (v votes) Len() int { return len(v) }
func (v votes) Less(i, j int) bool {
	return v[i].SortsCount < v[i].SortsCount
}
func (v votes) Swap(i, j int) { v[i], v[j] = v[j], v[i] }

func voteWeight(vs []*pt.Pos33VoteMsg) float64 {
	vw := 0.
	for _, v := range vs {
		if v.Order == 1 {
			vw += 1.
		} else if v.Order == 2 {
			vw += 0.87
		} else if v.Order == 3 {
			vw += 0.74
		} else {
			vw += 0.
		}
	}
	return vw
}

func checkVotesEnough(vs []*pt.Pos33VoteMsg, height int64, round int) bool {
	vw := voteWeight(vs)
	if vw < pt.Pos33MustVotes {
		plog.Info("block vote < 11", "height", height, "round", round)
		return false
	}

	// remove the largest and smallest 1/3
	sort.Sort(votes(vs))
	del := len(vs) / 3
	cvs := vs[del : len(vs)-del]

	sum := 0
	for _, v := range cvs {
		sum += int(v.SortsCount)
	}
	sortsCount := sum / len(cvs)
	if sortsCount > pt.Pos33RewardVotes {
		sortsCount = pt.Pos33RewardVotes
	}
	if int(vw*2) <= sortsCount {
		plog.Debug("vote less than 2/3", "height", height, "round", round)
		return false
	}
	return true
}

func (n *node) handleSortitionMsg(m *pt.Pos33SortMsg, lbHeight int64) {
	if m == nil || m.Proof == nil || m.Proof.Input == nil || m.SortHash == nil {
		plog.Error("handleSortitionMsg error, input msg is nil")
		return
	}
	height := m.Proof.Input.Height
	if height > lbHeight+pt.Pos33SortBlocks*2 {
		plog.Debug("handleSort height too hight", "height", height, "lbheight", lbHeight)
		return
	}
	if n.lastBlock().Height >= height {
		err := fmt.Errorf("sort msg too late, lbHeight=%d, sortHeight=%d", n.lastBlock().Height, height)
		plog.Debug("handleSort error", "err", err)
		return
	}
	if n.cms[height] == nil {
		n.cms[height] = make(map[int]map[int]map[string]*pt.Pos33SortMsg)
	}
	round := int(m.Proof.Input.Round)

	num := int(m.SortHash.Num)
	if num < 0 || num >= 3 {
		return
	}
	if n.cms[height][round] == nil {
		n.cms[height][round] = make(map[int]map[string]*pt.Pos33SortMsg)
	}
	if n.cms[height][round][num] == nil {
		n.cms[height][round][num] = make(map[string]*pt.Pos33SortMsg)
	}
	if n.findCps(height, round, num, string(m.Proof.Pubkey)) {
		plog.Debug("repeat sortition msg", "height", height, "round", round, "addr", address.PubKeyToAddr(m.Proof.Pubkey))
		return
	}
	n.cms[height][round][num][string(m.SortHash.Hash)] = m
	plog.Debug("handleSortitionMsg", "height", height, "round", round, "size", len(n.cms[height][round]))
	if round > 0 && height > n.maxSortHeight {
		n.maxSortHeight = height
	}
}

func (n *node) checkSort(s *pt.Pos33SortMsg, seed []byte, step int) error {
	if s == nil {
		return fmt.Errorf("sortMsg error")
	}
	if s.Proof == nil || s.Proof.Input == nil || s.SortHash == nil {
		return fmt.Errorf("sortMsg error")
	}

	height := s.Proof.Input.Height
	err := n.verifySort(height, step, seed, s)
	if err != nil {
		return err
	}
	return nil
}

func (n *node) handleSortsMsg(m *pt.Pos33Sorts, myself bool) {
	lb, err := n.RequestLastBlock()
	if err != nil {
		plog.Debug("handleSortsMsg err", "err", err)
		return
	}
	if m.S != nil {
		n.handleSortitionMsg(m.S, lb.Height)
	}
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
	case pt.Pos33Msg_S:
		var m pt.Pos33Sorts
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleSortsMsg(&m, false)
	case pt.Pos33Msg_V:
		var m pt.Pos33Votes
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleVotesMsg(&m, false)
	case pt.Pos33Msg_B:
		var m pt.Pos33BlockMsg
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleBlockMsg(&m, false)
	case pt.Pos33Msg_O:
		var m pt.Pos33Online
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleOnlineMsg(&m, false)
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

	n.sendOnline(false)
	plog.Info("pos33 running...", "last block height", lb.Height)

	isSync := false
	syncTick := time.NewTicker(time.Second)
	tch := make(chan int64, 1)
	nch := make(chan int64, 1)
	ach := make(chan tmt, 1)
	round := 0
	at := 0
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
		case <-n.cch:
			n.sendOnline(true)
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
			at = 0
			time.AfterFunc(time.Millisecond*700, func() {
				ach <- tmt{height, round}
			})
		case b := <-n.bch: // new block add to chain
			round = 0
			n.handleNewBlock(b)
			time.AfterFunc(time.Millisecond, func() {
				nch <- b.Height + 1
			})
		case t := <-ach:
			lb := n.lastBlock()
			at++
			if lb.Height < t.h && at <= 3 {
				l := n.handleAlterBlock(t.h, t.r, at)
				if l > 0 {
					time.AfterFunc(time.Millisecond*300, func() {
						ach <- t
					})
				}
			} else {
				at = 0
			}
		default:
			time.Sleep(time.Millisecond * 10)
		}
	}
}

func (n *node) handleAlterBlock(h int64, r, t int) int {
	plog.Info("handleAlterBlock", "height", h, "round", r)
	bs := n.abs[h][r].bs
	l := len(bs)
	if t < 3 {
		if l < 3 {
			return l
		}
	}
	if l == 0 {
		return 0
	}
	if l == 1 {
		n.sendBlockToChain(bs[0], false)
		return 0
	}
	var tb *pt.Pos33BlockMsg
	var minHash string
	for _, b := range bs {
		m, err := getMiner(b.B)
		if err != nil {
			panic("can't go here")
		}
		h := string(m.Sort.SortHash.Hash)
		if minHash == "" {
			minHash = h
			tb = b
		}
		if minHash > h {
			minHash = h
			tb = b
		}
	}
	n.sendBlockToChain(tb, false)
	return 0
}

const calcuDiffN = pt.Pos33SortBlocks * 1

func (n *node) handleNewBlock(b *types.Block) {
	round := 0
	if b.Height == 0 {
		n.firstSortition()
	} else {
		n.sortition(b, round)
	}
	if b.Height < pt.Pos33SortBlocks/2 {
		n.vote(b.Height+1, round)
	}
	n.vote(b.Height+pt.Pos33SortBlocks/2, round)
	n.clear(b.Height)
	if b.Height%(onlineDeration/10) == 0 {
		n.sendOnline(true)
	}
	if n.startHeight == 0 {
		n.startHeight = b.Height
		n.sendOnline(false)
	}
	n.updateOnline(b.Height)
}

func (n *node) makeNewBlock(height int64, round int) {
	// n.checkSorts(height, round)
	if round > 0 {
		// if timeout, only vote, handle vote will make new block
		n.vote(height, round)
		return
	}
	n.makeNextBlock(height, round)
}

func (n *node) sendSorts(height int64, round int) {
	s := n.mySort(height, round)

	m := &pt.Pos33Sorts{ /*Sorts: ss*/ S: s}
	n.gss.gossip(pos33Topic, marshalSortsMsg(m))
	n.handleSortsMsg(m, true)
}

func hexs(b []byte) string {
	s := hex.EncodeToString(b)
	if len(s) <= 16 {
		return s
	}
	return s[:16]
}

func (n *node) vote(height int64, round int) {
	pss := n.getMakerSorts(height, round)
	if pss == nil {
		plog.Debug("vote bp is nil", "height", height, "round", round)
		return
	}

	var vs1, vs2, vs3 []*pt.Pos33VoteMsg
	for i, sm := range pss {
		if sm == nil {
			continue
		}
		ss := n.voterSort(sm.Proof.Input.Seed, height, round, i, sm.Proof.Diff)
		if len(ss) == 0 {
			plog.Debug("I'm not verifer", "height", height)
			continue
		}
		minHash := sm.SortHash.Hash
		var vs []*pt.Pos33VoteMsg
		for _, s := range ss {
			v := &pt.Pos33VoteMsg{Sort: s, Hash: minHash, SortsCount: 30, Order: uint32(i + 1)}
			priv := n.getPriv()
			if priv == nil {
				panic("can't go here")
			}
			v.Sign(priv)
			vs = append(vs, v)
		}
		if i == 0 {
			vs1 = vs
		} else if i == 1 {
			vs2 = vs
		} else if i == 2 {
			vs3 = vs
		} else {
			panic("can't go here")
		}
	}

	v := &pt.Pos33Votes{Vs1: vs1, Vs2: vs2, Vs3: vs3}
	pm, data := marshalVoteMsg(v)
	for _, sm := range pss {
		if sm == nil {
			continue
		}
		pub := sm.Proof.Pubkey
		if string(n.priv.PubKey().Bytes()) != string(pub) {
			n.gss.sendMsg(pub, pm)
		}
		plog.Info("block vote", "height", height, "round", round, "maker", address.PubKeyToAddr(pub)[:16])
	}
	n.gss.gossip(pos33Topic, data)
	n.handleVotesMsg(v, true)
}

func marshalSortsMsg(m proto.Message) []byte {
	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_S,
	}
	return types.Encode(pm)
}

func marshalVoteMsg(v proto.Message) (*pt.Pos33Msg, []byte) {
	pm := &pt.Pos33Msg{
		Data: types.Encode(v),
		Ty:   pt.Pos33Msg_V,
	}
	return pm, types.Encode(pm)
}
