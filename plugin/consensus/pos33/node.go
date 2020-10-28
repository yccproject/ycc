package pos33

import (
	"encoding/hex"
	"fmt"
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

const pos33Topic = "pos33"

type node struct {
	*Client
	gss *gossip2

	// I'm candidate proposer in these blocks
	ips map[int64]map[int]*pt.Pos33SortMsg
	// I'm candidate verifer in these blocks
	ivs map[int64]map[int][]*pt.Pos33SortMsg
	// receive candidate proposers
	cps map[int64]map[int]map[string]*pt.Pos33SortMsg
	// receive candidate votes
	cvs map[int64]map[int]map[string][]*pt.Pos33VoteMsg
	// receive candidate verifers
	css map[int64]map[int][]*pt.Pos33SortMsg

	// for new block incoming to add
	bch     chan *types.Block
	lheight int64
	lround  int
}

// New create pos33 consensus client
func newNode(conf *subConfig) *node {
	n := &node{
		ips: make(map[int64]map[int]*pt.Pos33SortMsg),
		ivs: make(map[int64]map[int][]*pt.Pos33SortMsg),
		cps: make(map[int64]map[int]map[string]*pt.Pos33SortMsg),
		cvs: make(map[int64]map[int]map[string][]*pt.Pos33VoteMsg),
		css: make(map[int64]map[int][]*pt.Pos33SortMsg),
		bch: make(chan *types.Block, 16),
	}

	plog.Info("@@@@@@@ node start:", "conf", conf)
	return n
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
	plog.Info("make a minerTx", "nvs", len(vs), "height", height, "fee", tx.Fee, "from", tx.From())
	return tx, nil
}

func (n *node) blockDiff(lb *types.Block, w int) uint32 {
	powLimitBits := n.GetAPI().GetConfig().GetP(lb.Height).PowLimitBits
	return powLimitBits
}

func (n *node) myVotes(height int64, round int) []*pt.Pos33SortMsg {
	mp, ok := n.ivs[height]
	if !ok {
		return nil
	}

	vs, ok := mp[round]
	if !ok {
		return nil
	}

	return vs
}

func (n *node) mySort(height int64, round int) *pt.Pos33SortMsg {
	mp, ok := n.ips[height]
	if !ok {
		return nil
	}

	sort, ok := mp[round]
	if !ok {
		return nil
	}

	return sort
}

func (n *node) getPrivByTid(tid string, height int64) (crypto.PrivKey, error) {
	t := n.getTicket(tid)
	if t == nil {
		return nil, fmt.Errorf("getTicket error: %s", tid)
	}
	priv := n.getPriv(t.MinerAddress)
	if priv == nil {
		return nil, fmt.Errorf("getPriv error: %s", t.MinerAddress)
	}
	return priv, nil
}

func (n *node) makeBlock(height int64, round int, tid string, vs []*pt.Pos33VoteMsg) error {
	lb := n.lastBlock()
	if height != lb.Height+1 {
		return fmt.Errorf("makeBlock height error")
	}
	if n.lheight == height && n.lround == round {
		return fmt.Errorf("makeBlock already made error")
	}
	sort := n.mySort(height, round)
	if sort == nil {
		err := fmt.Errorf("makeBlock sort nil error")
		return err
	}
	if sort.SortHash.Tid != tid {
		err := fmt.Errorf("makeBlock tid error")
		return err
	}

	priv, err := n.getPrivByTid(sort.SortHash.Tid, height)
	if err != nil {
		return err
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
	plog.Info("@@@@@@@ I make a block: ", "height", height, "round", round, "ntx", len(nb.Txs), "nvs", len(vs), "diff", nb.Difficulty)

	// this code ONLY for TEST
	if n.conf.TrubleMaker {
		time.AfterFunc(time.Second*5, func() { n.setBlock(nb) })
	} else {
		if nb.BlockTime-lb.BlockTime >= 1 {
			return n.setBlock(nb)
		}
		time.AfterFunc(time.Millisecond*500, func() { n.setBlock(nb) })
	}
	return nil
}

func (n *node) addBlock(b *types.Block) {
	if !n.miningOK() {
		return
	}

	lastHeight := n.lastBlock().Height
	if b.Height != lastHeight {
		plog.Error("addBlock height error", "height", b.Height, "lastHeight", lastHeight)
		return
	}

	plog.Info("node.addBlock", "height", b.Height, "hash", common.ToHex(b.Hash(n.GetAPI().GetConfig())))
	if b.BlockTime-n.lastBlock().BlockTime < 1 {
		time.AfterFunc(time.Millisecond*300, func() {
			n.bch <- b
		})
	} else {
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
	for h := range n.css {
		if h <= height {
			delete(n.css, h)
		}
	}
	for h := range n.cps {
		if h <= height {
			delete(n.cps, h)
		}
	}
	for h := range n.ips {
		if h <= height {
			delete(n.ips, h)
		}
	}
	for h := range n.ivs {
		if h <= height {
			delete(n.ivs, h)
		}
	}
}

func saddr(sig *types.Signature) string {
	if sig == nil {
		return ""
	}
	return address.PubKeyToAddress(sig.Pubkey).String()
}

func (n *node) checkBlock(b, pb *types.Block) error {
	plog.Info("node.checkBlock", "height", b.Height, "pbheight", pb.Height)
	if b.Height <= pb.Height {
		return fmt.Errorf("check block height error")
	}
	if !n.miningOK() {
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
	if string(act.Sort.Proof.Pubkey) == string(n.getPriv("").PubKey().Bytes()) {
		return nil
	}

	sortHeight := b.Height - pt.Pos33SortitionSize
	allw := n.allTicketCount(sortHeight)
	seed, err := n.getSortSeed(sortHeight)
	if err != nil {
		return err
	}
	err = n.verifySort(b.Height, 0, allw, seed, act.GetSort())
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
	if len(act.Votes) > pt.Pos33RewardVotes {
		return fmt.Errorf("the block vote too much, height=%d", b.Height)
	}
	tid := act.Sort.SortHash.Tid
	for _, v := range act.Votes {
		err = n.checkVote(v, height, round, seed, allw, tid)
		if err != nil {
			return err
		}
	}

	return nil
}

func getMinerSeed(b *types.Block) ([]byte, error) {
	seed := zeroHash[:]
	if b.Height > pt.Pos33SortitionSize {
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
	b, err := n.RequestBlock(height - pt.Pos33SortitionSize)
	if err != nil {
		plog.Info("requestBlock error", "height", height-pt.Pos33SortitionSize, "error", err)
		return false
	}
	seed, err := getMinerSeed(b)
	if err != nil {
		plog.Error("reSortition error", "height", height, "round", round, "err", err)
		return false
	}
	const staps = 2
	allw := n.allTicketCount(height - pt.Pos33SortitionSize)
	for s := 0; s < staps; s++ {
		if s == 0 && n.conf.OnlyVoter {
			continue
		}
		sms := n.sort(seed, height, round, s, allw)
		if sms == nil {
			plog.Info("node.sortition nil", "height", height, "round", round)
			continue
		}
		plog.Info("node.reSortition", "height", height, "round", round, "step", s, "weight", len(sms))
		if s == 0 {
			mp := n.ips[height]
			if mp == nil {
				n.ips[height] = make(map[int]*pt.Pos33SortMsg)
			}
			n.ips[height][round] = sms[0]
		} else {
			mp := n.ivs[height]
			if mp == nil {
				n.ivs[height] = make(map[int][]*pt.Pos33SortMsg)
			}
			n.ivs[height][round] = sms
		}
	}
	n.sendSorts(height, round)
	return true
}

func (n *node) firstSortition() {
	seed := zeroHash[:]
	const steps = 2
	round := 0
	allw := n.allTicketCount(0)
	for s := 0; s < steps; s++ {
		for i := 0; i < pt.Pos33SortitionSize; i++ {
			height := int64(i + 1)
			sms := n.sort(seed, height, round, s, allw)
			if sms == nil {
				plog.Info("node.sortition nil", "height", height, "round", round)
				continue
			}
			plog.Info("node.sortition", "height", height, "round", round, "weight", len(sms), "allw", allw)
			if s == 0 {
				mp := n.ips[height]
				if mp == nil {
					n.ips[height] = make(map[int]*pt.Pos33SortMsg)
				}
				n.ips[height][round] = sms[0]
			} else {
				mp := n.ivs[height]
				if mp == nil {
					n.ivs[height] = make(map[int][]*pt.Pos33SortMsg)
				}
				n.ivs[height][round] = sms
			}
			n.sendSorts(height, 0)
		}
	}
}

func (n *node) sortition(b *types.Block, round int) {
	plog.Info("sortition", "height", b.Height, "round", round)
	const steps = 2
	allw := n.allTicketCount(b.Height)
	height := b.Height + pt.Pos33SortitionSize
	seed, err := getMinerSeed(b)
	if err != nil {
		plog.Error("sortition error", "error", err)
		return
	}
	for s := 0; s < steps; s++ {
		if s == 0 && n.conf.OnlyVoter {
			continue
		}
		sms := n.sort(seed, height, round, s, allw)
		if sms == nil {
			plog.Info("node.sortition nil", "height", height, "round", round)
			continue
		}
		plog.Info("node.sortition", "height", height, "round", round, "weight", len(sms), "allw", allw)
		if s == 0 {
			mp := n.ips[height]
			if mp == nil {
				n.ips[height] = make(map[int]*pt.Pos33SortMsg)
			}
			n.ips[height][round] = sms[0]
		} else {
			mp := n.ivs[height]
			if mp == nil {
				n.ivs[height] = make(map[int][]*pt.Pos33SortMsg)
			}
			n.ivs[height][round] = sms
		}
		n.sendSorts(height, 0)
	}
}

func (n *node) checkVote(vm *pt.Pos33VoteMsg, height int64, round int, seed []byte, allw int, tid string) error {
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
	if tid != vm.Tid {
		return fmt.Errorf("vote Tid is NOT consistent: %s != %s", tid, vm.Tid)
	}
	if string(vm.Sig.Pubkey) != string(vm.Sort.Proof.Pubkey) {
		return fmt.Errorf("vote pubkey is NOT consistent")
	}

	err := n.verifySort(height, 1, allw, seed, vm.Sort)
	if err != nil {
		return err
	}
	return nil
}

func (n *node) addVote(vm *pt.Pos33VoteMsg, height int64, round int, tid string) {
	if n.cvs[height] == nil {
		n.cvs[height] = make(map[int]map[string][]*pt.Pos33VoteMsg)
	}

	if n.cvs[height][round] == nil {
		mp := make(map[string][]*pt.Pos33VoteMsg)
		n.cvs[height][round] = mp
	}

	vs := n.cvs[height][round][tid]
	for i, v := range vs {
		if v.Equal(vm) {
			// delete previous
			vs[i] = vs[len(vs)-1]
			vs = vs[:len(vs)-1]
			break
		}
	}
	vs = append(vs, vm)
	n.cvs[height][round][tid] = vs
}

func (n *node) getSortSeed(height int64) ([]byte, error) {
	if height < pt.Pos33SortitionSize {
		return zeroHash[:], nil
	}
	sb, err := n.RequestBlock(height)
	if err != nil {
		plog.Info("request block error", "height", height, "err", err)
		return nil, err
	}
	return getMinerSeed(sb)
}

func (n *node) handleVotesMsg(vms *pt.Pos33Votes, myself bool) {
	if n.lastBlock() == nil {
		return
	}
	if len(vms.Vs) == 0 {
		plog.Error("votemsg sortition is 0")
		return
	}

	vm := vms.Vs[0]
	if vm.Sort == nil || vm.Sort.Proof == nil || vm.Sort.Proof.Input == nil {
		return
	}
	height := vm.Sort.Proof.Input.Height
	if height <= n.lastBlock().Height {
		plog.Info("vote too late")
		return
	}

	round := int(vm.Sort.Proof.Input.Round)
	tid := vm.Tid

	if n.lheight == height && n.lround == round {
		return
	}

	sortHeight := height - pt.Pos33SortitionSize
	seed, err := n.getSortSeed(sortHeight)
	if err != nil {
		plog.Error("getMinerSeed error", "err", err, "height", height)
		return
	}
	allw := n.allTicketCount(sortHeight)

	for _, vm := range vms.Vs {
		if !myself {
			err := n.checkVote(vm, height, round, seed, allw, tid)
			if err != nil {
				plog.Error("check error", "height", height, "round", round, "err", err)
				if err != nil {
					continue
				}
			}
		}
		n.addVote(vm, height, round, tid)
	}
	vs := n.cvs[height][round][tid]
	plog.Info("handleVotesMsg", "height", height, "round", round, "tid", tid, "voter", saddr(vm.GetSig()), "votes", len(vs))
}

func (n *node) makeNextBlock(height int64, round int) {
	plog.Info("makeNextBlock", "height", height)
	if n.lastBlock().Height+1 != height {
		return
	}
	rmp, ok := n.cvs[height]
	if !ok {
		plog.Info("makeNextBlock error: NOT enought votes", "height", height)
		return
	}
	mp, ok := rmp[round]
	if !ok {
		plog.Info("makeNextBlock error: NOT enought votes", "height", height)
		return
	}

	mtid := ""
	max := 0
	for tid, vs := range mp {
		if len(vs) > max {
			max = len(vs)
			mtid = tid
		}
	}
	vs := mp[mtid]
	if checkVotesEnough(vs, height, round) {
		err := n.makeBlock(height, round, mtid, vs)
		if err != nil {
			plog.Info("can't make block", "err", err, "height", height, "round", round)
		}
	}
}

func (n *node) getSorts(height int64, round int) []*pt.Pos33SortMsg {
	mp, ok := n.css[height]
	if !ok {
		return nil
	}
	ss, ok := mp[round]
	if !ok {
		return nil
	}

	return ss
}

type votes pt.Votes

func (v votes) Len() int { return len(v) }
func (v votes) Less(i, j int) bool {
	return string(v[i].SortsCount) < string(v[i].SortsCount)
}
func (v votes) Swap(i, j int) { v[i], v[j] = v[j], v[i] }

func checkVotesEnough(vs []*pt.Pos33VoteMsg, height int64, round int) bool {
	if len(vs) < pt.Pos33MustVotes {
		plog.Info("vote < 11", "height", height, "round", round)
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
	if len(vs)*2 <= sortsCount {
		plog.Info("vote less than 2/3", "height", height, "round", round)
		return false
	}
	return true
}

func (n *node) handleSortitionMsg(m *pt.Pos33SortMsg) {
	if m == nil || m.Proof == nil || m.Proof.Input == nil || m.SortHash == nil {
		plog.Error("handleSortitionMsg error, input msg is nil")
		return
	}
	height := m.Proof.Input.Height
	if n.lastBlock().Height >= height {
		err := fmt.Errorf("sort msg too late, lbHeight=%d, sortHeight=%d", n.lastBlock().Height, height)
		plog.Info("handleSort error", "err", err)
		return
	}
	if n.cps[height] == nil {
		n.cps[height] = make(map[int]map[string]*pt.Pos33SortMsg)
	}
	round := int(m.Proof.Input.Round)
	if n.cps[height][round] == nil {
		n.cps[height][round] = make(map[string]*pt.Pos33SortMsg)
	}
	n.cps[height][round][m.SortHash.Tid] = m
	plog.Info("handleSortitionMsg", "height", height, "round", round, "size", len(n.cps[height][round]))
}

func (n *node) checkSort(s *pt.Pos33SortMsg, seed []byte, allw int) error {
	if s == nil {
		return fmt.Errorf("sortMsg error")
	}
	if s.Proof == nil || s.Proof.Input == nil || s.SortHash == nil {
		return fmt.Errorf("sortMsg error")
	}

	height := s.Proof.Input.Height
	err := n.verifySort(height, int(s.Proof.Input.Step), allw, seed, s)
	if err != nil {
		plog.Error("verifySort error", "err", err, "height", height, "tid", s.SortHash.Tid)
		return err
	}
	return nil
}

func (n *node) checkSorts(height int64, round int) []*pt.Pos33SortMsg {
	mp, ok := n.css[height]
	if !ok {
		return nil
	}
	ss, ok := mp[round]
	if !ok {
		return nil
	}
	sortHeight := height - pt.Pos33SortitionSize
	seed, err := n.getSortSeed(sortHeight)
	if err != nil {
		return nil
	}
	allw := n.allTicketCount(sortHeight)
	var rss []*pt.Pos33SortMsg
	for _, s := range ss {
		err := n.checkSort(s, seed, allw)
		if err != nil {
			plog.Error("checkSort error", "err", err, "height", height, "round", round)
			continue
		}
		rss = append(rss, s)
	}
	n.css[height][round] = rss
	return rss
}

func (n *node) handleSortsMsg(m *pt.Pos33Sorts, myself bool) {
	if len(m.Sorts) == 0 && m.S == nil {
		return
	}
	lb, err := n.RequestLastBlock()
	if err != nil {
		plog.Info("handleSortsMsg err", "err", err)
		return
	}
	if m.S != nil {
		n.handleSortitionMsg(m.S)
	}
	for i, s := range m.Sorts {
		if !myself {
			/*
				err := n.checkSort(s)
				if err != nil {
					plog.Error("checkSort error", "err", err)
					return
				}
			*/
			if s == nil || s.Proof == nil || s.Proof.Input == nil || s.SortHash == nil {
				plog.Error("handleSortsMsg error, input msg is nil")
				return
			}
		}
		height := s.Proof.Input.Height
		if height > lb.Height+pt.Pos33SortitionSize {
			// too
			return
		}
		if n.lastBlock().Height >= height {
			err := fmt.Errorf("sort msg too late, lbHeight=%d, sortHeight=%d", n.lastBlock().Height, height)
			plog.Info("handleSort error", "err", err)
		}
		round := int(s.Proof.Input.Round)
		if n.css[height] == nil {
			n.css[height] = make(map[int][]*pt.Pos33SortMsg)
		}
		ss := n.css[height][round]
		ss = append(ss, s)
		n.css[height][round] = ss
		if i == len(m.Sorts)-1 {
			plog.Info("handleSortsMsg", "height", height, "round", round, "who", address.PubKeyToAddr(s.Proof.Pubkey), "len(css)", len(ss))
		}
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
		var vt pt.Pos33Votes
		err := types.Decode(pm.Data, &vt)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleVotesMsg(&vt, false)
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

func (n *node) runLoop() {
	lb, err := n.RequestLastBlock()
	if err != nil {
		panic(err)
	}

	svcTag := n.GetAPI().GetConfig().GetTitle()
	n.gss = newGossip2(n.getPriv(""), n.conf.ListenPort, svcTag, pos33Topic)
	msgch := n.handleGossipMsg()
	if len(n.conf.BootPeers) > 0 {
		n.gss.bootstrap(n.conf.BootPeers...)
	}

	time.AfterFunc(time.Second, func() {
		n.addBlock(lb)
	})

	plog.Info("@@@@@@@@ pos33 node running.......", "last block height", lb.Height)
	isSync := false
	syncTick := time.NewTicker(time.Second)
	etm := time.NewTimer(time.Hour)
	ch := make(chan int64, 1)
	round := 0
	blockTimeout := time.Second * 5

	for {
		select {
		case <-n.done:
			plog.Info("pos33 consensus run loop stoped")
			return
		case msg := <-msgch:
			t := time.Now()
			n.handlePos33Msg(msg)
			plog.Info("handlePos33Msg cost", "cost", time.Now().Sub(t))
		case <-syncTick.C:
			isSync = n.miningOK()
		}
		if !isSync {
			time.Sleep(time.Millisecond * 300)
			continue
		}

		select {
		case height := <-ch:
			if height == n.lastBlock().Height+1 {
				round++
				plog.Info("@@@ make timeout: ", "height", height, "round", round)
				n.reSortition(height, round)
				etm = time.NewTimer(time.Second * 3)
			}
		case <-etm.C:
			height := n.lastBlock().Height + 1
			n.makeNewBlock(height, round)
			time.AfterFunc(blockTimeout, func() {
				ch <- height
			})
		case b := <-n.bch: // new block add to chain
			round = 0
			n.handleNewBlock(b)
			etm = time.NewTimer(time.Millisecond * 10)
		default:
			time.Sleep(time.Millisecond * 10)
		}
	}
}

func (n *node) handleNewBlock(b *types.Block) {
	plog.Info("handleNewBlock", "height", b.Height)
	round := 0
	if b.Height == 0 {
		n.firstSortition()
	} else {
		n.sortition(b, round)
	}
	if b.Height < pt.Pos33SortitionSize/2 {
		n.vote(b.Height+1, round)
	}
	n.vote(b.Height+pt.Pos33SortitionSize/2, round)
	n.clear(b.Height)
}

func (n *node) makeNewBlock(height int64, round int) {
	n.checkSorts(height, round)
	if round > 0 {
		n.vote(height, round)
	}
	n.makeNextBlock(height, round)
}

func (n *node) sendSorts(height int64, round int) {
	ss := n.myVotes(height, round)
	s := n.mySort(height, round)

	m := &pt.Pos33Sorts{Sorts: ss, S: s}
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
	tid := n.bp(height, round)
	if tid == "" {
		plog.Info("vote bp is nil", "height", height, "round", round)
		return
	}
	ss := n.myVotes(height, round)
	if ss == nil {
		plog.Info("I'm not verifer", "height", height)
		return
	}
	plog.Info("vote bp", "height", height, "round", round, "tid", tid)
	var vs []*pt.Pos33VoteMsg
	for _, s := range ss {
		v := &pt.Pos33VoteMsg{Sort: s, Tid: tid, SortsCount: uint32(len(n.css[height][round]))}
		t := n.getTicket(s.SortHash.Tid)
		if t == nil {
			plog.Info("vote error: my ticket is gone", "ticketID", s.SortHash.Tid)
			continue
		}
		if t.Status == pt.Pos33TicketClosed {
			if t.CloseHeight < height-pt.Pos33SortitionSize {
				plog.Info("vote error: my ticket is closed", "ticketID", s.SortHash.Tid, "ticketCloseHeight", t.CloseHeight, "height", height)
				continue
			}
		}
		priv := n.getPriv(t.MinerAddress)
		if priv == nil {
			plog.Info("vote error: my miner address is gone", "mineaddr", t.MinerAddress)
			continue
		}
		v.Sign(priv)
		vs = append(vs, v)
	}
	v := &pt.Pos33Votes{Vs: vs}
	n.gss.gossip(pos33Topic, marshalVoteMsg(v))
	n.handleVotesMsg(v, true)
}

func marshalSortsMsg(m proto.Message) []byte {
	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_S,
	}
	return types.Encode(pm)
}

func marshalVoteMsg(v proto.Message) []byte {
	pm := &pt.Pos33Msg{
		Data: types.Encode(v),
		Ty:   pt.Pos33Msg_V,
	}
	return types.Encode(pm)
}
