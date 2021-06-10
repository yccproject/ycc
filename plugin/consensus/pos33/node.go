package pos33

import (
	"encoding/hex"
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
	t  int
	n  int
	f  bool
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

type voter struct {
	// mymvss []*pt.Pos33SortMsg          // 我作为 maker voter 的抽签
	myvss [3][]*pt.Pos33SortMsg       // 我作为 block voter 的抽签
	mss   map[string]*pt.Pos33SortMsg // 我接收到的maker 的抽签
	ab    *alterBlock                 // 我接收到所有备选block
}

// 区块制作人
type maker struct {
	my *pt.Pos33SortMsg // 我的抽签
	// mvss map[string]*pt.Pos33SortMsg   // 我收到的 maker vote 的抽签
	vss map[int]map[string]*pt.Pos33SortMsg // 我收到的 block vote 的抽签
	mvs map[string][]*pt.Pos33VoteMsg       // 我收到的 maker vote
	bvs map[string][]*pt.Pos33VoteMsg       // 我收到的 block vote
	ok  bool                                // 区块是否写入链，表示本轮完成
}

func (m *maker) getNum() int {
	min := 100
	num := -1
	for i := 0; i < 3; i++ {
		mp, ok := m.vss[i]
		if !ok {
			break
		}
		delt := int(math.Abs(float64(len(mp) - pt.Pos33VoterSize)))
		if delt < min {
			min = delt
			num = i
			break
		}
	}
	return num
}

func (m *maker) findVm(key, pub string) bool {
	return find(m.mvs, key, pub)
}

func (m *maker) findVb(key, pub string) bool {
	return find(m.bvs, key, pub)
}

func (m *maker) rmBvByPub(pub string, r int) {
	for key, vs := range m.bvs {
		if !m.findVb(key, pub) {
			continue
		}
		var nvs []*pt.Pos33VoteMsg
		for _, v := range vs {
			if string(v.Sig.Pubkey) != pub {
				nvs = append(nvs, v)
			}
		}
		m.bvs[key] = nvs
	}
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

type node struct {
	*Client
	gss *gossip2

	vmp map[int64]map[int]*voter
	mmp map[int64]map[int]*maker
	bch chan *types.Block
	vch chan hr
	sch chan hr

	maxSortHeight int64
	pid           string
	topic         string
}

func newNode(conf *subConfig) *node {
	return &node{
		mmp: make(map[int64]map[int]*maker),
		vmp: make(map[int64]map[int]*voter),
		bch: make(chan *types.Block, 16),
		vch: make(chan hr, 1),
		sch: make(chan hr, 1),
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
				Votes:     vs,
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
	plog.Debug("make a minerTx", "nvs", vsc, "height", height, "fee", tx.Fee, "from", tx.From())
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

	// nb = n.PreExecBlock(nb, false)
	// if nb == nil {
	// 	return nil, errors.New("PreExccBlock error")
	// }
	return nb, nil
}

func (n *node) broadcastBlock(b *types.Block) {
	// if n.pid == "" {
	// 	return
	// }
	m := &pt.Pos33BlockMsg{B: b, Pid: n.pid}
	// if n.conf.TrubleMaker {
	// 	time.AfterFunc(time.Millisecond*3000, func() {
	// 		n.handleBlockMsg(m, true)
	// 	})
	// } else {
	// 	n.handleBlockMsg(m, true)
	// }
	pm := &pt.Pos33Msg{Data: types.Encode(m), Ty: pt.Pos33Msg_B}
	data := types.Encode(pm)
	// n.gss.forwad(data)
	n.gss.gossip(n.topic, data)
	n.handleBlockMsg(m, true)
}

func (n *node) addBlock(b *types.Block) {
	// lastHeight := n.lastBlock().Height
	// if b.Height != lastHeight {
	// 	plog.Error("addBlock height error", "height", b.Height, "lastHeight", lastHeight)
	// 	return
	// }

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
		if h < height-10 {
			delete(n.mmp, h)
		}
	}

	for h := range n.vmp {
		if h < height-10 {
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

	maker := n.getMaker(b.Height, round)
	num := maker.getNum()
	nvs := len(act.Votes)
	all := len(maker.vss[num])
	if nvs*3 < all*2+1 {
		err = fmt.Errorf("checkBlock error: NOT enought votes. all=%d, nvs%d", all, nvs)
		plog.Error("block check error", "err", err, "height", b.Height, "round", round, "from", b.Txs[0].From()[:16])
		return err
	}

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
	if height > 10 && len(n.mmp) > 10 {
		n.sortMaker(seed, height, round)
	}
	n.sortVoter(seed, height, round)
}

func (n *node) firstSortition() {
	seed := zeroHash[:]
	for i := 0; i <= pt.Pos33SortBlocks; i++ {
		height := int64(i)
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
			bvs: make(map[string][]*pt.Pos33VoteMsg),
			mvs: make(map[string][]*pt.Pos33VoteMsg),
			// mvss: make(map[string]*pt.Pos33SortMsg),
			vss: make(map[int]map[string]*pt.Pos33SortMsg),
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
	for i := 0; i < 3; i++ {
		ss := n.voterSort(seed, height, round, BlockVoter, i)
		v := n.getVoter(height, round)
		v.myvss[i] = ss
		n.sendVoterSort(ss, height, round, int(pt.Pos33Msg_VS))
	}
}

/*
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
*/

func (n *node) sortMaker(seed []byte, height int64, round int) {
	plog.Debug("sortMaker", "height", height, "round", round)
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
	num := int(s0.SortHash.Num)
	if num >= 3 {
		plog.Error("handleVoterSort error: sort num >=3", "height", height, "round", round, "num", num, "addr", address.PubKeyToAddr(s0.Proof.Pubkey)[:16])
		return
	}

	mp, ok := maker.vss[num]
	if !ok {
		mp = make(map[string]*pt.Pos33SortMsg)
		maker.vss[num] = mp
	}
	// sty := BlockVoter
	// if ty == int(pt.Pos33Msg_MVS) {
	// 	sty = MakerVoter
	// }

	for _, s := range ss {
		// if !myself {
		// 	seed, err := n.getSortSeed(height - pt.Pos33SortBlocks)
		// 	if err != nil {
		// 		plog.Error("getSeed error", "err", err)
		// 		return
		// 	}
		// 	err = n.checkSort(s, seed, sty)
		// 	if err != nil {
		// 		plog.Error("checkSort error", "err", err)
		// 		return
		// 	}
		// }
		// if ty == int(pt.Pos33Msg_MVS) {
		// 	maker.mvss[string(s.SortHash.Hash)] = s
		// } else if ty == int(pt.Pos33Msg_BVS) {
		// 	maker.bvss[string(s.SortHash.Hash)] = s
		// }
		if num != int(s.SortHash.Num) {
			plog.Error("handleVoterSort error: num Not match", "height", height, "round", round)
			return
		}
		mp[string(s.SortHash.Hash)] = s
		// maker.vss[string(s.SortHash.Hash)] = s
	}
	// if num > 0 {
	// 	n.voteMaker(height, round)
	// }
	plog.Info("handleVoterSort", "all", len(maker.vss[num]), "nvs", len(m.Sorts), "height", height, "round", round, "num", num, "ty", ty, "addr", address.PubKeyToAddr(s0.Proof.Pubkey)[:16])
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

	if n.lastBlock().Height > height {
		return
	}

	maker := n.getMaker(height, round)
	// if maker.ok {
	// 	return
	// }
	// repeat msg
	if ty == int(pt.Pos33Msg_BV) {
		// if maker.findVb(string(m0.Hash), string(m0.Sig.Pubkey)) {
		// 	return
		// }
		maker.rmBvByPub(string(m0.Sig.Pubkey), int(m0.Round))
	} else if ty == int(pt.Pos33Msg_MV) {
		if maker.findVm(string(m0.Hash), string(m0.Sig.Pubkey)) {
			return
		}
	}

	for _, m := range ms {
		if m.Sort == nil || m.Sort.Proof == nil || m.Sort.Proof.Input == nil {
			return
		}
		if m.Sort.Proof.Input.Height != height || int(m.Sort.Proof.Input.Round) != round {
			return
		}
		if m.Round != m0.Round {
			return
		}
		if string(m.Hash) != string(m0.Hash) {
			return
		}

		if !myself {
			if !m.Verify() {
				plog.Info("block voter verify error")
				return
			}
		}

		_, ok := maker.vss[num][string(m.Sort.SortHash.Hash)]
		if !ok {
			plog.Info("can't go here", "height", height, "num", num, "addr", address.PubKeyToAddr(m0.Sig.Pubkey)[:16])
			continue
		}
		if ty == int(pt.Pos33Msg_BV) {
			maker.bvs[string(m.Hash)] = append(maker.bvs[string(m.Hash)], m)
		} else if ty == int(pt.Pos33Msg_MV) {
			maker.mvs[string(m.Hash)] = append(maker.mvs[string(m.Hash)], m)
		}
	}

	plog.Info("handleVoteMsg", "nvs", len(ms), "height", height, "round", round, "ty", ty, "num", num, "addr", address.PubKeyToAddr(m0.Sig.Pubkey)[:16])

	if ty == int(pt.Pos33Msg_BV) {
		n.trySetBlock(height, round, false)
	} else if ty == int(pt.Pos33Msg_MV) {
		if round > 0 {
			n.tryMakeBlock(height, round)
		}
		// if n.lastBlock().Height+1 == height {
		// 	n.tryMakeBlock(height, round)
		// }
	}
}

/*
func (n *node) delForkBlocks(lb *types.Block) {
	lvoter := n.getVoter(lb.Height, 0)
	lm, err := getMiner(lb)
	if err != nil {
		plog.Error("getMiner eror", "err", err, "height", lb.Height)
		return
	}
	lhash := string(lb.Hash(n.GetAPI().GetConfig()))
	sortHash := string(lm.Sort.SortHash.Hash)
	for _, b := range lvoter.ab.bs {
		m, err := getMiner(b)
		if err != nil {
			plog.Error("getMiner eror", "err", err, "height", b.Height)
			continue
		}
		if sortHash == string(m.Sort.SortHash.Hash) {
			lhash = string(b.Hash(n.GetAPI().GetConfig()))
			break
		}
	}

	var bs []*types.Block
	height := lb.Height + 1
	voter := n.getVoter(height, 0)
	for _, b := range voter.ab.bs {
		if string(b.ParentHash) == lhash {
			bs = append(bs, b)
		}
	}
	voter.ab.bs = bs
	plog.Info("deleteForkBlocks", "height", height, "n", len(bs))
}
*/

func (n *node) tryMakeBlock(height int64, round int) {
	maker := n.getMaker(height, round)
	if maker.my == nil {
		return
	}
	if maker.ok {
		return
	}
	mh := string(maker.my.SortHash.Hash)
	vs := maker.mvs[mh]

	plog.Info("try make block", "height", height, "round", round, "vss", len(maker.vss), "nvs", len(vs))
	if len(vs) < 11 {
		plog.Debug("tryMakeBlock nvs < 11", "height", height, "round", round, "vss", len(maker.vss), "nvs", len(vs))
		return
	}

	num := maker.getNum()
	if len(vs) < len(maker.vss[num])*2/3+1 {
		plog.Debug("maker vote NOT enough", "height", height, "round", round, "mbss", len(maker.vss), "nvs", len(vs))
		return
	}

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
	lmk := n.getMaker(height-1, lr)
	lvs := lmk.bvs[string(lb.Hash(n.GetAPI().GetConfig()))]
	if len(lvs) < 11 {
		plog.Info("tryMakeBlock lvs < 11", "height", height, "round", round, "vss", len(lmk.vss), "nvs", len(lvs))
		return
	}
	if len(lvs)*3 < len(lmk.vss[lmk.getNum()])*2 {
		plog.Info("tryMakeBlock nvs < 2/3", "height", height, "round", round, "vss", len(lmk.vss), "nvs", len(lvs))
		return
	}

	nb, err := n.makeBlock(height, round, maker.my, lvs)
	if err != nil {
		plog.Error("makeBlock error", "err", err, "height", height)
		return
	}
	n.broadcastBlock(nb)
	// n.setBlock(nb)
	maker.ok = true
}

func (n *node) trySetBlock(height int64, round int, must bool) bool {
	if n.lastBlock().Height >= height {
		return true
	}

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

	num := maker.getNum()
	if num == -1 {
		return false
	}
	nvss := len(maker.vss[num])
	// if must {
	// 	nvss = sum
	// }
	if max < 11 {
		return false
	}
	plog.Info("try set block", "height", height, "round", round, "num", num, "nbss", nvss, "sum", sum, "max", max, "bh", common.HashHex([]byte(bh))[:16], "must", must)
	if max*3 < nvss*2 {
		r := nvss - sum
		if r >= max {
			return false
		}
		if max > nvss/3+1 {
			n.revoteBlock([]byte(bh), height, round)
		}
		return false

		// failed := false
		// var bhs []string
		// for h, v := range maker.bvs {
		// 	if h != bh {
		// 		if r+len(v) > max {
		// 			failed = true
		// 			break
		// 		}
		// 		if r+len(v) == max {
		// 			bhs = append(bhs, h)
		// 			if r != 0 {
		// 				failed = true
		// 			}
		// 		}
		// 	}
		// }

		// if failed {
		// 	if r <= nvss/3+1 && !must {
		// 		time.AfterFunc(voteBlockDeadline, func() {
		// 			n.sch <- hr{height, round}
		// 		})
		// 	}
		// 	return false
		// }
		// if r == 0 {
		// 	for _, h := range bhs {
		// 		if h < bh {
		// 			bh = h
		// 		}
		// 	}
		// }
	}

	voter := n.getVoter(height, round)
	// 把相应的block写入链
	for _, b := range voter.ab.bs {
		h := string(b.B.Hash(n.GetAPI().GetConfig()))
		if bh == h {
			plog.Info("set block", "height", height, "round", round, "nbss", nvss, "sum", sum, "max", max, "bh", common.HashHex([]byte(h))[:16])
			// if b.Pid == n.pid || b.Pid == "" {
			n.setBlock(b.B)
			// } else {
			// 	n.setOthersBlock(b.B, b.Pid)
			// }
			return true
		}
	}
	return false
}

func (n *node) handleBlockMsg(m *pt.Pos33BlockMsg, myself bool) {
	height := m.B.Height

	if !myself {
		pb := n.lastBlock()
		if pb == nil {
			return
		}
		if height < pb.Height+1 {
			plog.Error("handleBlock error", "err", "height not match", "height", height, "pheight", pb.Height)
			return
		}
		// if string(m.B.ParentHash) != string(pb.Hash(n.GetAPI().GetConfig())) {
		// 	plog.Error("handleBlock error", "err", "parentHash not match", "height", height, "ph", common.HashHex(m.B.ParentHash)[:16])
		// 	return
		// }
		// err := n.blockCheck(m.B)
		// if err != nil {
		// 	plog.Error("handleBlock error", "height", m.B.Height, "err", err)
		// 	return
		// }
	}

	miner, err := getMiner(m.B)
	if err != nil {
		plog.Error("getMiner error", "err", err)
		return
	}
	round := int(miner.Sort.Proof.Input.Round)

	v := n.getVoter(height, round)
	if !v.ab.add(m) {
		return
	}

	hash := m.B.Hash(n.GetAPI().GetConfig())
	plog.Info("handleBlock", "height", height, "round", round, "bh", common.HashHex(hash)[:16], "addr", address.PubKeyToAddr(miner.Sort.Proof.Pubkey)[:16], "time", time.Now().Format("15:04:05.00000"))
	if !v.ab.f {
		v.ab.f = true
		time.AfterFunc(voteBlockWait, func() {
			n.vch <- hr{height, round}
		})
	}
}

func (n *node) voteMaker(height int64, round int) {
	voter := n.getVoter(height, round)
	// if len(voter.myvss) == 0 {
	// 	return
	// }

	var mss []*pt.Pos33SortMsg
	for _, s := range voter.mss {
		mss = append(mss, s)
	}

	if len(mss) == 0 {
		return
	}
	sort.Sort(pt.Sorts(mss))

	maker := n.getMaker(height, round)
	num := maker.getNum()
	if num == -1 {
		plog.Info("getNum error", "height", height, "round", round)
		return
	}

	for i, s := range mss {
		if i == 3 {
			break
		}
		var vs []*pt.Pos33VoteMsg
		for _, mys := range voter.myvss[num] {
			v := &pt.Pos33VoteMsg{
				Hash: s.SortHash.Hash,
				Sort: mys,
			}
			v.Sign(n.priv)
			vs = append(vs, v)
		}
		plog.Info("vote maker", "addr", address.PubKeyToAddr(s.Proof.Pubkey)[:16], "nvs", len(vs), "height", height, "round", round, "num", num)
		n.sendVote(vs, int(pt.Pos33Msg_MV))
	}
}

func (n *node) sendVote(vs []*pt.Pos33VoteMsg, ty int) {
	m := &pt.Pos33Votes{Vs: vs}
	n.handleVoteMsg(vs, true, ty)
	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_Ty(ty),
	}
	data := types.Encode(pm)
	n.gss.gossip(n.topic, data)
	if ty == int(pt.Pos33Msg_BV) {
		n.gss.forwad(data)
	}
}

const voteBlockWait = time.Millisecond * 300
const voteBlockDeadline = time.Millisecond * 900

func (n *node) voteBlock(height int64, round int) {
	voter := n.getVoter(height, round)
	// if len(voter.myvss) == 0 {
	// 	return
	// }
	ab := voter.ab
	if ab.n < len(ab.bs) {
		ab.n = len(ab.bs)
		time.AfterFunc(voteBlockWait, func() {
			n.vch <- hr{height, round}
		})
		return
	}

	minHash := ""
	// var vb *types.Block
	// TODO: must check blocks before vote // PreExecBlock
	for _, b := range voter.ab.bs {
		// m, err := getMiner(b.B)
		// if err != nil {
		// 	continue
		// }
		// h := string(m.Sort.SortHash.Hash)
		h := string(b.B.Hash(n.GetAPI().GetConfig()))
		if minHash == "" {
			minHash = h
			// vb = b.B
		}
		if h < minHash {
			minHash = h
			// vb = b.B
		}
	}
	if minHash == "" {
		return
	}
	n.voteBlockHash([]byte(minHash), height, round)
}

func (n *node) revoteBlock(bh []byte, height int64, round int) {
	maker := n.getMaker(height, round)
	if maker.findVb(string(bh), string(n.priv.PubKey().Bytes())) {
		return
	}

	voter := n.getVoter(height, round)
	if voter.ab.t == 0 {
		return
	}
	plog.Info("revoteBlock", "height", height, "round", round, "bh", common.HashHex(bh)[:16])
	n.voteBlockHash(bh, height, round)
}

func (n *node) voteBlockHash(bh []byte, height int64, round int) {
	voter := n.getVoter(height, round)
	// if len(voter.myvss) == 0 {
	// 	return
	// }

	maker := n.getMaker(height, round)
	num := maker.getNum()
	if num == -1 {
		plog.Info("getNum error", "height", height, "round", round)
		return
	}

	voter.ab.t++
	var vs []*pt.Pos33VoteMsg
	for _, mys := range voter.myvss[num] {
		v := &pt.Pos33VoteMsg{
			Hash:  bh,
			Sort:  mys,
			Round: int32(voter.ab.t),
		}
		v.Sign(n.priv)
		vs = append(vs, v)
	}
	plog.Info("vote block hash", "height", height, "round", round, "t", voter.ab.t, "bh", common.HashHex(bh)[:16], "nvs", len(vs))
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
	}
	round := int(m.Proof.Input.Round)
	v := n.getVoter(height, round)
	v.mss[string(m.SortHash.Hash)] = m
	if round > 0 && height > n.maxSortHeight {
		n.maxSortHeight = height
	}
	plog.Info("handleMakerSort", "nmss", len(v.mss), "height", height, "round", round, "addr", address.PubKeyToAddr(m.Proof.Pubkey)[:16])
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
	case pt.Pos33Msg_VS:
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
	ch := make(chan *pt.Pos33Msg, num*1024)
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
	n.gss = newGossip2(priv, n.conf.ListenPort, ns, n.conf.ForwardServers, n.topic)
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

	plog.Info("pos33 running...", "last block height", lb.Height)

	isSync := false
	syncTick := time.NewTicker(time.Second)
	tch := make(chan int64, 1)
	nch := make(chan int64, 1)

	round := 0
	blockTimeout := time.Second * 4
	resortTimeout := time.Second * 2
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
			n.makeNewBlock(height, round)
			time.AfterFunc(blockTimeout, func() {
				tch <- height
			})
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
			}
			plog.Info("go here", "d", d)
			time.AfterFunc(d, func() {
				nch <- b.Height + 1
			})
		case v := <-n.vch:
			height := v.h
			_, err := n.RequestBlock(height - 1)
			if err != nil {
				plog.Error("requestBlock error", "err", err, "height", height-1)
				time.AfterFunc(time.Millisecond*100, func() { n.vch <- v })
			} else {
				n.voteBlock(v.h, v.r)
			}
		case s := <-n.sch:
			n.trySetBlock(s.h, s.r, true)
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
			n.voteBlock(b.Hash(n.GetAPI().GetConfig()), h, r, n.getMaker(h, r).vr)
			ab.ok = true
			return true
		}
	}
	ab.n++
	return false
}
*/

