package pos33

import (
	"errors"
	"fmt"
	"os"
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

// 验证委员会
type committee struct {
	myss []*pt.Pos33SortMsg          // 我的抽签
	css  map[string]*pt.Pos33SortMsg // 我收到committee的抽签
	// ssmp map[string]*pt.Pos33SortMsg
	svmp map[string]int // 验证委员会的投票
	n    *node

	bvmp   map[string][]*pt.Pos33VoteMsg
	bmp    map[string]*types.Block
	voted  bool
	setted bool

	candidates []string
	comm       []*pt.Pos33SortMsg
}

func writeVotes(file string, bvmp map[string][]*pt.Pos33VoteMsg) error {
	var vs pt.Pos33Votes
	for _, v := range bvmp {
		vs.Vs = append(vs.Vs, v...)
	}
	err := os.WriteFile(file, types.Encode(&vs), 0666)
	if err != nil {
		return err
	}
	return nil
}

func readVotes(file string) (map[string][]*pt.Pos33VoteMsg, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var vs pt.Pos33Votes
	err = types.Decode(data, &vs)
	if err != nil {
		return nil, err
	}
	mp := make(map[string][]*pt.Pos33VoteMsg)
	for _, v := range vs.Vs {
		mp[string(v.Hash)] = append(mp[string(v.Hash)], v)
	}
	return mp, nil
}

func (c *committee) setCommittee(height int64) {
	if c.setted {
		return
	}
	c.setted = true
	var ss []*pt.Pos33SortMsg
	for k := range c.svmp {
		s, ok := c.css[k]
		if ok {
			ss = append(ss, s)
		}
	}
	sort.Sort(pt.Sorts(ss))
	if len(ss) > pt.Pos33CommitteeSize {
		ss = ss[:pt.Pos33CommitteeSize]
	}
	for _, s := range ss {
		n := c.svmp[string(s.SortHash.Hash)]
		if n >= pt.Pos33MustVotes {
			c.comm = append(c.comm, s)
		}
	}
	for i, s := range c.comm {
		c.candidates = append(c.candidates, string(s.SortHash.Hash))
		plog.Info("setCommittee", "canditate", common.HashHex(s.SortHash.Hash), "height", height)
		if i == 2 {
			break
		}
	}
	plog.Info("setCommittee", "len", len(c.comm), "height", height)
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
			css: make(map[string]*pt.Pos33SortMsg),
			// ssmp: make(map[string]*pt.Pos33SortMsg),
			svmp: make(map[string]int),
			bvmp: make(map[string][]*pt.Pos33VoteMsg),
			bmp:  make(map[string]*types.Block),
			n:    n,
		}
		rmp[round] = m
	}
	return m
}

func getMySorts(myaddr string, ss []*pt.Pos33SortMsg) []*pt.Pos33SortMsg {
	var ms []*pt.Pos33SortMsg
	for _, s := range ss {
		addr := address.PubKeyToAddr(ethID, s.Proof.Pubkey)
		if myaddr == addr {
			ms = append(ms, s)
		}
	}

	return ms
}

func getSorts(mp map[string]*pt.Pos33SortMsg, num int) []*pt.Pos33SortMsg {
	var ss []*pt.Pos33SortMsg
	for _, s := range mp {
		ss = append(ss, s)
	}
	sort.Sort(pt.Sorts(ss))
	if len(ss) > num {
		ss = ss[:num]
	}
	return ss
}

// func find(vmp map[string][]*pt.Pos33VoteMsg, key, pub string) bool {
// 	vs, ok := vmp[key]
// 	if !ok {
// 		return false
// 	}
// 	for _, v := range vs {
// 		if string(v.Sig.Pubkey) == pub {
// 			return true
// 		}
// 	}
// 	return false
// }

type vArg struct {
	v  *pt.Pos33VoteMsg
	ch chan<- bool
}

type node struct {
	*Client
	gss *gossip2

	mmp map[int64]map[int]*committee
	bch chan *types.Block // for add block

	vCh    chan vArg
	sortCh chan *sortArg

	mu    sync.Mutex
	blsMp map[string]string

	pid   string
	topic string
}

