package executor

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/33cn/chain33/common/address"
	dbm "github.com/33cn/chain33/common/db"
	"github.com/33cn/chain33/types"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

// mine param key
func MineParamKey() []byte {
	return []byte("mavl-pos33-mine-param")
}

// Consignee key
func ConsigneeKey(addr string) (key []byte) {
	return []byte("mavl-pos33-consignee-" + addr)
}

// Consignor  key
func ConsignorKey(addr string) (key []byte) {
	return []byte("mavl-pos33-consignor-" + addr)
}

func AllFrozenAmount() []byte {
	return []byte("mavl-pos33-all-frozen")
}

func getAllAmount(db dbm.KV) (int64, error) {
	val, err := db.Get(AllFrozenAmount())
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(string(val))
	if err != nil {
		return 0, err
	}
	return int64(n), nil
}

func (act *Action) updateConsignee(consignee *ty.Pos33Consignee) []*types.KeyValue {
	key := ConsigneeKey(consignee.Address)
	return []*types.KeyValue{{Key: key, Value: types.Encode(consignee)}}
}

func (action *Action) updateConsignor(cr *ty.Consignor, consignee string) []*types.KeyValue {
	consignor, err := getConsignor(action.db, cr.Address)
	if err != nil {
		consignor = &ty.Pos33Consignor{Address: cr.Address}
	}
	found := false
	for _, e := range consignor.Consignees {
		if e.Address == consignee {
			e.Amount = cr.Amount
			found = true
			break
		}
	}
	if !found {
		consignor.Consignees = append(consignor.Consignees, &ty.Consignee{Address: consignee, Amount: cr.Amount})
	}
	key := ConsignorKey(cr.Address)
	return []*types.KeyValue{{Key: key, Value: types.Encode(consignor)}}
}

func getConsignor(db dbm.KV, addr string) (*ty.Pos33Consignor, error) {
	val, err := db.Get(ConsignorKey(addr))
	if err != nil {
		return nil, err
	}
	consignor := new(ty.Pos33Consignor)
	err = types.Decode(val, consignor)
	if err != nil {
		return nil, err
	}
	return consignor, nil
}

func getConsignee(db dbm.KV, addr string) (*ty.Pos33Consignee, error) {
	val, err := db.Get(ConsigneeKey(addr))
	if err != nil {
		return nil, err
	}
	consignee := new(ty.Pos33Consignee)
	err = types.Decode(val, consignee)
	if err != nil {
		return nil, err
	}
	return consignee, nil
}

func (action *Action) getConsignee(addr string) (*ty.Pos33Consignee, error) {
	return getConsignee(action.db, addr)
}

func (act *Action) minerReward(addr string, mineReward int64) (*types.Receipt, error) {
	chain33Cfg := act.api.GetConfig()
	mp := ty.GetPos33MineParam(chain33Cfg, act.height)
	needTransfer := mp.RewardTransfer
	tprice := mp.GetTicketPrice()

	var kvs []*types.KeyValue
	var logs []*types.ReceiptLog

	consignee, err := act.getConsignee(addr)
	if err != nil {
		return nil, err
	}

	r1 := float64(mineReward) / float64(consignee.Amount/tprice)
	for _, cr := range consignee.Consignors {
		crr := int64(r1 * float64(cr.Amount/tprice))
		cr.Reward += crr
		cr.RemainReward += crr
		tlog.Info("reward add", "addr", cr.Address, "reward", cr.Reward, "height", act.height)
		if cr.RemainReward >= needTransfer {
			fee := cr.RemainReward * mp.MinerFeePersent / 100
			consignee.FeeReward += fee
			transferAmount := cr.RemainReward - fee
			receipt, err := act.coinsAccount.Transfer(act.execaddr, cr.Address, transferAmount)
			if err != nil {
				tlog.Error("Pos33Miner.ExecDeposit error", "voter", addr, "execaddr", act.execaddr)
				return nil, err
			}
			cr.RemainReward = 0
			logs = append(logs, receipt.Logs...)
			kvs = append(kvs, receipt.KV...)
			tlog.Info("reward transfer to", "addr", cr.Address, "height", act.height, "amount", transferAmount, "fee", fee)
		}
	}
	kvs = append(kvs, act.updateConsignee(consignee)...)
	return &types.Receipt{KV: kvs, Logs: logs, Ty: types.ExecOk}, nil
}

