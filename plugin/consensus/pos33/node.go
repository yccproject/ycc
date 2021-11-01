package pos33

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/33cn/chain33/common"
	"github.com/33cn/chain33/common/address"
	"github.com/33cn/chain33/common/crypto"
	"github.com/33cn/chain33/common/log/log15"
	"github.com/33cn/chain33/common/merkle"
	"github.com/33cn/chain33/types"
	"github.com/golang/protobuf/proto"
	pt "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

var plog = log15.New("module", "pos33")

const pos33Topic = "ycc-pos33"

type alterBlock struct {
	n  int
	bs []*pt.Pos33BlockMsg
}

func (ab *alterBlock) add(nb *pt.Pos33BlockMsg) bool {
	for _, b := range ab.bs {
		if string(b.B.TxHash) == string(nb.B.TxHash) {
			return false
		}
	}
	ab.bs = append(ab.bs, nb)
	return true
}

// 区块制作人
type maker struct {
	my       *pt.Pos33SortMsg              // 我的抽签
	mvs      map[string][]*pt.Pos33VoteMsg // 我收到的 maker vote
	n        *node
	selected bool
	ok       bool
}

// 验证委员会
type committee struct {
	myss [3][]*pt.Pos33SortMsg               // 我的抽签
	mss  map[string]*pt.Pos33SortMsg         // 我接收到的maker 的抽签
	css  map[int]map[string]*pt.Pos33SortMsg // 我收到committee的抽签
	ssmp map[string]*pt.Pos33SortMsg
	svmp map[string]int // 验证委员会的投票
	ab   *alterBlock    // 我接收到所有备选block
	bvs  map[string][]*pt.Pos33VoteMsg
	ok   bool
}

func (n *node) getmaker(height int64, round int) *maker {
	rmp, ok := n.vmp[height]
	if !ok {
		rmp = make(map[int]*maker)
		n.vmp[height] = rmp
	}
	v, ok := rmp[round]
	if !ok {
		v = &maker{
			mvs: make(map[string][]*pt.Pos33VoteMsg),
		}
		rmp[round] = v
	}
	return v
}

func (c *committee) setCommittee(height int64) {
	for k, n := range c.svmp {
		if n < pt.Pos33MustVotes {
			delete(c.svmp, k)
		}
	}
	plog.Info("committee len", "len", len(c.svmp), "height", height)
}

func (c *committee) myCommitteeSort(pub string) []*pt.Pos33SortMsg {
	var mss []*pt.Pos33SortMsg
	for _, ss := range c.myss {
		for _, s := range ss {
			_, ok := c.svmp[string(s.SortHash.Hash)]
			if ok {
				mss = append(mss, s)
			}
		}
	}
	if len(mss) == 0 {
		return c.getMySorts(pub)
	}
	return mss
}

func (n *node) getCommittee(height int64, round int) *committee {
	rmp, ok := n.mmp[height]
	if !ok {
		rmp = make(map[int]*committee)
		n.mmp[height] = rmp
	}
	m, ok := rmp[round]
	if !ok {
		m = &committee{
			mss:  make(map[string]*pt.Pos33SortMsg),
			css:  make(map[int]map[string]*pt.Pos33SortMsg),
			ssmp: make(map[string]*pt.Pos33SortMsg),
			svmp: make(map[string]int),
			bvs:  make(map[string][]*pt.Pos33VoteMsg),
			ab:   &alterBlock{},
		}
		rmp[round] = m
	}
	return m
}

// const (
// 	start = iota
// 	sortOk
// 	makeBlockOk
// 	voteBlockOk
// 	setBlockOk
// )

func (c *committee) getMySorts(pub string) []*pt.Pos33SortMsg {
	ssmp := c.getCommitteeSorts()
	var ss []*pt.Pos33SortMsg
	for _, s := range ssmp {
		if pub == string(s.Proof.Pubkey) {
			ss = append(ss, s)
		}
	}
	return ss
}

// func (c *committee) checkSors() {
// 	for i := 0; i < 3; i++ {
// 		for k, s := range c.css[i] {
// 			err := c.n.checkSort(s, Voter)
// 			if err != nil {
// 				plog.Error("checkSort error:", "err", err)
// 				delete(c.css[i], k)
// 			}
// 		}
// 	}
// }

func getSorts(mp map[string]*pt.Pos33SortMsg, num int) []*pt.Pos33SortMsg {
	var ss []*pt.Pos33SortMsg
	for _, s := range mp {
		ss = append(ss, s)
	}
	if len(ss) > num {
		sort.Sort(pt.Sorts(ss))
		ss = ss[:num]
	}
	return ss
}

func (m *maker) checkVotes(height int64, vs []*pt.Pos33VoteMsg) (int, error) {
	if height > 0 && len(vs) < pt.Pos33VoterSize/2+1 {
		return 0, errors.New("checkVotes error: NOT enough votes")
	}
	return len(vs), nil
}

func (c *committee) checkVotes(vs []*pt.Pos33VoteMsg) (int, error) {
	if len(vs) < pt.Pos33VoterSize/2+1 {
		return 0, errors.New("checkVotes error: NOT enough votes")
	}
	return len(vs), nil
}

// func (m *maker) checkVotes0(vs []*pt.Pos33VoteMsg) (int, error) {
// 	mp := make(map[string]int)
// 	for k, c := range m.svmp {
// 		if c >= pt.Pos33MustVotes {
// 			mp[k] = c
// 		}
// 	}
// 	if len(mp) < pt.Pos33MustVotes {
// 		return 0, errors.New("maker.checkVotes error: len(mp) < 17")
// 	}
// 	n := 0
// 	for _, v := range vs {
// 		_, ok := mp[string(v.Sort.SortHash.Hash)]
// 		if !ok {
// 			continue
// 		}
// 		n++
// 	}
// 	if n < pt.Pos33MustVotes {
// 		return len(mp), errors.New("maker.checkVotes error: len(vs) < 2/3 len(mp)")
// 	}
// 	return len(mp), nil
// }