const calcuDiffN = pt.Pos33SortBlocks * 1

// func (n *node) AddFirstBlock(b *types.Block) {
// 	v := n.getVoter(0, 0)
// 	v.ab.add(b)
// }

func (n *node) handleNewBlock(b *types.Block) {
	// if b.Height < n.lastBlock().Height {
	// 	return
	// }
	round := 0
	if b.Height == 0 {
		n.firstSortition()
		n.voteBlockHash(b.Hash(n.GetAPI().GetConfig()), 0, 0)
		for i := 0; i < 5; i++ {
			n.voteMaker(int64(i), round)
		}
	} else {
		n.sortition(b, round)
	}
	plog.Info("handleNewBlock", "height", b.Height, "round", round)
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
	// lb, err := n.RequestBlock(height - 1)
	// if err != nil {
	// 	plog.Error("requestBlock error", "err", err, "height", height-1)
	// 	return
	// }
	// n.delForkBlocks(lb)
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
	n.gss.gossip(n.topic, types.Encode(pm))
	n.handleVoterSort(m, true, ty)
}

func (n *node) sendMakerSort(m *pt.Pos33SortMsg, height int64, round int) {
	pm := &pt.Pos33Msg{
		Data: types.Encode(m),
		Ty:   pt.Pos33Msg_MS,
	}
	n.gss.gossip(n.topic, types.Encode(pm))
	n.handleMakerSort(m, true)
}

func hexs(b []byte) string {
	s := hex.EncodeToString(b)
	if len(s) <= 16 {
		return s
	}
	return s[:16]
}
