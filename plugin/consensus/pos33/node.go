package pos33

import (
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
	"github.com/33cn/chain33/types"
	"github.com/33cn/plugin/plugin/crypto/bls"
	"github.com/golang/protobuf/proto"

	pt "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

var plog = log15.New("module", "pos33")

const pos33Topic = "ycc-pos33"

// 区块制作人
type maker struct {
	my       *pt.Pos33SortMsg              // 我的抽签
	mvs      map[string][]*pt.Pos33VoteMsg // 我收到的 maker vote
	selected bool
	ok       bool
}

// 验证委员会
type committee struct {
	myss           [3][]*pt.Pos33SortMsg               // 我的抽签
	mss            map[string]*pt.Pos33SortMsg         // 我接收到的maker 的抽签
	css            map[int]map[string]*pt.Pos33SortMsg // 我收到committee的抽签
	ssmp           map[string]*pt.Pos33SortMsg
	svmp           map[string]int  // 验证委员会的投票
	sortCheckedMap map[string]bool // key is sort_hash, val is checked
	n              *node
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

func (n *node) getCommittee(height int64, round int) *committee {
	rmp, ok := n.mmp[height]
	if !ok {
		rmp = make(map[int]*committee)
		n.mmp[height] = rmp
	}
	m, ok := rmp[round]
	if !ok {
		m = &committee{
			mss:            make(map[string]*pt.Pos33SortMsg),
			css:            make(map[int]map[string]*pt.Pos33SortMsg),
			ssmp:           make(map[string]*pt.Pos33SortMsg),
			svmp:           make(map[string]int),
			sortCheckedMap: make(map[string]bool),
			n:              n,
		}
		rmp[round] = m
	}
	return m
}

func (c *committee) getMySorts(myaddr string, height int64) []*pt.Pos33SortMsg {
	ssmp := c.getCommitteeSorts()
	var ss []*pt.Pos33SortMsg
	for _, s := range ssmp {
		addr := address.PubKeyToAddr(address.DefaultID, s.Proof.Pubkey)
		if myaddr == addr {
			ss = append(ss, s)
		}
	}

	return ss
}

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
	if height > 0 && len(vs) < 17 {
		return 0, errors.New("checkVotes error: NOT enough votes")
	}
	return len(vs), nil
}

func (c *committee) getCommitteeSorts() map[string]*pt.Pos33SortMsg {
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

func (m *maker) findVm(key, pub string) bool {
	return find(m.mvs, key, pub)
}

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

type vArg struct {
	v  *pt.Pos33VoteMsg
	ch chan<- bool
}

type node struct {
	*Client
	gss *gossip2

	vmp map[int64]map[int]*maker
	mmp map[int64]map[int]*committee
	bch chan *types.Block

	vCh    chan vArg
	sortCh chan *sortArg

	blsMp map[string]string

	maxSortHeight int64
	pid           string
	topic         string
}

func newNode(conf *subConfig) *node {
	return &node{
		mmp:    make(map[int64]map[int]*committee),
		vmp:    make(map[int64]map[int]*maker),
		bch:    make(chan *types.Block, 16),
		blsMp:  make(map[string]string),
		vCh:    make(chan vArg, 8),
		sortCh: make(chan *sortArg, 8),
	}
}

func (n *node) lastBlock() *types.Block {
	b, err := n.RequestLastBlock()
	if err != nil {
		b = n.GetCurrentBlock()
	}
	return b
}

func (n *node) minerTx(height int64, round int, sm *pt.Pos33SortMsg, vs []*pt.Pos33VoteMsg, priv crypto.PrivKey) (*types.Transaction, error) {
	if len(vs) > pt.Pos33VoterSize {
		sort.Sort(pt.Votes(vs))
		vs = vs[:pt.Pos33VoterSize]
	}
	var pklist [][]byte
	var sigs []crypto.Signature
	for _, v := range vs {
		pklist = append(pklist, v.Sig.Pubkey)
		var sig [bls.BLSSignatureLength]byte
		copy(sig[:], v.Sig.Signature)
		sigs = append(sigs, bls.SignatureBLS(sig))
	}

	blsSig, err := bls.Driver{}.Aggregate(sigs)
	if err != nil && round < 3 {
		return nil, err
	} else if err != nil {
		blsSig = bls.SignatureBLS{}
	}
	act := &pt.Pos33TicketAction{
		Value: &pt.Pos33TicketAction_Miner{
			Miner: &pt.Pos33MinerMsg{
				BlsPkList: pklist,
				Hash:      sm.SortHash.Hash,
				BlsSig:    blsSig.Bytes(),
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
	return nb, nil
}

func (n *node) makeBlock(height int64, round int, sort *pt.Pos33SortMsg, vs []*pt.Pos33VoteMsg) (*types.Block, error) {
	priv := n.getPriv()
	if priv == nil {
		panic("can't go here")
	}

	lb, err := n.RequestBlock(height - 1)
	if err != nil {
		return nil, err
	}
	tx, err := n.minerTx(height, round, sort, vs, priv)
	if err != nil {
		return nil, err
	}

	nb, err := n.newBlock(lb, []*Tx{tx}, height)
	if err != nil {
		return nil, err
	}

	nb.Difficulty = n.blockDiff(lb, len(vs))
	plog.Info("block make", "height", height, "round", round, "ntx", len(nb.Txs), "nvs", len(vs), "hash", common.HashHex(nb.Hash(n.GetAPI().GetConfig()))[:16])

	// nb = n.PreExecBlock(nb, false)
	// if nb == nil {
	// 	return nil, errors.New("PreExccBlock error")
	// }

	return nb, nil
}

func (n *node) broadcastComm(height int64, round int, msg types.Message) {
	comm := n.getCommittee(height, round)
	mp := make(map[string]bool)
	for k := range comm.svmp {
		sort, ok := comm.ssmp[k]
		if !ok {
			continue
		}
		pub := sort.Proof.Pubkey
		_, ok = mp[string(pub)]
		if ok {
			continue
		}
		mp[string(pub)] = true
		if string(pub) == string(n.priv.PubKey().Bytes()) {
			continue
		}
		n.gss.sendMsg(pub, msg)
	}
}

func (n *node) broadcastBlock(b *types.Block, round int) {
	m := &pt.Pos33BlockMsg{B: b, Pid: n.pid}

	// pm := &pt.Pos33Msg{Data: types.Encode(m), Ty: pt.Pos33Msg_B}
	// n.broadcastComm(b.Height, round, pm)
	// data := types.Encode(pm)
	// n.gss.gossip(n.topic+"/block", data)
	n.handleBlockMsg(m, true)
}

func (n *node) addBlock(b *types.Block) {
	if b.Height > 0 {
		lb, err := n.RequestBlock(b.Height - 1)
		if err != nil {
			panic("can't go here")
		}
		plog.Info("block add", "height", b.Height, "hash", common.ToHex(b.Hash(n.GetAPI().GetConfig()))[:16], "time", time.Now().Format("15:04:05.00000"))
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

func (n *node) runVerifyVotes() {
	for i := 0; i < 8; i++ {
		go func() {
			for v := range n.vCh {
				v.ch <- v.v.Verify()
			}
		}()
	}
}
func (n *node) verifyVotes(vs []*pt.Pos33VoteMsg) bool {
	ch := make(chan bool, len(vs))
	defer close(ch)
	k := 0

FOR:
	for _, v := range vs {
		n.vCh <- vArg{v, ch}
		k++ // 发送请求数量

		// 测试处理结果，如果有失败，就退出
		select {
		case ok := <-ch:
			ch <- ok // 发送回 ch
			if !ok {
				break FOR
			}
		default:
		}
	}

	j := 0
	rok := true
	for ok := range ch {
		if !ok {
			rok = false
		}

		// j用来指示接收的结果数量, 和发送相等时，退出循环，关闭ch
		j++
		if k == j {
			break
		}
	}
	return rok
}

func (n *node) checkVotes(vs []*pt.Pos33VoteMsg, ty int, hash []byte, h int64, checkEnough, checkCommittee bool) error {
	v0 := vs[0]
	height := v0.Sort.Proof.Input.Height
	round := v0.Sort.Proof.Input.Round

	if checkEnough {
		if len(vs) < pt.Pos33MustVotes {
			return errors.New("checkVotes error: NOT enough votes")
		}
	}

	if !n.verifyVotes(vs) {
		plog.Error("verifyVotes error", "height", height)
		return errors.New("verifyVotes error")
	}

	for _, v := range vs {
		ht := v.Sort.Proof.Input.Height
		rd := v.Sort.Proof.Input.Round
		if ht != height || rd != round {
			return errors.New("checkVotes error: height, round or num NOT same")
		}
		err := n.checkVote(v, hash, ty)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *node) checkVote(v *pt.Pos33VoteMsg, hash []byte, ty int) error {
	if string(v.Hash) != string(hash) {
		return errors.New("vote hash NOT right")
	}

	blsAddr := address.PubKeyToAddr(address.DefaultID, v.Sig.Pubkey)
	addr, ok := n.blsMp[blsAddr]
	if !ok {
		msg, err := n.GetAPI().Query(pt.Pos33TicketX, "Pos33BlsAddr", &types.ReqAddr{Addr: blsAddr})
		if err != nil {
			return err
		}
		addr = msg.(*types.ReplyString).Data
		n.blsMp[blsAddr] = addr
	}
	sortAddr := address.PubKeyToAddr(address.DefaultID, v.Sort.Proof.Pubkey)
	if addr != sortAddr {
		return errors.New("Pos33BindAddr NOT match")
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
	if len(act.BlsPkList) < pt.Pos33MustVotes {
		return fmt.Errorf("NOT enought votes")
	}
	round := int(act.Sort.Proof.Input.Round)

	plog.Info("block check", "height", b.Height, "from", b.Txs[0].From()[:16])
	err = n.checkSort(act.Sort, 0)
	if err != nil {
		plog.Error("blockCheck error", "err", err, "height", b.Height, "round", round)
		return err
	}

	if round >= 3 {
		return nil
	}

	return act.Verify()
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
		plog.Error("handleVoterSort error: sort num >=3", "height", height, "round", round, "num", num, "addr", address.PubKeyToAddr(address.DefaultID, s0.Proof.Pubkey)[:16])
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
		mp[string(s.SortHash.Hash)] = s
	}
	// plog.Info("handleVoterSort", "all", len(comm.css[num]), "nvs", len(ss), "height", height, "round", round, "num", num, "ty", ty, "addr", address.PubKeyToAddr(address.DefaultID,s0.Proof.Pubkey)[:16])
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

	maker := n.getmaker(height, round)

	// repeat msg
	if maker.findVm(string(m0.Hash), string(m0.Sig.Pubkey)) {
		return
	}
	if len(ms) == 0 {
		return
	}

	err := n.checkVotes(ms, ty, m0.Hash, height, false, true)
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

		maker.mvs[string(m.Hash)] = append(maker.mvs[string(m.Hash)], m)
	}

	vs := maker.mvs[string(m0.Hash)]
	if len(vs) >= 10 {
		plog.Info("handleVoteMsg maker", "hash", common.HashHex(m0.Hash)[:16], "allmvs", len(vs), "nvs", len(ms), "height", height, "round", round, "ty", ty, "addr", address.PubKeyToAddr(address.DefaultID, m0.Sig.Pubkey)[:16])
	}
	if round > 0 {
		n.tryMakeBlock(height, round)
	}
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

	nb, err := n.makeBlock(height, round, maker.my, vs)
	if err != nil && round < 3 {
		plog.Error("makeBlock error", "err", err, "height", height)
		return
	}
	n.broadcastBlock(nb, round)
	maker.ok = true
}

func (n *node) handleBlockMsg(m *pt.Pos33BlockMsg, myself bool) {
	plog.Info("handleBlockMsg", "height", m.B.Height, "time", time.Now().Format("15:04:05.00000"))
	n.setBlock(m.B)
}

func checkTime(t int64) bool {
	// mt := time.Now().UnixNano() / 1000000
	// if mt-t > 3000 || t-mt > 2000 {
	// 	plog.Info("checkTime false", "t", t, "mt", mt, "mt-t", mt-t)
	// 	return false
	// }
	return true
}

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
	plog.Info("handleCommittee", "nsvmp", len(comm.svmp), "nvs", len(m.MySorts), "height", height, "addr", address.PubKeyToAddr(address.DefaultID, m.Sig.Pubkey)[:16], "time", time.Now().Format("15:04:05.00000"))
}

func (n *node) voteCommittee(height int64, round int) {
	comm := n.getCommittee(height, round)
	css := comm.getCommitteeSorts()
	var ss [][]byte
	for k := range css {
		ss = append(ss, []byte(k))
	}

	myss := comm.getMySorts(n.myAddr, height)
	if len(myss) == 0 {
		return
	}

	m := &pt.Pos33SortsVote{
		MySorts:     myss,
		SelectSorts: ss,
		Height:      height,
		Round:       int32(round),
	}
	m.Sign(n.priv)

	plog.Info("voteCommittee", "height", height, "nmySelect", len(ss), "nv", len(m.MySorts))
	n.handleCommittee(m, true)

	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_CV,
	}
	data := types.Encode(pm)
	n.gss.gossip(n.topic+"/committee", data)
}

func signVotes(priv crypto.PrivKey, vs []*pt.Pos33VoteMsg) {
	ch := make(chan *pt.Pos33VoteMsg)
	wg := new(sync.WaitGroup)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for v := range ch {
				v.Sign(priv)
			}
		}()
	}
	for _, v := range vs {
		ch <- v
	}
	close(ch)
	wg.Wait()
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

	myss := comm.getMySorts(n.myAddr, height)

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
			vs = append(vs, v)
		}
		if len(vs) == 0 {
			continue
		}
		signVotes(n.priv, vs)
		mvs = append(mvs, &pt.Pos33Votes{Vs: vs})
		plog.Info("vote maker", "addr", address.PubKeyToAddr(address.DefaultID, s.Proof.Pubkey)[:16], "height", height, "round", round, "time", time.Now().Format("15:04:05.00000"))
		break
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
			plog.Error("handleSort time NOT right", "height", height, "addr", address.PubKeyToAddr(address.DefaultID, m.Proof.Pubkey)[:16])
			return
		}
	}
	round := int(m.Proof.Input.Round)
	comm := n.getCommittee(height, round)
	comm.mss[string(m.SortHash.Hash)] = m
	if round > 0 && height > n.maxSortHeight {
		n.maxSortHeight = height
	}
	plog.Debug("handleMakerSort", "nmss", len(comm.mss), "height", height, "round", round, "addr", address.PubKeyToAddr(address.DefaultID, m.Proof.Pubkey)[:16])
}