func (c *committee) getCommitteeSorts() map[string]*pt.Pos33SortMsg {
	if len(c.ssmp) > 0 {
		return c.ssmp
	}
	// c.checkSors()
	num := int(pt.Pos33VoterSize)

	var ss []*pt.Pos33SortMsg
	for i := 0; num > 0 && i < 3; i++ {
		ss1 := getSorts(c.css[i], num)
		ss = append(ss, ss1...)
		num -= len(ss1)
	}

	for _, s := range ss {
		c.ssmp[string(s.SortHash.Hash)] = s
	}
	return c.ssmp
}

/*
func (m *maker) getNum() int {
	min := 100
	num := -1
	for i := 0; i < 3; i++ {
		mp, ok := m.vss[i]
		if !ok {
			continue
		}
		dlt := int(math.Abs(float64(len(mp) - pt.Pos33VoterSize)))
		if dlt < min {
			min = dlt
			num = i
		}
	}
	return num
}
*/

func (m *maker) findVm(key, pub string) bool {
	return find(m.mvs, key, pub)
}

func (m *committee) findVb(key, pub string) bool {
	return find(m.bvs, key, pub)
}

// func (m *committee) rmBvByPub(pub string, r int) {
// 	for key, vs := range m.bvs {
// 		if !m.findVb(key, pub) {
// 			continue
// 		}
// 		var nvs []*pt.Pos33VoteMsg
// 		for _, v := range vs {
// 			if string(v.Sig.Pubkey) != pub {
// 				nvs = append(nvs, v)
// 			}
// 		}
// 		m.bvs[key] = nvs
// 	}
// }

func find(vmp map[string][]*pt.Pos33VoteMsg, key, pub string) bool {
	vs, ok := vmp[key]
	if !ok {
		return false
	}
	for _, v := range vs {
		if string(v.Sig.Pubkey) == pub {
			return true
		}
	}
	return false
}

type node struct {
	*Client
	gss *gossip2

	vmp map[int64]map[int]*maker
	mmp map[int64]map[int]*committee
	bch chan *types.Block
	vch chan hr
	sch chan hr

	maxSortHeight int64
	pid           string
	topic         string
}

func newNode(conf *subConfig) *node {
	return &node{
		mmp: make(map[int64]map[int]*committee),
		vmp: make(map[int64]map[int]*maker),
		bch: make(chan *types.Block, 16),
		vch: make(chan hr, 1),
		sch: make(chan hr, 1),
	}
}

func (n *node) lastBlock() *types.Block {
	b, err := n.RequestLastBlock()
	if err != nil {
		b = n.GetCurrentBlock()
	}
	return b
}

