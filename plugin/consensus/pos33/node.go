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
	ok bool
	n  int
	bs []*types.Block
}

func (ab *alterBlock) add(nb *types.Block) bool {
	for _, b := range ab.bs {
		if string(b.TxHash) == string(nb.TxHash) {
			return false
		}
	}
	ab.bs = append(ab.bs, nb)
	return true
}

type voter struct {
	mymvss []*pt.Pos33SortMsg          // 我作为 maker voter 的抽签
	mybvss []*pt.Pos33SortMsg          // 我作为 block voter 的抽签
	mss    map[string]*pt.Pos33SortMsg // 我都到的 maker 的抽签
	ab     *alterBlock                 // 我接收到所有备选block
}

// 区块制作人
type maker struct {
	my     *pt.Pos33SortMsg              // 我的抽签
	mvss   map[string]*pt.Pos33SortMsg   // 我收到的 maker vote 的抽签
	bvss   map[string]*pt.Pos33SortMsg   // 我收到的 block vote 的抽签
	mvs    map[string][]*pt.Pos33VoteMsg // 我收到的 maker vote
	bvs    map[string][]*pt.Pos33VoteMsg // 我收到的 block vote
	vr     int
	makeok bool // 区块是否制作完成
	ok     bool // 区块是否写入链，表示本轮完成
}

func (m *maker) findVm(key, pub string) bool {
	return find(m.mvs, key, pub)
}