func (n *node) checkSort(s *pt.Pos33SortMsg, ty int) error {
	height := s.Proof.Input.Height
	round := int(s.Proof.Input.Round)
	comm := n.getCommittee(height, round)
	k := string(s.SortHash.Hash)
	_, ok := comm.sortCheckedMap[k]
	if ok {
		return nil
	}
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
	comm.sortCheckedMap[k] = true
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

// handleGossipMsg multi-goroutine verify pos33 message
func (n *node) handleGossipMsg() chan *pt.Pos33Msg {
	num := 4
	ch := make(chan *pt.Pos33Msg, num*128)
	for i := 0; i < num; i++ {
		go func() {
			for {
				data := <-n.gss.C
				pm, err := unmarshal(data)
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
	for range time.NewTicker(time.Second * 3).C {
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

var pos33Topics = []string{
	"/makersorts",
	"/votersorts",
	"/makervotes",
	// "/blockvotes",
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
	}

	n.updateTicketCount(lb.Height)

	if lb.Height > 0 {
		time.AfterFunc(time.Second, func() {
			n.addBlock(lb)
		})
	}

	plog.Info("pos33 running... 07131918", "last block height", lb.Height)
	go n.runVerifyVotes()
	go n.runSortition()

	isSync := false
	tch := make(chan int64, 1)
	nch := make(chan int64, 1)

	round := 0
	blockTimeout := time.Second * 5
	resortTimeout := time.Second * 5
	blockD := int64(900)

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
			n.getCommittee(height, round).setCommittee(height)
			cb := n.GetCurrentBlock()
			if cb.Height == height-1 {
				n.makeNewBlock(height, round)
				time.AfterFunc(blockTimeout, func() {
					tch <- height
				})
			}
		case b := <-n.bch: // new block add to chain
			if b.Height < n.GetCurrentHeight() {
				continue
			}
			round = 0
			n.handleNewBlock(b)
			d := blockD
			if b.Height > 0 {
				m, err := getMiner(b)
				if err != nil {
					panic("can't go here")
				}
				d = m.BlockTime + blockD - time.Now().UnixNano()/1000000
				if d < 0 {
					d = 0
				}
				if d > blockD {
					d = blockD
				}
			}
			plog.Info("after ms make next block", "d", d, "height", b.Height, "time", time.Now().Format("15:04:05.00000"))
			time.AfterFunc(time.Millisecond*time.Duration(d), func() {
				nch <- b.Height + 1
			})
		}
	}
}

func (n *node) handleNewBlock(b *types.Block) {
	tb := time.Now()
	round := 0
	plog.Info("handleNewBlock", "height", b.Height, "round", round, "time", time.Now().Format("15:04:05.00000"))
	if b.Height == 0 {
		n.firstSortition()
		for i := 0; i < 5; i++ {
			n.voteMaker(int64(i), round)
		}
	} else {
		n.sortition(b, round)
	}
	n.voteMaker(b.Height+pt.Pos33SortBlocks/2, round)
	n.clear(b.Height)
	plog.Info("handleNewBlock cost", "height", b.Height, "cost", time.Since(tb))
}

func (n *node) makeNewBlock(height int64, round int) {
	plog.Info("makeNewBlock", "height", height, "round", round, "time", time.Now().Format("15:04:05.00000"))
	if round > 0 {
		// if timeout, only vote, handle vote will make new block
		n.voteMaker(height, round)
		return
	}
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