func (n *node) minerTx(height int64, sm *pt.Pos33SortMsg, vs []*pt.Pos33VoteMsg, priv crypto.PrivKey) (*types.Transaction, error) {
	if len(vs) > pt.Pos33VoterSize {
		sort.Sort(pt.Votes(vs))
		vs = vs[:pt.Pos33VoterSize]
	}
	act := &pt.Pos33TicketAction{
		Value: &pt.Pos33TicketAction_Miner{
			Miner: &pt.Pos33TicketMiner{
				Vs:        vs,
				Sort:      sm,
				BlockTime: time.Now().UnixNano() / 1000000,
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

func (n *node) newBlock(lastBlock *types.Block, txs []*types.Transaction, height int64) (*types.Block, error) {
	if lastBlock.Height+1 != height {
		plog.Error("newBlock height error", "lastHeight", lastBlock.Height, "height", height)
		return nil, fmt.Errorf("the last block too low")
	}

	bt := time.Now().Unix()
	if bt < lastBlock.GetBlockTime() {
		bt = lastBlock.GetBlockTime()
	}

	cfg := n.GetAPI().GetConfig()
	nb := &types.Block{
		ParentHash: lastBlock.Hash(cfg),
		Height:     lastBlock.Height + 1,
		BlockTime:  bt,
	}

	maxTxs := int(cfg.GetP(height).MaxTxNumber)
	txs = append(txs, n.RequestTx(maxTxs, nil)...)
	txs = n.AddTxsToBlock(nb, txs)

	nb.Txs = txs
	nb.TxHash = merkle.CalcMerkleRoot(cfg, lastBlock.Height, txs)
	return nb, nil
}

func (n *node) makeBlock(height int64, round int, sort *pt.Pos33SortMsg, vs []*pt.Pos33VoteMsg) (*types.Block, error) {
	priv := n.getPriv()
	if priv == nil {
		panic("can't go here")
	}

	tx, err := n.minerTx(height, sort, vs, priv)
	if err != nil {
		return nil, err
	}

	lb, err := n.RequestBlock(height - 1)
	if err != nil {
		return nil, err
	}
	nb, err := n.newBlock(lb, []*Tx{tx}, height)
	if err != nil {
		return nil, err
	}

	nb.Difficulty = n.blockDiff(lb, len(vs))
	plog.Info("block make", "height", height, "round", round, "ntx", len(nb.Txs), "nvs", len(vs), "hash", common.HashHex(nb.Hash(n.GetAPI().GetConfig()))[:16])

	nb = n.PreExecBlock(nb, false)
	if nb == nil {
		return nil, errors.New("PreExccBlock error")
	}
	return nb, nil
}

func (n *node) broadcastBlock(b *types.Block) {
	txs := b.Txs
	b.Txs = nil
	nb := b.Clone()
	b.Txs = txs
	nb.Txs = txs[:1]
	m := &pt.Pos33BlockMsg{B: b, Pid: n.pid}
	n.handleBlockMsg(m, true)

	nm := &pt.Pos33BlockMsg{B: nb, Pid: n.pid}
	pm := &pt.Pos33Msg{Data: types.Encode(nm), Ty: pt.Pos33Msg_B}
	data := types.Encode(pm)
	// n.gss.forwad(data)
	n.gss.gossip(n.topic+"/block", data)
}

func (n *node) addBlock(b *types.Block) {
	if b.Height > 0 {
		lb, err := n.RequestBlock(b.Height - 1)
		if err != nil {
			panic("can't go here")
		}
		plog.Info("block add", "height", b.Height, "hash", common.ToHex(b.Hash(n.GetAPI().GetConfig()))[:16])
		if b.BlockTime-lb.BlockTime < 1 {
			time.AfterFunc(time.Millisecond*500, func() {
				n.pushBlock(b)
			})
			return
		}
	}
	n.pushBlock(b)
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
		if h < height-20 {
			delete(n.mmp, h)
		}
	}

	for h := range n.vmp {
		if h < height-20 {
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
	// err := n.blockCheck(b)
	// if err != nil {
	// 	plog.Error("blockCheck error", "err", err, "height", b.Height)
	// 	return err
	// }
	return nil
}

func (n *node) checkVotes(vs []*pt.Pos33VoteMsg, hash []byte, h int64, checkEnough, checkCommittee bool) error {
	v0 := vs[0]
	height := v0.Sort.Proof.Input.Height
	round := v0.Sort.Proof.Input.Round

	if checkEnough {
		if len(vs) < pt.Pos33MustVotes {
			return errors.New("checkVotes error: NOT enough votes")
		}
	}

	comm := n.getCommittee(height, int(round))

	for _, v := range vs {
		ht := v.Sort.Proof.Input.Height
		rd := v.Sort.Proof.Input.Round
		if ht != height || rd != round {
			return errors.New("checkVotes error: height, round or num NOT same")
		}
		if checkCommittee && len(comm.svmp) >= pt.Pos33MustVotes {
			_, ok := comm.svmp[string(v.Sort.SortHash.Hash)]
			if !ok {
				return errors.New("checkvotes error: sort hash NOT in committee")
			}
		}
		err := n.checkVote(v, hash)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *node) checkVote(v *pt.Pos33VoteMsg, hash []byte) error {
	if !v.Verify() {
		return errors.New("checkVote verify false")
	}
	if string(v.Hash) != string(hash) {
		return errors.New("vote hash NOT right")
	}
	return n.checkSort(v.Sort, Voter)
}

func (n *node) blockCheck(b *types.Block) error {
	height := b.Height
	pb, err := n.RequestBlock(height - 1)
	if err != nil {
		plog.Error("blockCheck error:", "err", err, "height", height)
		return err
	}
	if string(b.ParentHash) != string(pb.Hash(n.GetAPI().GetConfig())) {
		plog.Error("blockCheck error", "err", "parentHash not match", "height", height, "ph", common.HashHex(b.ParentHash)[:16])
		return err
	}

	act, err := getMiner(b)
	if err != nil {
		return err
	}
	if act.Sort == nil || act.Sort.Proof == nil || act.Sort.Proof.Input == nil {
		return fmt.Errorf("miner tx error")
	}
	round := int(act.Sort.Proof.Input.Round)
	comm := n.getCommittee(b.Height, round)
	cfg := n.GetAPI().GetConfig()
	ok := false
	for _, vb := range comm.ab.bs {
		if string(vb.B.Hash(cfg)) == string(b.Hash(cfg)) {
			ok = true
			break
		}
	}
	if ok {
		plog.Info("block already check", "height", b.Height, "from", b.Txs[0].From()[:16])
		return nil
	}
	plog.Info("block check", "height", b.Height, "from", b.Txs[0].From()[:16])
	err = n.checkSort(act.Sort, 0)
	if err != nil {
		plog.Error("blockCheck error", "err", err, "height", b.Height, "round", round)
		return err
	}

	err = n.checkVotes(act.Vs, act.Sort.SortHash.Hash, b.Height, true, true)
	if err != nil {
		plog.Error("blockCheck check vs error", "err", err, "height", b.Height, "round", round)
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
		plog.Error("requestBlock error", "height", height-pt.Pos33SortBlocks, "error", err)
		return false
	}
	seed, err := getMinerSeed(b)
	if err != nil {
		plog.Error("reSortition error", "height", height, "round", round, "err", err)
		return false
	}
	n.sortMaker(seed, height, round)
	n.sortCommittee(seed, height, round)
	return true
}

func (n *node) sortition(b *types.Block, round int) {
	seed, err := getMinerSeed(b)
	height := b.Height + pt.Pos33SortBlocks
	if err != nil {
		plog.Error("reSortition error", "height", height, "round", round, "err", err)
		return
	}
	if height > 10 && len(n.mmp) > 10 || n.GetAPI().GetConfig().GetModuleConfig().BlockChain.SingleMode {
		n.sortMaker(seed, height, round)
		n.sortCommittee(seed, height, round)
	}
}

func (n *node) firstSortition() {
	seed := zeroHash[:]
	for i := 0; i <= pt.Pos33SortBlocks; i++ {
		height := int64(i)
		n.sortMaker(seed, height, 0)
		n.sortCommittee(seed, height, 0)
	}
}

func (n *node) sortCommittee(seed []byte, height int64, round int) {
	var vss []*pt.Pos33Sorts
	c := n.getCommittee(height, round)
	for i := 0; i < 3; i++ {
		ss := n.voterSort(seed, height, round, Voter, i)
		if len(ss) == 0 {
			continue
		}
		c.myss[i] = ss
		vss = append(vss, &pt.Pos33Sorts{Sorts: ss})
	}
	n.sendVoterSort(vss, height, round, int(pt.Pos33Msg_VS))
}

func (n *node) sortMaker(seed []byte, height int64, round int) {
	plog.Debug("sortMaker", "height", height, "round", round)
	if n.conf.OnlyVoter {
		return
	}
	s := n.makerSort(seed, height, round)
	if s == nil {
		return
	}
	m := n.getmaker(height, round)
	m.my = s
	n.sendMakerSort(m.my, height, round)
}

func (n *node) getSortSeed(height int64) ([]byte, error) {
	if height < pt.Pos33SortBlocks {
		return zeroHash[:], nil
	}
	sb, err := n.RequestBlock(height)
	if err != nil {
		plog.Error("request block error", "height", height, "err", err)
		return nil, err
	}
	return getMinerSeed(sb)
}

func (n *node) getDiff(height int64, round int, isMaker bool) float64 {
	height -= pt.Pos33SortBlocks
	w := n.allCount(height)
	size := pt.Pos33MakerSize
	if !isMaker {
		size = pt.Pos33VoterSize
	}
	diff := float64(size) / float64(w)
	diff *= math.Pow(1.1, float64(round))
	return diff
}

func (n *node) handleVoterSorts(ms []*pt.Pos33Sorts, myself bool, ty int) {
	for _, m := range ms {
		n.handleVoterSort(m.Sorts, myself, ty)
	}
}

func (n *node) handleVoterSort(ss []*pt.Pos33SortMsg, myself bool, ty int) bool {
	if len(ss) == 0 {
		return false
	}
	s0 := ss[0]
	if s0.Proof == nil || s0.Proof.Input == nil || s0.SortHash == nil {
		plog.Error("handleSortitionMsg error, input msg is nil")
		return false
	}

	height := s0.Proof.Input.Height
	if height > 0 && height <= n.lastBlock().Height {
		return false
	}

	round := int(s0.Proof.Input.Round)
	num := int(s0.SortHash.Num)
	if num >= 3 {
		plog.Error("handleVoterSort error: sort num >=3", "height", height, "round", round, "num", num, "addr", address.PubKeyToAddr(s0.Proof.Pubkey)[:16])
		return false
	}

	comm := n.getCommittee(height, round)
	mp, ok := comm.css[num]
	if !ok {
		mp = make(map[string]*pt.Pos33SortMsg)
		comm.css[num] = mp
	}

	for _, s := range mp {
		if string(s.Proof.Pubkey) == string(s0.Proof.Pubkey) {
			return true
		}
	}

	for _, s := range ss {
		// if !myself {
		// 	err := n.checkSort(s, BlockVoter)
		// 	if err != nil {
		// 		plog.Error("checkSort error", "err", err, "height", height)
		// 		return false
		// 	}
		// }
		// if num != int(s.SortHash.Num) {
		// 	plog.Error("handleVoterSort error: num Not match", "height", height, "round", round)
		// 	return false
		// }
		mp[string(s.SortHash.Hash)] = s
	}
	// plog.Info("handleVoterSort", "all", len(comm.css[num]), "nvs", len(ss), "height", height, "round", round, "num", num, "ty", ty, "addr", address.PubKeyToAddr(s0.Proof.Pubkey)[:16])
	return true
}

func (n *node) handleMakerVotes(mvs []*pt.Pos33Votes, myself bool, ty int) {
	for _, vs := range mvs {
		n.n.handleVoteMsg(vs.Vs, myself, ty)
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
	num := int(m0.Sort.SortHash.Num)
	if num >= 3 {
		return
	}

	if n.lastBlock().Height > height {
		return
	}

	comm := n.getCommittee(height, round)
	maker := n.getmaker(height, round)

	// repeat msg
	if ty == int(pt.Pos33Msg_BV) {
		// maker.rmBvByPub(string(m0.Sig.Pubkey), int(m0.Round))
		if comm.findVb(string(m0.Hash), string(m0.Sig.Pubkey)) {
			return
		}
		// 检查投票是否属于委员会, 去除非委员会投票
		if len(comm.svmp) >= pt.Pos33MustVotes { // 兼容旧版本
			var vs []*pt.Pos33VoteMsg
			for _, v := range ms {
				_, ok := comm.svmp[string(v.Sort.SortHash.Hash)]
				if ok {
					vs = append(vs, v)
				} else {
					plog.Info("handleVoteMsg vote Not in committee", "height", height)
				}
			}
			ms = vs
		}
	} else if ty == int(pt.Pos33Msg_MV) {
		if maker.findVm(string(m0.Hash), string(m0.Sig.Pubkey)) {
			return
		}
	}
	if len(ms) == 0 {
		return
	}

	err := n.checkVotes(ms, m0.Hash, height, false, false)
	if err != nil {
		plog.Error("checkVotes error", "err", err, "height", height)
		return
	}

	for _, m := range ms {
		if m.Round != m0.Round {
			return
		}
		if string(m.Hash) != string(m0.Hash) {
			return
		}

		if ty == int(pt.Pos33Msg_BV) {
			comm.bvs[string(m.Hash)] = append(comm.bvs[string(m.Hash)], m)
		} else if ty == int(pt.Pos33Msg_MV) {
			maker.mvs[string(m.Hash)] = append(maker.mvs[string(m.Hash)], m)
		}
	}

	if ty == int(pt.Pos33Msg_BV) {
		vs := comm.bvs[string(m0.Hash)]
		if len(vs) >= 10 { //pt.Pos33MustVotes {
			plog.Info("handleVoteMsg block", "hash", common.HashHex(m0.Hash)[:16], "allbvs", len(vs), "nvs", len(ms), "height", height, "round", round, "ty", ty, "addr", address.PubKeyToAddr(m0.Sig.Pubkey)[:16])
		}
		if len(vs) < pt.Pos33VoterSize/2+1 {
			return
		}
		n.trySetBlock(height, round, vs, string(m0.Hash))
		// 如果下一个高度被选中出块，但是没有收集到这个高度足够的投票，那么会尝试制作下一个区块
		lvs := comm.bvs[string(m0.Hash)]
		n.tryMakeNextBlock(height+1, lvs)
	} else {
		vs := maker.mvs[string(m0.Hash)]
		if len(vs) >= 10 {
			plog.Debug("handleVoteMsg maker", "hash", common.HashHex(m0.Hash)[:16], "allmvs", len(vs), "nvs", len(ms), "height", height, "round", round, "ty", ty, "addr", address.PubKeyToAddr(m0.Sig.Pubkey)[:16])
		}
		if round > 0 {
			n.tryMakeBlock(height, round)
		}
	}
}

func (n *node) tryMakeNextBlock(height int64, lvs []*pt.Pos33VoteMsg) {
	maker := n.getmaker(height, 0)
	if maker.ok {
		return
	}
	if !maker.selected {
		return
	}
	plog.Info("tryMakeNextBlock", "height", height, "nlvs", len(lvs))
	if len(lvs) < pt.Pos33MustVotes {
		return
	}
	nb, err := n.makeBlock(height, 0, maker.my, lvs)
	if err != nil {
		plog.Error("makeBlock error", "err", err, "height", height)
		return
	}
	n.broadcastBlock(nb)
	maker.ok = true
}

func (n *node) tryMakeBlock(height int64, round int) {
	maker := n.getmaker(height, round)
	if maker.my == nil {
		return
	}
	if maker.ok {
		return
	}
	vs := maker.mvs[string(maker.my.SortHash.Hash)]
	nvs := len(vs)

	plog.Info("try make block", "height", height, "round", round, "nvs", nvs)

	_, err := maker.checkVotes(height, vs)
	if err != nil {
		plog.Error("tryMakerBlock checkVotes error", "err", err, "height", height, "round", round)
		return
	}

	maker.selected = true

	lb, err := n.RequestBlock(height - 1)
	if err != nil {
		plog.Error("tryMakeBlock error", "err", err)
		return
	}

	lr := 0
	if lb.Height > 0 {
		lm, err := getMiner(lb)
		if err != nil {
			plog.Error("tryMakeBlock error", "err", err)
			return
		}
		lr = int(lm.Sort.Proof.Input.Round)
	}

	lcomm := n.getCommittee(height-1, lr)
	lh := lb.Hash(n.GetAPI().GetConfig())
	lvs := lcomm.bvs[string(lh)]
	if round <= 1 {
		_, err := lcomm.checkVotes(lvs)
		if err != nil {
			plog.Error("tryMakerBlock checklastVotes error", "err", err, "phash", common.HashHex(lh)[:16], "height", height, "round", round, "lnvs", len(lvs))
			return
		}
	}

	nb, err := n.makeBlock(height, round, maker.my, lvs)
	if err != nil {
		plog.Error("makeBlock error", "err", err, "height", height)
		return
	}
	n.broadcastBlock(nb)
	maker.ok = true
}

func (n *node) trySetBlock(height int64, round int, vs []*pt.Pos33VoteMsg, bh string) bool {
	if n.lastBlock().Height >= height {
		return true
	}

	comm := n.getCommittee(height, round)
	if comm.ok {
		return true
	}

	plog.Info("try set block", "height", height, "round", round, "nvs", len(vs), "bh", common.HashHex([]byte(bh))[:16])

	_, err := comm.checkVotes(vs)
	if err != nil {
		return false
	}

	// 把相应的block写入链
	for _, b := range comm.ab.bs {
		h := string(b.B.Hash(n.GetAPI().GetConfig()))
		if bh == h {
			plog.Info("set block", "height", height, "round", round, "bh", common.HashHex([]byte(h))[:16])
			if b.B.Txs[0].From() == address.PubKeyToAddr(n.priv.PubKey().Bytes()) {
				n.setBlock(b.B)
			}
			comm.ok = true
			return true
		}
	}
	return false
}

func (n *node) handleBlockMsg(m *pt.Pos33BlockMsg, myself bool) {
	height := m.B.Height
	miner, err := getMiner(m.B)
	if err != nil {
		plog.Error("getMiner error", "err", err)
		return
	}

	if height > 10 && !checkTime(miner.BlockTime) {
		plog.Error("checkTime error", "height", height)
		return
	}

	round := int(miner.Sort.Proof.Input.Round)
	comm := n.getCommittee(height, round)
	if !comm.ab.add(m) {
		return
	}

	hash := m.B.Hash(n.GetAPI().GetConfig())
	plog.Info("handleBlock", "height", height, "round", round, "ntx", len(m.B.Txs), "bh", common.HashHex(hash)[:16], "addr", address.PubKeyToAddr(miner.Sort.Proof.Pubkey)[:16], "time", time.Now().Format("15:04:05.00000"))
	if comm.ab.n == 0 {
		comm.ab.n = 1
		time.AfterFunc(voteBlockWait, func() {
			n.vch <- hr{height, round}
		})
	}
}

func checkTime(t int64) bool {
	mt := time.Now().UnixNano() / 1000000
	if mt-t > 3000 || t-mt > 2000 {
		plog.Info("checkTime false", "t", t, "mt", mt, "mt-t", mt-t)
		return false
	}
	return true
}

// func (n *node) setSorts(height int64, round int) {
// 	maker := n.getCommittee(height, round)
// 	for k, c := range maker.svmp {
// 		if c < pt.Pos33MustVotes {
// 			delete(maker.svmp, k)
// 		}
// 	}
// }

func (n *node) handleCommittee(m *pt.Pos33SortsVote, self bool) {
	if !self && !m.Verify() {
		plog.Error("handleVotesCount error, signature verify false")
		return
	}

	height := m.Height
	for _, s := range m.MySorts {
		if string(m.Sig.Pubkey) != string(s.Proof.Pubkey) {
			return
		}
		err := n.checkSort(s, Voter)
		if err != nil {
			plog.Error("checkSort error", "err", err, "height", height)
			return
		}
		found := false
		for _, h := range m.SelectSorts {
			if string(s.SortHash.Hash) == string(h) {
				found = true
				break
			}
		}
		if !found {
			return
		}
	}
	round := int(m.Round)
	comm := n.getCommittee(height, round)
	for _, h := range m.SelectSorts {
		comm.svmp[string(h)] += len(m.MySorts)
	}
	plog.Info("handleCommittee", "height", height)
}

func (n *node) voteCommittee(height int64, round int) {
	comm := n.getCommittee(height, round)
	css := comm.getCommitteeSorts()
	var ss [][]byte
	for k := range css {
		ss = append(ss, []byte(k))
	}

	m := &pt.Pos33SortsVote{
		MySorts:     comm.getMySorts(string(n.priv.PubKey().Bytes())),
		SelectSorts: ss,
		Height:      height,
		Round:       int32(round),
	}
	m.Sign(n.priv)
	n.handleCommittee(m, true)

	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_CV,
	}
	data := types.Encode(pm)
	n.gss.gossip(n.topic+"/committee", data)
}

func (n *node) voteMaker(height int64, round int) {
	comm := n.getCommittee(height, round)
	n.voteCommittee(height, round)

	var mss []*pt.Pos33SortMsg
	for _, s := range comm.mss {
		mss = append(mss, s)
	}

	if len(mss) == 0 {
		return
	}
	sort.Sort(pt.Sorts(mss))

	myss := comm.getMySorts(string(n.priv.PubKey().Bytes()))

	var mvs []*pt.Pos33Votes
	for i, s := range mss {
		if i == 3 {
			break
		}
		var vs []*pt.Pos33VoteMsg
		for _, mys := range myss {
			v := &pt.Pos33VoteMsg{
				Hash: s.SortHash.Hash,
				Sort: mys,
			}
			v.Sign(n.priv)
			vs = append(vs, v)
		}
		if len(vs) == 0 {
			continue
		}
		mvs = append(mvs, &pt.Pos33Votes{Vs: vs})
		plog.Info("vote maker", "addr", address.PubKeyToAddr(s.Proof.Pubkey)[:16], "height", height, "round", round)
	}
	if len(mvs) == 0 {
		return
	}
	n.sendMaketVotes(mvs, int(pt.Pos33Msg_MV))
}

func (n *node) sendMaketVotes(mvs []*pt.Pos33Votes, ty int) {
	m := &pt.Pos33MakerVotes{Mvs: mvs}
	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_Ty(ty),
	}
	data := types.Encode(pm)
	n.gss.gossip(n.topic+"/makervotes", data)
	n.handleMakerVotes(mvs, true, ty)
}

func (n *node) sendVote(vs []*pt.Pos33VoteMsg, ty int) {
	m := &pt.Pos33Votes{Vs: vs}
	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_Ty(ty),
	}
	data := types.Encode(pm)
	n.gss.gossip(n.topic+"/blockvotes", data)
	if ty == int(pt.Pos33Msg_BV) {
		n.gss.forwad(data)
	}
	n.handleVoteMsg(vs, true, ty)
}

const voteBlockWait = time.Millisecond * 700

// const voteBlockDeadline = time.Millisecond * 900

func (n *node) voteBlock(height int64, round int) {
	comm := n.getCommittee(height, round)
	// if ab.n < len(ab.bs) {
	// 	ab.n = len(ab.bs)
	// 	time.AfterFunc(voteBlockWait, func() {
	// 		n.vch <- hr{height, round}
	// 	})
	// 	return
	// }
	comm.setCommittee(height)

	minHash := ""
	for _, b := range comm.ab.bs {
		err := n.blockCheck(b.B)
		if err != nil {
			plog.Error("boteBlock error", "height", b.B.Height, "err", err)
			continue
		}

		h := string(b.B.Hash(n.GetAPI().GetConfig()))
		if minHash == "" {
			minHash = h
		}
		if h < minHash {
			minHash = h
		}
	}
	if minHash == "" {
		return
	}
	n.voteBlockHash([]byte(minHash), height, round)
}

// func (n *node) revoteBlock(bh []byte, height int64, round int) {
// 	maker := n.getCommittee(height, round)
// 	if maker.findVb(string(bh), string(n.priv.PubKey().Bytes())) {
// 		return
// 	}

// 	plog.Info("revoteBlock", "height", height, "round", round, "bh", common.HashHex(bh)[:16])
// 	n.voteBlockHash(bh, height, round)
// }

func (n *node) voteBlockHash(bh []byte, height int64, round int) {
	comm := n.getCommittee(height, round)
	var myss []*pt.Pos33SortMsg
	if height <= 10 {
		myss = comm.getMySorts(string(n.priv.PubKey().Bytes()))
	} else {
		myss = comm.myCommitteeSort(string(n.priv.PubKey().Bytes()))
	}
	if len(myss) == 0 {
		plog.Info("i am NOT committee", "height", height)
		return
	}
	// if maker.status >= voteBlockOk {
	// 	return
	// }
	// maker.status = voteBlockOk

	var vs []*pt.Pos33VoteMsg
	for _, mys := range myss {
		v := &pt.Pos33VoteMsg{
			Hash: bh,
			Sort: mys,
		}
		v.Sign(n.priv)
		vs = append(vs, v)
	}
	plog.Info("vote block hash", "height", height, "round", round, "bh", common.HashHex(bh)[:16], "nvs", len(vs))
	if len(vs) == 0 {
		return
	}
	n.sendVote(vs, int(pt.Pos33Msg_BV))
}

func (n *node) handleMakerSort(m *pt.Pos33SortMsg, myself bool) {
	if m == nil || m.Proof == nil || m.Proof.Input == nil || m.SortHash == nil {
		plog.Error("handleSortitionMsg error, input msg is nil")
		return
	}
	height := m.Proof.Input.Height
	if !myself {
		if height > n.lastBlock().Height+pt.Pos33SortBlocks*2 {
			plog.Error("handleSort height too hight", "height", height)
			return
		}
		if n.lastBlock().Height >= height {
			err := fmt.Errorf("sort msg too late, lbHeight=%d, sortHeight=%d", n.lastBlock().Height, height)
			plog.Error("handleSort error", "err", err)
			return
		}
		if !checkTime(m.SortHash.Time) {
			plog.Error("handleSort time NOT right", "height", height, "addr", address.PubKeyToAddr(m.Proof.Pubkey)[:16])
			return
		}
	}
	round := int(m.Proof.Input.Round)
	comm := n.getCommittee(height, round)
	comm.mss[string(m.SortHash.Hash)] = m
	if round > 0 && height > n.maxSortHeight {
		n.maxSortHeight = height
	}
	plog.Debug("handleMakerSort", "nmss", len(comm.mss), "height", height, "round", round, "addr", address.PubKeyToAddr(m.Proof.Pubkey)[:16])
}

func (n *node) checkSort(s *pt.Pos33SortMsg, ty int) error {
	height := s.Proof.Input.Height
	seed, err := n.getSortSeed(height - pt.Pos33SortBlocks)
	if err != nil {
		plog.Error("getSeed error", "err", err)
		return err
	}
	if s == nil {
		return fmt.Errorf("sortMsg error")
	}
	if s.Proof == nil || s.Proof.Input == nil || s.SortHash == nil {
		return fmt.Errorf("sortMsg error")
	}

	err = n.verifySort(height, ty, seed, s)
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
	case pt.Pos33Msg_VS:
		var m pt.Pos33VoteSorts
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleVoterSorts(m.VoteSorts, false, int(pm.Ty))
	case pt.Pos33Msg_MV:
		var m pt.Pos33MakerVotes
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleMakerVotes(m.Mvs, false, int(pm.Ty))
	case pt.Pos33Msg_BV:
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
	case pt.Pos33Msg_CV:
		var m pt.Pos33SortsVote
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleCommittee(&m, false)
	default:
		panic("not support this message type")
	}

	return true
}

type crp struct {
	m  sync.Mutex
	mp map[string]struct{}
	s  []string
}

func newCrp(n int) *crp {
	return &crp{
		mp: make(map[string]struct{}, n*2),
		s:  make([]string, n*2),
	}
}

func (c *crp) add(k string) {
	c.m.Lock()
	defer c.m.Unlock()
	c.mp[k] = struct{}{}
	c.s = append(c.s, k)
}

func (c *crp) find(k string) bool {
	c.m.Lock()
	defer c.m.Unlock()
	_, ok := c.mp[k]
	return ok
}

func (c *crp) findNotAdd(k string) bool {
	c.m.Lock()
	defer c.m.Unlock()

	_, ok := c.mp[k]
	if ok {
		return false
	}

	c.mp[k] = struct{}{}
	c.s = append(c.s, k)
	return true
}

func (c *crp) rm(l int) {
	c.m.Lock()
	defer c.m.Unlock()

	r := len(c.s) - l
	plog.Info("delete some cache", "len", r)
	if l <= 0 {
		return
	}
	for i, k := range c.s {
		if i >= r {
			break
		}
		delete(c.mp, k)
	}
	c.s = c.s[r:]
}

// handleGossipMsg multi-goroutine verify pos33 message
func (n *node) handleGossipMsg() chan *pt.Pos33Msg {
	num := 4
	// cr := newCrp(2048)
	ch := make(chan *pt.Pos33Msg, num*128)
	for i := 0; i < num; i++ {
		go func() {
			for {
				data := <-n.gss.C
				// hash := crypto.Sha256(data)
				// if !cr.findNotAdd(string(hash)) {
				// 	continue
				// }
				pm, err := unmarshal(data)
				if err != nil {
					plog.Error(err.Error())
					continue
				}
				ch <- pm
				// if len(ch) > 10 {
				// 	plog.Info("cache gossip msg len", "len", len(ch))
				// }
			}
		}()
	}
	// go func() {
	// 	for range time.Tick(time.Second * 30) {
	// 		cr.rm(2048)
	// 	}
	// }()
	return ch
}

func (n *node) synced() bool {
	return n.IsCaughtUp() || n.lastBlock().Height+3 > n.maxSortHeight
}

func (n *node) getPID() {
	// get my pid
	for range time.Tick(time.Second * 3) {
		list, err := n.GetAPI().PeerInfo(&types.P2PGetPeerReq{})
		if err != nil {
			plog.Error("getPeerPid error", "err", err)
			continue
		}
		if len(list.Peers) == 0 {
			continue
		}
		self := list.Peers[len(list.Peers)-1]
		n.pid = self.Name
		plog.Info("getSelfPid", "pid", n.pid)
		break
	}
}

type hr struct {
	h int64
	r int
}

var pos33Topics = []string{
	"/makersorts",
	"/votersorts",
	"/makervotes",
	"/blockvotes",
	"/block",
	"/committee",
}

func (n *node) runLoop() {
	lb, err := n.RequestLastBlock()
	if err != nil {
		panic(err)
	}

	go n.getPID()
	priv := n.getPriv()
	if priv == nil {
		panic("can't go here")
	}

	title := n.GetAPI().GetConfig().GetTitle()
	n.topic = title + pos33Topic
	ns := fmt.Sprintf("%s-%d", title, n.conf.ListenPort)

	var topics []string
	for _, t := range pos33Topics {
		topics = append(topics, n.topic+t)
	}

	n.gss = newGossip2(priv, n.conf.ListenPort, ns, n.conf.ForwardServers, n.conf.ForwardPeers, topics...)
	msgch := n.handleGossipMsg()
	if len(n.conf.BootPeers) > 0 {
		n.gss.bootstrap(n.conf.BootPeers...)
		// time.Sleep(time.Second * 5) // wait libp2p init...
	}

	n.updateTicketCount(lb.Height)

	if lb.Height > 0 {
		time.AfterFunc(time.Second, func() {
			n.addBlock(lb)
		})
	}

	plog.Info("pos33 running... 07131918", "last block height", lb.Height)

	isSync := false
	syncTick := time.NewTicker(time.Second)
	tch := make(chan int64, 1)
	nch := make(chan int64, 1)

	round := 0
	blockTimeout := time.Second * 5
	resortTimeout := time.Second * 5
	blockD := time.Millisecond * 2000

	for {
		if !isSync {
			time.Sleep(time.Millisecond * 1000)
			plog.Info("NOT sync .......")
			isSync = n.synced()
			continue
		}
		select {
		case <-n.done:
			plog.Info("pos33 consensus run loop stoped")
			return
		case msg := <-msgch:
			n.handlePos33Msg(msg)
		case msg := <-n.gss.incoming:
			n.handlePos33Msg(msg)
		case <-syncTick.C:
			isSync = n.synced()
		case height := <-tch:
			if height == n.lastBlock().Height+1 {
				round++
				plog.Info("block timeout", "height", height, "round", round)
				n.reSortition(height, round)
				time.AfterFunc(resortTimeout, func() {
					nh := n.lastBlock().Height + 1
					nch <- nh
				})
			}
		case height := <-nch:
			// n.getCommittee(height, round).setCommittee(height)
			cb := n.GetCurrentBlock()
			if cb.Height == height-1 {
				n.makeNewBlock(height, round)
				time.AfterFunc(blockTimeout, func() {
					tch <- height
				})
			}
		case b := <-n.bch: // new block add to chain
			round = 0
			n.handleNewBlock(b)
			d := blockD
			if b.Height > 0 {
				m, err := getMiner(b)
				if err != nil {
					panic("can't go here")
				}
				d = time.Millisecond * time.Duration(m.BlockTime+2000-time.Now().UnixNano()/1000000)
				if d < 0 {
					d = 0
				}
				if d > blockD {
					d = time.Millisecond * 1500
				}
			}
			plog.Info("after ms make next block", "d", d, "height", b.Height)
			time.AfterFunc(d, func() {
				nch <- b.Height + 1
			})
		case v := <-n.vch:
			// height := v.h
			// _, err := n.RequestBlock(height - 1)
			// if err != nil {
			// 	plog.Error("requestBlock error", "err", err, "height", height-1)
			// 	time.AfterFunc(time.Millisecond*100, func() { n.vch <- v })
			// } else {
			n.voteBlock(v.h, v.r)
			// }
			// case s := <-n.sch:
			// 	n.trySetBlock(s.h, s.r, true)
		}
	}
}

/*
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
			n.voteBlock(b.Hash(n.GetAPI().GetConfig()), h, r, n.getCommittee(h, r).vr)
			ab.ok = true
			return true
		}
	}
	ab.n++
	return false
}
*/

const calcuDiffN = pt.Pos33SortBlocks * 1

func (n *node) handleNewBlock(b *types.Block) {
	round := 0
	n.voteBlock(b.Height+1, round)
	plog.Info("handleNewBlock", "height", b.Height, "round", round)
	if b.Height == 0 {
		n.firstSortition()
		n.voteBlockHash(b.Hash(n.GetAPI().GetConfig()), 0, 0)
		for i := 0; i < 5; i++ {
			n.voteMaker(int64(i), round)
		}
	} else {
		n.sortition(b, round)
	}
	n.voteMaker(b.Height+pt.Pos33SortBlocks/2, round)
	n.clear(b.Height)
}

func (n *node) makeNewBlock(height int64, round int) {
	plog.Info("makeNewBlock", "height", height, "round", round)
	if round > 0 {
		// if timeout, only vote, handle vote will make new block
		n.voteMaker(height, round)
		return
	}
	// n.setSorts(height, round)
	n.tryMakeBlock(height, round)
}

func (n *node) sendVoterSort(ss []*pt.Pos33Sorts, height int64, round, ty int) {
	m := &pt.Pos33VoteSorts{
		VoteSorts: ss,
	}
	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_Ty(ty),
	}
	n.gss.gossip(n.topic+"/votersorts", types.Encode(pm))
	n.handleVoterSorts(ss, true, ty)
}

func (n *node) sendMakerSort(m *pt.Pos33SortMsg, height int64, round int) {
	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_MS,
	}
	n.gss.gossip(n.topic+"/makersorts", types.Encode(pm))
	n.handleMakerSort(m, true)
}

func hexs(b []byte) string {
	s := hex.EncodeToString(b)
	if len(s) <= 16 {
		return s
	}
	return s[:16]
}