func (m *maker) findVb(key, pub string) bool {
	return find(m.bvs, key, pub)
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

func (n *node) makeBlock(lb *types.Block, round int, sort *pt.Pos33SortMsg, vs []*pt.Pos33VoteMsg) (*types.Block, error) {
	height := lb.Height + 1

	priv := n.getPriv()
	if priv == nil {
		panic("can't go here")
	}

	tx, err := n.minerTx(height, sort, vs, priv)
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
	m := &pt.Pos33BlockMsg{B: b, Pid: n.pid}
	// if n.conf.TrubleMaker {
	// 	time.AfterFunc(time.Millisecond*3000, func() {
	// 		n.handleBlockMsg(m, true)
	// 	})
	// } else {
	// 	n.handleBlockMsg(m, true)
	// }
	pm := &pt.Pos33Msg{Data: types.Encode(m), Ty: pt.Pos33Msg_B}
	n.gss.gossip(n.topic, types.Encode(pm))
	n.handleBlockMsg(m, true)
}

func (n *node) addBlock(b *types.Block) {
	// lastHeight := n.lastBlock().Height
	// if b.Height != lastHeight {
	// 	plog.Error("addBlock height error", "height", b.Height, "lastHeight", lastHeight)
	// 	return
	// }

	plog.Info("block add", "height", b.Height, "hash", common.ToHex(b.Hash(n.GetAPI().GetConfig()))[:16])
	// if b.BlockTime-n.lastBlock().BlockTime < 1 {
	// 	time.AfterFunc(time.Millisecond*500, func() {
	// 		n.pushBlock(b)
	// 	})
	// } else {
	// 	n.pushBlock(b)
	// }
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
	voter := n.getVoter(b.Height, 0)
	if voter.ab != nil && len(voter.ab.bs) > 0 {
		n.sortMaker(seed, height, round)
	}
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

	plog.Info("handleVoterSort", "nvs", len(m.Sorts), "height", height, "round", round, "ty", ty, "addr", address.PubKeyToAddr(s0.Proof.Pubkey)[:16])

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

	if n.lastBlock().Height >= height {
		plog.Info("go here0")
		return
	}

	maker := n.getMaker(height, round)
	if maker.ok {
		plog.Info("go here1")
		return
	}
	// repeat msg
	if ty == int(pt.Pos33Msg_BV) {
		if maker.findVb(string(m0.Hash), string(m0.Sig.Pubkey)) {
			return
		}
	} else if ty == int(pt.Pos33Msg_MV) {
		if maker.findVm(string(m0.Hash), string(m0.Sig.Pubkey)) {
			return
		}
	}

	plog.Info("handleVoteMsg", "nvs", len(ms), "height", height, "round", round, "ty", ty, "addr", address.PubKeyToAddr(m0.Sig.Pubkey)[:16])

	for _, m := range ms {
		if m.Sort == nil || m.Sort.Proof == nil || m.Sort.Proof.Input == nil {
			return
		}
		if m.Sort.Proof.Input.Height != height || int(m.Sort.Proof.Input.Round) != round {
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

		if ty == int(pt.Pos33Msg_BV) {
			_, ok := maker.bvss[string(m.Sort.SortHash.Hash)]
			if !ok {
				continue
			}
			if maker.vr == int(m.Round) {
				maker.bvs[string(m.Hash)] = append(maker.bvs[string(m.Hash)], m)
			}
		} else if ty == int(pt.Pos33Msg_MV) {
			_, ok := maker.mvss[string(m.Sort.SortHash.Hash)]
			if !ok {
				continue
			}
			maker.mvs[string(m.Hash)] = append(maker.mvs[string(m.Hash)], m)
		}
	}

	if ty == int(pt.Pos33Msg_BV) {
		n.trySetBlock(height, round, false)
	} else if ty == int(pt.Pos33Msg_MV) {
		if round > 0 {
			// n.tryMakeBlock(height, round)
		}
	}
}

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

func (n *node) tryMakeBlock(height int64, round int) {
	maker := n.getMaker(height, round)
	if maker.my == nil {
		plog.Info("go here2")
		return
	}
	if maker.makeok {
		plog.Info("go here3")
		return
	}
	mh := string(maker.my.SortHash.Hash)
	vs := maker.mvs[mh]

	plog.Info("try make block", "height", height, "round", round, "mbss", len(maker.mvss), "nvs", len(vs))
	if len(vs) < len(maker.mvss)/2+1 {
		plog.Error("maker vote NOT enough", "height", height, "round", round, "mbss", len(maker.mvss), "nvs", len(vs))
		return
	}

	// lbh := string(zeroHash[:])
	// if height > 1 {
	// 	lb, err := n.RequestBlock(height - 2)
	// 	if err != nil {
	// 		plog.Error("requestBlock error", "err", err, "height", height)
	// 		return
	// 	}
	// 	lbh = string(lb.Hash(n.GetAPI().GetConfig()))
	// }

	voter := n.getVoter(height-1, 0)
	for _, b := range voter.ab.bs {
		// if string(b.ParentHash) != lbh {
		// 	plog.Error("lastBlock not match", "height", height)
		// 	continue
		// }
		nb, err := n.makeBlock(b, round, maker.my, vs)
		if err != nil {
			plog.Error("makeBlock error", "err", err, "height", height)
			continue
		}
		n.broadcastBlock(nb)
	}
	maker.makeok = true
}

func (n *node) trySetBlock(height int64, round int, must bool) bool {
	// if height == 1 {
	// 	return true
	// }
	maker := n.getMaker(height, round)
	if maker.ok {
		return true
	}

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
	if must {
		nvss = sum
	}
	if max == 0 {
		return false
	}
	plog.Info("try set block", "height", height, "round", round, "nbss", nvss, "sum", sum, "max", max, "bh", common.HashHex([]byte(bh))[:16], "must", must)
	if max < nvss/2+1 {
		r := nvss - sum
		if r >= max {
			return false
		}

		failed := false
		var bhs []string
		for h, v := range maker.bvs {
			if h != bh {
				if r+len(v) > max {
					failed = true
					break
				}
				if r+len(v) == max {
					bhs = append(bhs, h)
					if r != 0 {
						failed = true
					}
				}
			}
		}

		if failed {
			if r <= nvss/3+1 && !must {
				time.AfterFunc(voteBlockDeadline, func() {
					n.sch <- hr{height, round}
				})
			}
			return false
		}
		if r == 0 {
			for _, h := range bhs {
				if h < bh {
					bh = h
				}
			}
		}
	}

	voter := n.getVoter(height, round)
	// 把相应的block写入链
	for _, b := range voter.ab.bs {
		h := string(b.Hash(n.GetAPI().GetConfig()))
		if bh == h {
			maker.ok = true
			plog.Info("set block", "height", height, "round", round, "nbss", nvss, "sum", sum, "max", max, "bh", common.HashHex([]byte(h))[:16])
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
		if height < pb.Height+1 {
			plog.Error("handleBlock error", "err", "height not match", "height", height, "pheight", pb.Height)
			return
		}
		// if string(m.B.ParentHash) != string(pb.Hash(n.GetAPI().GetConfig())) {
		// 	plog.Error("handleBlock error", "err", "parentHash not match")
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
	if !v.ab.add(m.B) {
		return
	}

	hash := m.B.Hash(n.GetAPI().GetConfig())
	num := miner.Sort.SortHash.Num
	plog.Info("handleBlock", "height", height, "round", round, "num", num, "n", len(v.ab.bs), "bh", common.HashHex(hash)[:16], "time", time.Now().String())
	if !v.ab.ok {
		time.AfterFunc(voteBlockWait, func() {
			n.vch <- hr{height, round}
		})
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

	// plog.Info("vote maker", "height", height, "round", round, "nmss", len(mss))
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
				plog.Info("vote maker", "addr", address.PubKeyToAddr(s.Proof.Pubkey)[:16], "nvs", len(vs), "height", height, "round", round, "num", i)
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
	n.gss.gossip(n.topic, types.Encode(pm))
}

const voteBlockWait = time.Millisecond * 500
const voteBlockDeadline = time.Millisecond * 900

func (n *node) voteBlock(height int64, round int) {
	voter := n.getVoter(height, round)
	if len(voter.mybvss) == 0 {
		return
	}
	ab := voter.ab
	if ab.ok {
		return
	}
	// if ab.n < len(ab.bs) {
	// 	ab.n = len(ab.bs)
	// 	time.AfterFunc(voteBlockWait, func() {
	// 		n.vch <- hr{height, round}
	// 	})
	// 	return
	// }

	minHash := ""
	var vb *types.Block
	for _, b := range voter.ab.bs {
		m, err := getMiner(b)
		if err != nil {
			continue
		}
		h := string(m.Sort.SortHash.Hash)
		if minHash == "" {
			minHash = h
			vb = b
		}
		if h < minHash {
			minHash = h
			vb = b
		}
	}
	if vb == nil {
		return
	}
	voter.ab.ok = true

	bh := vb.Hash(n.GetAPI().GetConfig())
	var vs []*pt.Pos33VoteMsg
	for _, mys := range voter.mybvss {
		v := &pt.Pos33VoteMsg{
			Hash: bh,
			Sort: mys,
		}
		v.Sign(n.priv)
		vs = append(vs, v)
	}
	plog.Info("vote block", "height", height, "round", round, "bh", common.HashHex(bh)[:16], "nvs", len(vs))
	n.sendVote(vs, int(pt.Pos33Msg_BV))
}

func (n *node) handleMakerSort(m *pt.Pos33SortMsg, myself bool) {
	if m == nil || m.Proof == nil || m.Proof.Input == nil || m.SortHash == nil {
		plog.Error("handleSortitionMsg error, input msg is nil")
		return
	}
	height := m.Proof.Input.Height
	if !myself {
		// if height > n.lastBlock().Height+pt.Pos33SortBlocks*2 {
		// 	plog.Error("handleSort height too hight", "height", height)
		// 	return
		// }
		// if n.lastBlock().Height >= height {
		// 	err := fmt.Errorf("sort msg too late, lbHeight=%d, sortHeight=%d", n.lastBlock().Height, height)
		// 	plog.Error("handleSort error", "err", err)
		// 	return
		// }
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
	cr := newCrp(2048)
	ch := make(chan *pt.Pos33Msg, num*16)
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
	go func() {
		for range time.Tick(time.Second * 10) {
			cr.rm(2048)
		}
	}()
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
	if len(list.Peers) == 0 {
		return
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
	n.topic = title + pos33Topic
	ns := fmt.Sprintf("%s-%d", title, n.conf.ListenPort)
	n.gss = newGossip2(priv, n.conf.ListenPort, ns, n.conf.ForwardServers, n.topic)
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
	n.handleBlockMsg(&pt.Pos33BlockMsg{B: lb}, true)

	plog.Info("pos33 running...", "last block height", lb.Height)

	isSync := false
	syncTick := time.NewTicker(time.Second)
	tch := make(chan int64, 1)
	nch := make(chan int64, 1)
	// ach := make(chan hr, 1)
	round := 0
	blockTimeout := time.Second * 3
	resortTimeout := time.Second * 2

	for {
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
					nch <- nh
				})
			}
		case height := <-nch:
			n.makeNewBlock(height, round)
			// time.AfterFunc(time.Millisecond*700, func() {
			// 	ach <- hr{height, round}
			// })
			time.AfterFunc(blockTimeout, func() {
				tch <- height
			})
		case b := <-n.bch: // new block add to chain
			round = 0
			n.handleNewBlock(b)
			time.AfterFunc(time.Millisecond, func() {
				nch <- b.Height + 1
			})
		case v := <-n.vch:
			n.voteBlock(v.h, v.r)
			n.tryMakeBlock(v.h+1, 0)
		case s := <-n.sch:
			n.trySetBlock(s.h, s.r, true)
		// case t := <-ach:
		// 	lb := n.lastBlock()
		// 	if lb.Height < t.h {
		// 		ok := n.handleAlterBlock(t.h, t.r)
		// 		if !ok {
		// 			time.AfterFunc(time.Millisecond*700, func() {
		// 				ach <- t
		// 			})
		// 		}
		// 	}
		default:
			time.Sleep(time.Millisecond * 10)
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

func (n *node) AddFirstBlock(b *types.Block) {
	v := n.getVoter(0, 0)
	v.ab.add(b)
}

func (n *node) handleNewBlock(b *types.Block) {
	// if b.Height < n.lastBlock().Height {
	// 	return
	// }
	round := 0
	if b.Height == 0 {
		n.firstSortition()
	} else {
		n.sortition(b, round)
	}
	plog.Info("handleNewBlock", "height", b.Height, "round", round)
	if b.Height == 0 {
		for i := 1; i < 5; i++ {
			n.voteMaker(int64(i), 0)
		}
		n.AddFirstBlock(b)
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
	if height == 1 {
		n.tryMakeBlock(1, 0)
		return
	}
	// lb := n.lastBlock()
	lb, err := n.RequestBlock(height - 1)
	if err != nil {
		plog.Error("requestBlock error", "err", err, "height", height-1)
		return
	}
	n.delForkBlocks(lb)
	// n.tryMakeBlock(height+1, round)
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