func newNode(conf *subConfig) *node {
	return &node{
		mmp:    make(map[int64]map[int]*committee),
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
	if err != nil {
		return nil, err
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

	signID := types.EncodeSignID(types.SECP256K1, ethID)
	tx.Sign(signID, priv)
	plog.Info("make a minerTx", "nvs", len(vs), "height", height, "fee", tx.Fee, "from", tx.From())
	return tx, nil
}

// 返回值越小表示难度越大
func (n *node) blockDiff(sort *pt.Pos33SortMsg, vs []*pt.Pos33VoteMsg, height int64) uint32 {
	powLimitBits := n.GetAPI().GetConfig().GetP(height).PowLimitBits
	return powLimitBits
	// tmpHash := make([]byte, 32)
	// copy(tmpHash, sort.SortHash.Hash)
	// hash := difficulty.HashToBig(tmpHash)

	// sumHash := new(big.Int)
	// for _, v := range vs {
	// 	copy(tmpHash, v.Sort.SortHash.Hash)
	// 	hash := difficulty.HashToBig(tmpHash)
	// 	sumHash = new(big.Int).Add(sumHash, hash)
	// }
	// product := new(big.Int).Mul(sumHash, big.NewInt(25))
	// product = new(big.Int).Mul(product, hash)
	// quotient := new(big.Int).Div(product, big.NewInt(int64(len(vs)*len(vs))))
	// return difficulty.BigToCompact(quotient)
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

	nb.Difficulty = n.blockDiff(sort, vs, height)

	// nb = n.PreExecBlock(nb, false)
	// if nb == nil {
	// 	return nil, errors.New("PreExccBlock error")
	// }
	plog.Info("block make", "height", height, "round", round, "ntx", len(nb.Txs), "nvs", len(vs), "hash", common.HashHex(nb.Hash(n.GetAPI().GetConfig()))[:16], "diff", nb.Difficulty)

	return nb, nil
}

// func (n *node) broadcastComm(height int64, round int, msg types.Message) {
// 	comm := n.getCommittee(height, round)
// 	mp := make(map[string]bool)
// 	for k := range comm.svmp {
// 		sort, ok := comm.ssmp[k]
// 		if !ok {
// 			continue
// 		}
// 		pub := sort.Proof.Pubkey
// 		_, ok = mp[string(pub)]
// 		if ok {
// 			continue
// 		}
// 		mp[string(pub)] = true
// 		if string(pub) == string(n.getPriv().PubKey().Bytes()) {
// 			continue
// 		}
// 		n.gss.sendMsg(pub, msg)
// 	}
// }

func (n *node) broadcastBlock(b *types.Block, round int) {
	m := &pt.Pos33BlockMsg{B: b, Pid: n.pid}
	pm := &pt.Pos33Msg{Data: types.Encode(m), Ty: pt.Pos33Msg_B}
	// n.broadcastComm(b.Height, round, pm)
	data := types.Encode(pm)
	n.gss.gossip(n.topic+"/block", data)

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
}

func (n *node) prepareOK(height int64) bool {
	if height < 10 {
		return true
	}
	return n.IsCaughtUp() && /*n.allCount(height) > 0 &&*/ n.queryTicketCount(n.myAddr, height-10) > 0
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
	if b.Txs[0].From() == n.myAddr {
		return nil
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

	// if checkEnough {
	// 	if len(vs) < pt.Pos33MustVotes {
	// 		return errors.New("checkVotes error: NOT enough votes")
	// 	}
	// }

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

func (n *node) getMinerList() []string {
	n.mu.Lock()
	defer n.mu.Unlock()

	var ss []string
	for _, v := range n.blsMp {
		ss = append(ss, v)
	}
	return ss
}

func (n *node) checkVote(v *pt.Pos33VoteMsg, hash []byte, ty int) error {
	if string(v.Hash) != string(hash) {
		return errors.New("vote hash NOT right")
	}

	blsAddr := address.PubKeyToAddr(ethID, v.Sig.Pubkey)
	n.mu.Lock()
	defer n.mu.Unlock()
	addr, ok := n.blsMp[blsAddr]
	if !ok {
		msg, err := n.GetAPI().Query(pt.Pos33TicketX, "Pos33BlsAddr", &types.ReqAddr{Addr: blsAddr})
		if err != nil {
			return err
		}
		addr = msg.(*types.ReplyString).Data
		n.blsMp[blsAddr] = addr
	}
	sortAddr := address.PubKeyToAddr(ethID, v.Sort.Proof.Pubkey)
	if addr != sortAddr {
		return errors.New("Pos33BindAddr NOT match")
	}

	return n.checkSort(v.Sort, Committee)
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
	if act == nil || err != nil {
		plog.Error("block check getMiner error", "err", err, "height", b.Height)
		return err
	}
	if act.Sort == nil || act.Sort.Proof == nil || act.Sort.Proof.Input == nil {
		return fmt.Errorf("miner tx error")
	}
	// if len(act.BlsPkList) < pt.Pos33MustVotes {
	// 	return fmt.Errorf("NOT enought votes")
	// }
	round := int(act.Sort.Proof.Input.Round)

	plog.Info("block check", "height", b.Height, "from", b.Txs[0].From()[:16])
	err = n.checkSort(act.Sort, Committee)
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
	n.sortCommittee(seed, height, round)
}

func (n *node) firstSortition() {
	seed := zeroHash[:]
	for i := 0; i <= pt.Pos33SortBlocks; i++ {
		height := int64(i)
		// n.sortMaker(seed, height, 0)
		n.sortCommittee(seed, height, 0)
	}
}

func (n *node) sortCommittee(seed []byte, height int64, round int) {
	ss := n.committeeSort(seed, height, round, Committee)
	if len(ss) == 0 {
		return
	}
	c := n.getCommittee(height, round)
	c.myss = ss
	plog.Info("sortCommittee", "height", height, "round", round, "ss len", len(ss))
	n.sendCommitteeerSort([]*pt.Pos33Sorts{{Sorts: ss}}, height, round, int(pt.Pos33Msg_VS))
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

func (n *node) getDiff(height int64, round int) float64 {
	height -= pt.Pos33SortBlocks
	w := n.allCount(height)
	size := pt.Pos33CommitteeSize
	diff := float64(size) / float64(w)
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
		plog.Error("handleVoterSort error: sort num >=3", "height", height, "round", round, "num", num, "addr", address.PubKeyToAddr(ethID, s0.Proof.Pubkey)[:16])
		return false
	}

	comm := n.getCommittee(height, round)

	for _, s := range comm.css {
		if string(s.Proof.Pubkey) == string(s0.Proof.Pubkey) {
			return true
		}
	}

	for _, s := range ss {
		comm.css[string(s.SortHash.Hash)] = s
	}
	plog.Info("handleVoterSort", "all", len(comm.css), "nvs", len(ss), "height", height, "round", round, "num", num, "ty", ty, "addr", address.PubKeyToAddr(ethID, s0.Proof.Pubkey)[:16])
	return true
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
	if num >= 1 {
		return
	}

	if n.lastBlock().Height > height {
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

	comm := n.getCommittee(height, round)
	for _, m := range ms {
		if m.Round != m0.Round {
			return
		}
		if string(m.Hash) != string(m0.Hash) {
			return
		}

		comm.bvmp[string(m.Hash)] = append(comm.bvmp[string(m.Hash)], m)
	}

	plog.Info("handleVoteMsg", "height", height, "round", round, "nvs", len(ms), "hash", common.HashHex(m0.Hash)[:16], "bvmp", len(comm.bvmp[string(m0.Hash)]))

	if height == 0 || n.GetCurrentHeight() >= height {
		return
	}

	vs := comm.bvmp[string(m0.Hash)]
	if len(vs) > pt.Pos33VoterSize/2 {
		myS := comm.myCandidataeSort()
		if myS == nil {
			return
		}
		if string(m0.Hash) == string(myS.SortHash.Hash) {
			nb, err := n.makeBlock(height, round, myS, vs)
			if err != nil {
				plog.Error("makeBlock error", "err", err, "height", height)
				return
			}
			n.setBlock(nb)
		}
	}
}

func (co *committee) myCandidataeSort() *pt.Pos33SortMsg {
	for _, c := range co.candidates {
		for _, s := range co.myss {
			if c == string(s.SortHash.Hash) {
				return s
			}
		}
	}
	return nil
}

func (n *node) verifyPreBlock(b *types.Block) bool {
	pb, err := n.RequestBlock(b.Height - 1)
	if err != nil {
		plog.Error("requestBlock error", "err", err, "height", b.Height)
		return false
	}
	if string(pb.Hash(n.GetAPI().GetConfig())) != string(b.ParentHash) {
		plog.Error("pb hash NOT match", "height", b.Height)
		return false
	}
	comm := n.getCommittee(b.Height, int(b.BlockTime))
	found := false
	for _, c := range comm.candidates {
		if c == string(b.TxHash) {
			found = true
			break
		}
	}
	if !found {
		plog.Error("sort hash NOT match canditates", "height", b.Height)
		return false
	}
	sig := b.Signature
	b.Signature = nil
	ok := types.CheckSign(types.Encode(b), "", sig, b.Height)
	b.Signature = sig
	if !ok {
		plog.Error("checkSign failed", "height", b.Height)
	}
	return ok
}

func (n *node) preMakeBlock(height int64, round int) (*types.Block, error) {
	pb, err := n.RequestBlock(height - 1)
	if err != nil {
		return nil, err
	}

	comm := n.getCommittee(height, round)
	sort := comm.myCandidataeSort()
	if sort == nil {
		return nil, fmt.Errorf("preMakeBlock error: my candidate sort is nil. height=%d, round=%d", height, round)
	}
	plog.Info("preMakeBlock", "height", height, "sort hash", common.HashHex(sort.SortHash.Hash))

	cfg := n.GetAPI().GetConfig()
	nb := &types.Block{
		ParentHash: pb.Hash(cfg),
		Height:     height,
		TxHash:     sort.SortHash.Hash, // use TxHash fot sort hash
		BlockTime:  int64(round),       // use BlokeTime for round
	}

	sig := n.priv.Sign(types.Encode(nb))
	nb.Signature = &types.Signature{
		Signature: sig.Bytes(),
		Pubkey:    n.priv.PubKey().Bytes(),
		Ty:        types.EncodeSignID(types.SECP256K1, ethID),
	}

	return nb, nil
}

func (n *node) tryMakeBlock(height int64, round int) {
	comm := n.getCommittee(height, round)
	comm.setCommittee(height)

	myss := comm.myss
	if len(myss) == 0 {
		return
	}
	myS := comm.myCandidataeSort()
	if myS == nil {
		return
	}

	/*
		pb := n.GetCurrentBlock()
		lround := 0
		if pb.Height != 0 {
			miner, err := getMiner(pb)
			if err != nil {
				plog.Error("getMiner error", "height", height, "err", err)
				return
			}
			lround = int(miner.Sort.Proof.Input.Round)
		}
		lcomm := n.getCommittee(height-1, lround)
		lvs := lcomm.bvmp[string(pb.Hash(n.GetAPI().GetConfig()))]
		plog.Info("tryMakeBlock", "height", height, "nlvs", len(lvs))

		nb, err := n.makeBlock(height, round, myS, lvs)
		if err != nil {
			plog.Error("makeBlock error", "err", err, "height", height)
			return
		}
	*/
	nb, err := n.preMakeBlock(height, round)
	if err != nil {
		plog.Error("preMakeBlock error", "err", err, "height", height, "round", round)
		return
	}
	n.broadcastBlock(nb, round)
}

func (n *node) handleBlockMsg(m *pt.Pos33BlockMsg, myself bool) {
	round := int(m.B.BlockTime)
	comm := n.getCommittee(m.B.Height, round)
	hash := m.B.TxHash
	comm.bmp[string(hash)] = m.B
	if len(comm.bmp) > 2 {
		n.voteBlock(m.B.Height, round)
	}
	plog.Info("handleBlockMsg", "height", m.B.Height, "hash", common.HashHex(hash)[:16])
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
		err := n.checkSort(s, Committee)
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
	plog.Info("handleCommittee", "nsvmp", len(comm.svmp), "nvs", len(m.MySorts), "height", height, "addr", address.PubKeyToAddr(ethID, m.Sig.Pubkey)[:16], "time", time.Now().Format("15:04:05.00000"))
}

func (n *node) voteCommittee(height int64, round int) {
	comm := n.getCommittee(height, round)
	css := getSorts(comm.css, pt.Pos33VoterSize)

	var ss [][]byte
	for _, s := range css {
		ss = append(ss, s.SortHash.Hash)
	}

	myss := getMySorts(n.myAddr, css)
	if len(myss) == 0 {
		return
	}

	m := &pt.Pos33SortsVote{
		MySorts:     myss,
		SelectSorts: ss,
		Height:      height,
		Round:       int32(round),
	}
	m.Sign(n.getPriv())

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

func (n *node) sendBlockVotes(mvs []*pt.Pos33VoteMsg, ty int) {
	m := &pt.Pos33Votes{Vs: mvs}
	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_Ty(ty),
	}
	data := types.Encode(pm)
	n.gss.gossip(n.topic+"/blockvotes", data)
	n.handleVoteMsg(mvs, true, ty)
}

func (n *node) checkSort(s *pt.Pos33SortMsg, ty int) error {
	height := s.Proof.Input.Height
	seed, err := n.getSortSeed(height - pt.Pos33SortBlocks)
	if err != nil {
		plog.Error("getSeed error", "err", err, "height", height)
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
	case pt.Pos33Msg_BV:
		var m pt.Pos33Votes
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleVoteMsg(m.Vs, false, int(pm.Ty))
	case pt.Pos33Msg_VS:
		var m pt.Pos33VoteSorts
		err := types.Decode(pm.Data, &m)
		if err != nil {
			plog.Error(err.Error())
			return false
		}
		n.handleVoterSorts(m.VoteSorts, false, int(pm.Ty))
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
		plog.Debug("getSelfPid", "pid", n.pid)
		break
	}
}

var pos33Topics = []string{
	"/votersorts",
	"/blockvotes",
	"/block",
	"/committee",
}

func (n *node) voteBlock(height int64, round int) {
	comm := n.getCommittee(height, round)
	comm.setCommittee(height)

	var hash []byte
	for _, c := range comm.candidates {
		b, ok := comm.bmp[c]
		if ok {
			if !n.verifyPreBlock(b) {
				continue
			}
			hash = []byte(c)
			break
		}
	}

	if hash == nil {
		return
	}

	if !comm.voted {
		comm.voted = true
	} else {
		return
	}

	myss := getMySorts(n.myAddr, comm.comm)
	if len(myss) == 0 {
		return
	}

	var vs []*pt.Pos33VoteMsg
	for _, mys := range myss {
		v := &pt.Pos33VoteMsg{
			Hash: hash,
			Sort: mys,
		}
		vs = append(vs, v)
	}
	signVotes(n.getPriv(), vs)
	plog.Info("voteBlock", "height", height, "round", round, "hash", common.HashHex(hash)[:16], "nvs", len(vs))
	n.sendBlockVotes(vs, int(pt.Pos33Msg_BV))
}

// func (n *node) voteCanditate(height int64, round, who int) {
// 	comm := n.getCommittee(height, round)
// 	if len(comm.candidates) < who+1 {
// 		return
// 	}
// 	candidate := comm.candidates[who]

// 	// 我的抽签
// 	myss := comm.getMySorts(n.myAddr, height)

// 	// 对候选人投票
// 	var mvs []*pt.Pos33Votes
// 	var vs []*pt.Pos33VoteMsg
// 	for _, mys := range myss {
// 		v := &pt.Pos33VoteMsg{
// 			Hash: []byte(candidate),
// 			Sort: mys,
// 		}
// 		vs = append(vs, v)
// 	}
// 	signVotes(n.getPriv(), vs)
// 	mvs = append(mvs, &pt.Pos33Votes{Vs: vs})
// 	if len(mvs) == 0 {
// 		return
// 	}
// 	n.sendCanditateVotes(mvs, int(pt.Pos33Msg_MV))
// }

func blockRound(b *types.Block) int {
	m, err := getMiner(b)
	if err != nil {
		panic(err)
	}
	return int(m.Sort.Proof.Input.Round)
}

const blockVotePath = "./datadir/bv.data"

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

	n.updateTicketCount(lb)

	if lb.Height > 0 {
		time.AfterFunc(time.Second, func() {
			n.addBlock(lb)
		})
	}

	plog.Info("pos33 running... ", "last block height", lb.Height)
	go n.runVerifyVotes()
	go n.runSortition()

	isSync := n.IsCaughtUp()
	syncTm := time.NewTicker(time.Second * 30)
	syncCh := make(chan bool, 1)

	go func() {
		for range syncTm.C {
			syncCh <- n.IsCaughtUp()
		}
	}()

	tch := make(chan int64, 1)
	nch := make(chan int64, 1)
	vch := make(chan int64, 1)
	cch := make(chan int64, 1)
	blockTimeout := time.Second * 3
	resortTimeout := time.Second * 1
	voteCommitteeTmo := time.Second * 1
	voteBlockTmo := time.Millisecond * 900

	round := 0
	blockD := int64(900)

	if lb.Height > 0 {
		bvmp, err := readVotes(blockVotePath)
		if err != nil {
			plog.Info("readVotes error", "err", err)
		}
		lround := 0
		if lb.Height != 0 {
			lround = blockRound(lb)
		}
		lcomm := n.getCommittee(lb.Height, lround)
		// lcomm.bmp[string(lb.Hash(n.GetAPI().GetConfig()))] = lb
		lcomm.bvmp = bvmp
	}

	go func() {
		for range time.NewTicker(time.Second * 30).C {
			syncCh <- n.IsCaughtUp()
		}
	}()

	for {
		if !isSync {
			time.Sleep(time.Millisecond * 1000)
			plog.Info("NOT sync .......")
			isSync = n.IsCaughtUp()
			continue
		}
		select {
		case isSync = <-syncCh:
		case <-n.done:
			plog.Debug("pos33 consensus run loop stoped")
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
				tt := time.Now()
				time.AfterFunc(resortTimeout, func() {
					nh := n.lastBlock().Height + 1
					if nh > height {
						return
					}
					plog.Debug("block timeout", "height", height, "round", round, "cost", time.Since(tt))
					cch <- nh
				})
			}
		case height := <-cch:
			n.voteCommittee(height, round)
			time.AfterFunc(voteCommitteeTmo, func() {
				nch <- height
			})
		case height := <-nch:
			cb := n.GetCurrentBlock()
			if cb.Height == height-1 {
				n.tryMakeBlock(height, round)
				time.AfterFunc(blockTimeout, func() {
					if n.GetCurrentHeight() >= height {
						return
					}
					tch <- height
				})
				time.AfterFunc(voteBlockTmo, func() {
					vch <- height
				})
			}
		case height := <-vch:
			if height == n.GetCurrentHeight()+1 {
				n.voteBlock(height, round)
			}
		case b := <-n.bch: // new block add to chain
			if b.Height < n.GetCurrentHeight() && b.Height > 10 {
				break
			}
			round = 0
			// n.getCommittee(height, round).setCommittee(height)
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
			plog.Debug("after ms make next block", "d", d, "height", b.Height, "time", time.Now().Format("15:04:05.00000"))
			time.AfterFunc(time.Millisecond*time.Duration(d), func() {
				nch <- b.Height + 1
			})
			if b.Height%100 == 0 {
				plog.Info("bls", "height", b.Height, "bls", n.blsMp)
			}
		}
	}
}

func writeBlockVotes(b *types.Block, n *node) error {
	m, err := getMiner(b)
	if err != nil {
		return err
	}
	round := int(m.Sort.Proof.Input.Round)
	comm := n.getCommittee(b.Height, round)
	return writeVotes(blockVotePath, comm.bvmp)
}

func (n *node) setCommittee(height int64, round int) {
	comm := n.getCommittee(height, round)
	comm.setCommittee(height)
}

func (n *node) handleNewBlock(b *types.Block) {
	tb := time.Now()
	round := 0
	plog.Info("handleNewBlock", "height", b.Height, "round", round, "time", time.Now().Format("15:04:05.00000"))
	if b.Height == 0 {
		n.firstSortition()
		for i := 0; i < 5; i++ {
			n.voteCommittee(int64(i), round)
		}
	} else {
		n.sortition(b, round)
	}
	n.voteCommittee(b.Height+pt.Pos33SortBlocks/2, round)
	n.setCommittee(b.Height+2, round)
	n.clear(b.Height)
	plog.Debug("handleNewBlock cost", "height", b.Height, "cost", time.Since(tb))
	if b.Height > 0 {
		err := writeBlockVotes(b, n)
		if err != nil {
			plog.Error("writeBlockVotes error", "err", err)
		}
	}
}

func (n *node) sendCommitteeerSort(ss []*pt.Pos33Sorts, height int64, round, ty int) {
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