func (act *Action) voteReward(rds []*rewards, voteReward int64) (*types.Receipt, error) {
	chain33Cfg := act.api.GetConfig()
	mp := ty.GetPos33MineParam(chain33Cfg, act.height)
	needTransfer := mp.RewardTransfer
	tprice := mp.GetTicketPrice()

	var kvs []*types.KeyValue
	var logs []*types.ReceiptLog

	for _, rd := range rds {
		addr := rd.addr
		consignee, err := act.getConsignee(addr)
		if err != nil {
			return nil, err
		}
		vr := voteReward * int64(rd.count)
		r1 := float64(vr) / float64(consignee.Amount/tprice)
		for _, cr := range consignee.Consignors {
			crr := int64(r1 * float64(cr.Amount/tprice))
			cr.Reward += crr
			cr.RemainReward += crr
			if cr.RemainReward >= needTransfer {
				fee := cr.RemainReward * mp.MinerFeePersent / 100
				consignee.FeeReward += fee
				transferAmount := cr.RemainReward - fee
				receipt, err := act.coinsAccount.Transfer(act.execaddr, cr.Address, transferAmount)
				if err != nil {
					tlog.Error("transfer reward error", "height", act.height, "to", cr.Address, "amount", transferAmount)
					return nil, err
				}
				cr.RemainReward = 0
				logs = append(logs, receipt.Logs...)
				kvs = append(kvs, receipt.KV...)
				tlog.Info("reward transfer to", "addr", cr.Address, "height", act.height, "transfer", transferAmount, "fee", fee)
			}
		}
		kvs = append(kvs, act.updateConsignee(consignee)...)
	}
	return &types.Receipt{KV: kvs, Logs: logs, Ty: types.ExecOk}, nil
}

func (act *Action) getFromBls(pk []byte) (string, error) {
	val, err := act.db.Get(BlsKey(address.PubKeyToAddr(address.DefaultID, pk)))
	if err != nil {
		tlog.Error("getFromBls error", "err", err, "height", act.height)
		return "", err
	}
	return string(val), nil
}

type rewards struct {
	addr  string
	count int
}

func (action *Action) Pos33MinerNew(miner *ty.Pos33MinerMsg, index int) (*types.Receipt, error) {
	chain33Cfg := action.api.GetConfig()
	if !chain33Cfg.IsDappFork(action.height, ty.Pos33TicketX, "UseEntrust") {
		return nil, errors.New("config exec.ycc.UseEntrust error")
	}
	if index != 0 {
		return nil, types.ErrCoinBaseIndex
	}

	Coin := chain33Cfg.GetCoinPrecision()
	// Pos33BlockReward 区块奖励
	var Pos33BlockReward = Coin * 30
	// Pos33VoteReward 每ticket区块voter奖励
	var Pos33VoteReward = Coin / 2 // 0.5 ycc
	// Pos33MakerReward 每ticket区块bp奖励
	var Pos33MakerReward = Coin * 22 / 100 // 0.22 ycc

	if chain33Cfg.IsDappFork(action.height, ty.Pos33TicketX, "ForkReward15") {
		Pos33BlockReward /= 2
		Pos33VoteReward /= 2
		Pos33MakerReward /= 2
	}
	if chain33Cfg.IsDappFork(action.height, ty.Pos33TicketX, "UseEntrust") {
		mp := ty.GetPos33MineParam(chain33Cfg, action.height)
		Pos33BlockReward = mp.BlockReward
		Pos33VoteReward = mp.VoteReward
		Pos33MakerReward = mp.MineReward
	}

	var kvs []*types.KeyValue
	var logs []*types.ReceiptLog

	// first issue to execaddr block reward
	{
		// issue coins to exec addr
		receipt, err := action.coinsAccount.ExecIssueCoins(action.execaddr, Pos33BlockReward)
		if err != nil {
			tlog.Error("Pos33TicketMiner.ExecIssueCoins fund to autonomy fund", "addr", action.execaddr, "error", err)
			return nil, err
		}
		logs = append(logs, receipt.Logs...)
		kvs = append(kvs, receipt.KV...)
	}

	// voters reward
	mp := make(map[string]int)
	for _, pk := range miner.BlsPkList {
		addr, err := action.getFromBls(pk)
		if err != nil {
			return nil, err
		}
		mp[addr]++
	}
	rds := make([]*rewards, 0, len(miner.BlsPkList))
	for k, v := range mp {
		rds = append(rds, &rewards{k, v})
	}
	sort.Slice(rds, func(i, j int) bool { return rds[i].addr < rds[j].addr })
	tlog.Info("Pos33MinerNew", "map", mp, "height", action.height)
	receipt, err := action.voteReward(rds, Pos33VoteReward)
	if err != nil {
		tlog.Error("Pos33MinerNew error", "err", err, "height", action.height)
		return nil, err
	}
	kvs = append(kvs, receipt.KV...)

	// bp reward
	bpReward := Pos33MakerReward * int64(len(miner.BlsPkList))
	receipt, err = action.minerReward(action.fromaddr, bpReward)
	if err != nil {
		tlog.Error("Pos33MinerNew error", "err", err, "height", action.height)
		return nil, err
	}
	kvs = append(kvs, receipt.KV...)

	// fund reward
	fundReward := Pos33BlockReward - (Pos33VoteReward+Pos33MakerReward)*int64(len(miner.BlsPkList))
	fundaddr := chain33Cfg.MGStr("mver.consensus.fundKeyAddr", action.height)
	tlog.Info("fund rerward", "fundaddr", fundaddr, "height", action.height, "reward", fundReward)

	receipt, err = action.coinsAccount.Transfer(action.execaddr, fundaddr, fundReward)
	if err != nil {
		tlog.Error("fund reward error", "error", err, "fund", fundaddr, "value", fundReward)
		return nil, err
	}
	logs = append(logs, receipt.Logs...)
	kvs = append(kvs, receipt.KV...)

	return &types.Receipt{Ty: types.ExecOk, KV: kvs, Logs: logs}, nil
}

