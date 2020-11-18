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

const pos33Topic = "pos33-1104"

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
	bch chan *types.Block
	// already make block height and round
	lheight int64
	lround  int

	bvs []int
}

// whether sortition or vote, the same height and round, only 1 time
//
func (n *node) findCps(height int64, round int, pub string) bool {
	mp, ok := n.cps[height][round]
	if !ok {
		return false
	}
	for _, v := range mp {
		if string(v.Proof.Pubkey) == pub {
			return true
		}
	}
	return false
}

func (n *node) findCss(height int64, round int, pub string) bool {
	mp, ok := n.css[height][round]
	if !ok {
		return false
	}
	for _, v := range mp {
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
		ips: make(map[int64]map[int]*pt.Pos33SortMsg),
		ivs: make(map[int64]map[int][]*pt.Pos33SortMsg),
		cps: make(map[int64]map[int]map[string]*pt.Pos33SortMsg),
		cvs: make(map[int64]map[int]map[string][]*pt.Pos33VoteMsg),
		css: make(map[int64]map[int][]*pt.Pos33SortMsg),
		bch: make(chan *types.Block, 16),
	}

	plog.Debug("@@@@@@@ node start:", "conf", conf)
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
	plog.Debug("make a minerTx", "nvs", len(vs), "height", height, "fee", tx.Fee, "from", tx.From())
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

func (n *node) makeBlock(height int64, round int, minHash string, vs []*pt.Pos33VoteMsg) error {
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
	if string(sort.SortHash.Hash) != minHash {
		err := fmt.Errorf("makeBlock minHash error")
		return err
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
	plog.Info("block make", "height", height, "round", round, "ntx", len(nb.Txs), "nvs", len(vs))

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
	if !n.prepareOK(b.Height) {
		return
	}

	lastHeight := n.lastBlock().Height
	if b.Height != lastHeight {
		plog.Error("addBlock height error", "height", b.Height, "lastHeight", lastHeight)
		return
	}

	plog.Info("block add", "height", b.Height, "hash", common.ToHex(b.Hash(n.GetAPI().GetConfig()))[:16])
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

func (n *node) prepareOK(height int64) bool {
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

	sortHeight := b.Height - pt.Pos33SortitionSize
	allw := n.allCount(sortHeight)
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
	hash := act.Sort.SortHash.Hash
	for _, v := range act.Votes {
		err = n.checkVote(v, height, round, seed, allw, string(hash))
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
		plog.Debug("requestBlock error", "height", height-pt.Pos33SortitionSize, "error", err)
		return false
	}
	seed, err := getMinerSeed(b)
	if err != nil {
		plog.Error("reSortition error", "height", height, "round", round, "err", err)
		return false
	}
	const staps = 2
	allw := n.allCount(height - pt.Pos33SortitionSize)
	for s := 0; s < staps; s++ {
		if s == 0 && n.conf.OnlyVoter {
			continue
		}
		sms := n.sort(seed, height, round, s, allw)
		if sms == nil {
			plog.Debug("node.sortition nil", "height", height, "round", round)
			continue
		}
		plog.Debug("node.reSortition", "height", height, "round", round, "step", s, "weight", len(sms))
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
	allw := n.allCount(0)
	for s := 0; s < steps; s++ {
		for i := 0; i < pt.Pos33SortitionSize; i++ {
			height := int64(i + 1)
			sms := n.sort(seed, height, round, s, allw)
			if sms == nil {
				plog.Debug("node.sortition nil", "height", height, "round", round)
				continue
			}
			plog.Debug("node.sortition", "height", height, "round", round, "weight", len(sms), "allw", allw)
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
	plog.Debug("sortition", "height", b.Height, "round", round)
	const steps = 2
	allw := n.allCount(b.Height)
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
			plog.Debug("node.sortition nil", "height", height, "round", round)
			continue
		}
		plog.Debug("node.sortition", "height", height, "round", round, "weight", len(sms), "allw", allw)
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

func (n *node) checkVote(vm *pt.Pos33VoteMsg, height int64, round int, seed []byte, allw int, minHash string) error {
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
	if minHash != string(vm.MinHash) {
		return fmt.Errorf("vote Tid is NOT consistent")
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
	if height < pt.Pos33SortitionSize {
		return zeroHash[:], nil
	}
	sb, err := n.RequestBlock(height)
	if err != nil {
		plog.Debug("request block error", "height", height, "err", err)
		return nil, err
	}
	return getMinerSeed(sb)
}

func (n *node) handleVotesMsg(vms *pt.Pos33Votes, myself bool) {
	lb := n.lastBlock()
	if len(vms.Vs) == 0 {
		plog.Error("votemsg sortition is 0")
		return
	}

	vm := vms.Vs[0]
	if vm.Sort == nil || vm.Sort.Proof == nil || vm.Sort.Proof.Input == nil {
		return
	}
	height := vm.Sort.Proof.Input.Height
	if height <= lb.Height {
		plog.Debug("vote too late")
		return
	}
	if height > lb.Height+pt.Pos33SortitionSize*2 {
		plog.Debug("vote too hight")
		return
	}

	round := int(vm.Sort.Proof.Input.Round)
	if n.lheight == height && n.lround == round {
		return
	}

	if n.findCvs(height, round, string(vm.Sig.Pubkey)) {
		plog.Error("repeat vote msg", "height", height, "round", round, "addr", address.PubKeyToAddr(vm.Sig.Pubkey))
		return
	}

	sortHeight := height - pt.Pos33SortitionSize
	seed, err := n.getSortSeed(sortHeight)
	if err != nil {
		plog.Error("getMinerSeed error", "err", err, "height", height)
		return
	}
	allw := n.allCount(sortHeight)

	minHash := string(vm.MinHash)
	for _, vm := range vms.Vs {
		if !myself {
			err := n.checkVote(vm, height, round, seed, allw, minHash)
			if err != nil {
				plog.Error("check error", "height", height, "round", round, "err", err)
				return
			}
		}
		n.addVote(vm, height, round, minHash)
	}
	vs := n.cvs[height][round][minHash]
	plog.Debug("handleVotesMsg", "height", height, "round", round, "voter", saddr(vm.GetSig()), "votes", len(vs))

	// 如果 block(height, round) 超时，收到票后，检查并 make block
	if round > 0 && height > pt.Pos33SortitionSize && checkVotesEnough(vs, height, round) {
		err := n.makeBlock(height, round, minHash, vs)
		if err != nil {
			plog.Debug("can't make block", "err", err, "height", height, "round", round)
		}
	}
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

	minHash := ""
	max := 0
	for hash, vs := range mp {
		if len(vs) > max {
			max = len(vs)
			minHash = hash
		}
	}
	vs := mp[minHash]
	if checkVotesEnough(vs, height, round) {
		err := n.makeBlock(height, round, minHash, vs)
		if err != nil {
			plog.Debug("can't make block", "err", err, "height", height, "round", round)
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
	return v[i].SortsCount < v[i].SortsCount
}
func (v votes) Swap(i, j int) { v[i], v[j] = v[j], v[i] }

func checkVotesEnough(vs []*pt.Pos33VoteMsg, height int64, round int) bool {
	if len(vs) < pt.Pos33MustVotes {
		if round == 0 {
			plog.Info("block vote < 11", "height", height, "round", round)
		} else {
			plog.Debug("block vote < 11", "height", height, "round", round)
		}
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
	if height > lbHeight+pt.Pos33SortitionSize*2 {
		plog.Debug("handleSort height too hight", "height", height, "lbheight", lbHeight)
		return
	}
	if n.lastBlock().Height >= height {
		err := fmt.Errorf("sort msg too late, lbHeight=%d, sortHeight=%d", n.lastBlock().Height, height)
		plog.Debug("handleSort error", "err", err)
		return
	}
	if n.cps[height] == nil {
		n.cps[height] = make(map[int]map[string]*pt.Pos33SortMsg)
	}
	round := int(m.Proof.Input.Round)
	if n.cps[height][round] == nil {
		n.cps[height][round] = make(map[string]*pt.Pos33SortMsg)
	}
	if n.findCps(height, round, string(m.Proof.Pubkey)) {
		plog.Error("repeat sortition msg", "height", height, "round", round, "addr", address.PubKeyToAddr(m.Proof.Pubkey))
		return
	}
	n.cps[height][round][string(m.SortHash.Hash)] = m
	plog.Debug("handleSortitionMsg", "height", height, "round", round, "size", len(n.cps[height][round]))
}

func (n *node) checkSort(s *pt.Pos33SortMsg, seed []byte, allw, step int) error {
	if s == nil {
		return fmt.Errorf("sortMsg error")
	}
	if s.Proof == nil || s.Proof.Input == nil || s.SortHash == nil {
		return fmt.Errorf("sortMsg error")
	}

	height := s.Proof.Input.Height
	err := n.verifySort(height, step, allw, seed, s)
	if err != nil {
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
	allw := n.allCount(sortHeight)
	var rss []*pt.Pos33SortMsg
	for _, s := range ss {
		err := n.checkSort(s, seed, allw, 1)
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
		plog.Debug("handleSortsMsg err", "err", err)
		return
	}
	if m.S != nil {
		n.handleSortitionMsg(m.S, lb.Height)
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
		if height > lb.Height+pt.Pos33SortitionSize*2 {
			plog.Debug("handleSort height too hight", "height", height, "lbheight", lb.Height)
			// too
			return
		}
		if n.lastBlock().Height >= height {
			err := fmt.Errorf("sort msg too late, lbHeight=%d, sortHeight=%d", n.lastBlock().Height, height)
			plog.Debug("handleSort error", "err", err)
		}
		round := int(s.Proof.Input.Round)
		if n.css[height] == nil {
			n.css[height] = make(map[int][]*pt.Pos33SortMsg)
		}
		if n.findCss(height, round, string(s.Proof.Pubkey)) {
			plog.Error("repeat sortition msg", "height", height, "round", round, "addr", address.PubKeyToAddr(s.Proof.Pubkey))
			return
		}
		ss := n.css[height][round]
		ss = append(ss, s)
		n.css[height][round] = ss
		if i == len(m.Sorts)-1 {
			plog.Debug("handleSortsMsg", "height", height, "round", round, "who", address.PubKeyToAddr(s.Proof.Pubkey), "len(css)", len(ss))
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

	time.AfterFunc(time.Second, func() {
		n.addBlock(lb)
	})

	plog.Info("pos33 running...", "last block height", lb.Height)
	isSync := false
	syncTick := time.NewTicker(time.Second)
	tch := make(chan int64, 1)
	nch := make(chan int64, 1)
	round := 0
	blockTimeout := time.Second * 5
	resortTimeout := time.Second * 3

	for {
		select {
		case <-n.done:
			plog.Debug("pos33 consensus run loop stoped")
			return
		case msg := <-msgch:
			n.handlePos33Msg(msg)
		case <-syncTick.C:
			isSync = n.IsCaughtUp()
		default:
		}
		if !isSync {
			time.Sleep(time.Millisecond * 300)
			continue
		}

		select {
		case height := <-tch:
			if height == n.lastBlock().Height+1 {
				round++
				plog.Info("block timeout", "height", height, "round", round)
				n.reSortition(height, round)
				time.AfterFunc(resortTimeout, func() {
					nch <- height
				})
			}
		case height := <-nch:
			if height == n.lastBlock().Height+1 {
				n.makeNewBlock(height, round)
				time.AfterFunc(blockTimeout, func() {
					tch <- height
				})
			}
		case b := <-n.bch: // new block add to chain
			round = 0
			n.handleNewBlock(b)
			time.AfterFunc(time.Millisecond, func() {
				nch <- b.Height + 1
			})
		default:
			time.Sleep(time.Millisecond * 10)
		}
	}
}

func (n *node) handleNewBlock(b *types.Block) {
	plog.Debug("handleNewBlock", "height", b.Height)
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

	if b.Height > 0 {
		m, err := getMiner(b)
		if err != nil {
			return
		}
		n.bvs = append(n.bvs, len(m.Votes))
		if len(n.bvs) > pt.Pos33SortitionSize*100 {
			n.bvs = n.bvs[1:]
		}
	}
}

func (n *node) makeNewBlock(height int64, round int) {
	n.checkSorts(height, round)
	if round > 0 {
		// if timeout, only vote, handle vote will make new block
		n.vote(height, round)
		return
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
	minHash, pub := n.bp(height, round)
	if minHash == "" {
		plog.Debug("vote bp is nil", "height", height, "round", round)
		return
	}
	ss := n.myVotes(height, round)
	if ss == nil {
		plog.Debug("I'm not verifer", "height", height)
		return
	}
	plog.Info("block vote", "height", height, "round", round, "maker", address.PubKeyToAddr(pub)[:16])
	var vs []*pt.Pos33VoteMsg
	for _, s := range ss {
		v := &pt.Pos33VoteMsg{Sort: s, MinHash: []byte(minHash), SortsCount: uint32(len(n.css[height][round]))}
		priv := n.getPriv()
		if priv == nil {
			panic("can't go here")
		}
		v.Sign(priv)
		vs = append(vs, v)
	}
	v := &pt.Pos33Votes{Vs: vs}
	data := marshalVoteMsg(v)
	if string(n.priv.PubKey().Bytes()) != string(pub) {
		go n.gss.sendto(pub, data)
		n.gss.gossip(pos33Topic, data)
	}
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