func (action *Action) Pos33BlsBind(pm *ty.Pos33BlsBind) (*types.Receipt, error) {
	miner := action.fromaddr
	if action.height == 0 {
		miner = pm.MinerAddr
	}
	tlog.Info("bls bind", "blsaddr", pm.BlsAddr, "minerAddr", miner)
	return &types.Receipt{KV: []*types.KeyValue{{Key: BlsKey(pm.BlsAddr), Value: []byte(miner)}}, Ty: types.ExecOk}, nil
}

func (action *Action) updateAllAmount(newAmount int64) *types.KeyValue {
	allAmount, err := getAllAmount(action.db)
	if err != nil {
		tlog.Error("getAllAmount error", "err", err)
	}
	allAmount += newAmount
	value := []byte(fmt.Sprintf("%d", allAmount))
	tlog.Info("updateAllAmount", "height", action.height, "allAmount", allAmount, "newAmount", newAmount)
	return &types.KeyValue{Key: AllFrozenAmount(), Value: value}
}

func (action *Action) Pos33Migrate(pm *ty.Pos33Migrate) (*types.Receipt, error) {
	if action.fromaddr != pm.Miner {
		return nil, types.ErrFromAddr
	}

	d, err := getDeposit(action.db, pm.Miner)
	if err != nil {
		return nil, err
	}

	req := &types.ReqBalance{Addresses: []string{d.Raddr}, Execer: ty.Pos33TicketX}
	accs, err := action.coinsAccount.GetBalance(action.api, req)
	if err != nil {
		tlog.Error("GetBalance error", "err", err, "height", action.height)
		return nil, err
	}
	acc := accs[0]
	amount := acc.Frozen
	tlog.Info("pos33 migrate", "miner", action.fromaddr, "consignor", acc.Addr, "amount", amount)
	return action.setEntrust(&ty.Pos33Entrust{Consignee: action.fromaddr, Consignor: acc.Addr, Amount: amount})
}

func (action *Action) freeze(addr string, amount int64) (*types.Receipt, error) {
	var receipt *types.Receipt
	var err error
	if amount > 0 {
		receipt, err = action.coinsAccount.ExecFrozen(addr, action.execaddr, amount)
		if err != nil {
			tlog.Error("freeze error", "err", err, "height", action.height, "consignor", addr)
			return nil, err
		}
	} else {
		receipt, err = action.coinsAccount.ExecActive(addr, action.execaddr, -amount)
		if err != nil {
			tlog.Error("freeze error", "err", err, "height", action.height, "consignor", addr)
			return nil, err
		}
	}
	tlog.Info("freeze", "height", action.height, "addr", addr, "amount", amount)
	return receipt, nil
}

func (action *Action) Pos33Entrust(pe *ty.Pos33Entrust) (*types.Receipt, error) {
	receipt, err := action.setEntrust(pe)
	if err != nil {
		return nil, err
	}
	receipt1, err := action.freeze(pe.Consignor, pe.Amount)
	if err != nil {
		return nil, err
	}
	receipt.KV = append(receipt.KV, receipt1.KV...)
	receipt.Logs = append(receipt.Logs, receipt1.Logs...)
	return receipt, nil
}

func (action *Action) setEntrust(pe *ty.Pos33Entrust) (*types.Receipt, error) {
	if action.height == 0 {
		action.fromaddr = pe.Consignor
	}
	if action.fromaddr != pe.Consignor && action.fromaddr != pe.Consignee {
		return nil, types.ErrFromAddr
	}
	if pe.Amount == 0 {
		return nil, types.ErrAmount
	}

	chain33Cfg := action.api.GetConfig()
	mp := ty.GetPos33MineParam(chain33Cfg, action.height)

	consignee, err := action.getConsignee(pe.Consignee)
	if err != nil {
		tlog.Error("setEntrust error", "err", err, "height", action.height, "consignee", pe.Consignee)
		consignee = &ty.Pos33Consignee{Address: pe.Consignee, FeePersent: mp.MinerFeePersent}
	}
	var consignor *ty.Consignor
	for _, cr := range consignee.Consignors {
		if cr.Address == pe.Consignor {
			consignor = cr
			break
		}
	}
	if pe.Amount < 0 && consignor == nil {
		return nil, types.ErrAmount
	}

	if consignor == nil {
		consignor = &ty.Consignor{Address: pe.Consignor, Amount: 0}
		consignee.Consignors = append(consignee.Consignors, consignor)
	}

	consignee.Amount += pe.Amount
	consignor.Amount += pe.Amount
	kvs := action.updateConsignor(consignor, pe.Consignee)
	kvs = append(kvs, action.updateConsignee(consignee)...)
	kvs = append(kvs, action.updateAllAmount(pe.Amount))

	tlog.Info("pos33 set entrust", "consignor", consignor.Address[:16], "consignee", consignee.Address[:16], "amount", pe.Amount)
	return &types.Receipt{KV: kvs, Ty: types.ExecOk}, nil
}

func (action *Action) minerWithdrawFee(amount int64, consignee *ty.Pos33Consignee) (*types.Receipt, error) {
	if consignee.FeeReward <= amount {
		return nil, types.ErrAmount
	}
	consignee.FeeReward -= amount
	receipt, err := action.coinsAccount.Transfer(action.execaddr, consignee.Address, amount)
	if err != nil {
		tlog.Error("miner withdrawFee error", "err", err, "height", action.height)
		return nil, err
	}
	kv := action.updateConsignee(consignee)
	receipt.KV = append(receipt.KV, kv...)
	tlog.Info("miner withdraw", "height", action.height, "miner", consignee.Address, "amount", amount)
	return receipt, nil
}

func (action *Action) Pos33WithdrawReward(wr *ty.Pos33WithdrawReward) (*types.Receipt, error) {
	if action.fromaddr != wr.Consignor {
		return nil, types.ErrFromAddr
	}
	if wr.Amount <= 0 {
		return nil, types.ErrAmount
	}
	consignee, err := action.getConsignee(wr.Consignee)
	if err != nil {
		tlog.Error("pos33 withdrawReward error", "err", err, "height", action.height, "consignee", wr.Consignee, "consignor", wr.Consignor)
		return nil, err
	}

	if wr.Consignor == wr.Consignee {
		return action.minerWithdrawFee(wr.Amount, consignee)
	}

	var consignor *ty.Consignor
	for _, cr := range consignee.Consignors {
		if cr.Address == wr.Consignor {
			consignor = cr
			break
		}
	}
	if consignor == nil {
		tlog.Error("pos33 withdrawReward error", "err", "not entrust with consignee", "height", action.height, "consignee", wr.Consignee, "consignor", wr.Consignor)
		return nil, errors.New("not entrust with consignee")
	}
	if consignor.RemainReward < wr.Amount {
		return nil, types.ErrAmount
	}

	amount := wr.Amount
	needFee := float64(amount*int64(consignee.FeePersent)) / 100.
	consignee.FeeReward += int64(needFee)

	consignor.RemainReward -= int64(needFee)
	if consignor.RemainReward < amount {
		amount = consignor.RemainReward
		consignor.RemainReward = 0
	} else {
		consignor.RemainReward -= amount
	}

	receipt, err := action.coinsAccount.Transfer(action.execaddr, wr.Consignor, amount)
	if err != nil {
		tlog.Error("withdrawReward error", "err", err, "height", action.height)
		return nil, err
	}
	kv := action.updateConsignee(consignee)
	receipt.KV = append(receipt.KV, kv...)
	tlog.Info("withdrawReward ", "height", action.height, "consignor", wr.Consignor, "miner", wr.Consignee, "amount", wr.Amount)
	return receipt, nil
}

func (action *Action) Pos33SetMinerFeeRate(fr *ty.Pos33MinerFeeRate) (*types.Receipt, error) {
	consignee, err := action.getConsignee(action.fromaddr)
	if err != nil {
		tlog.Error("pos33 set miner feerate error", "err", err, "height", action.height, "miner", action.fromaddr)
		return nil, err
	}
	consignee.FeePersent = int64(fr.FeeRatePersent)
	kv := action.updateConsignee(consignee)
	return &types.Receipt{KV: kv}, nil
}
