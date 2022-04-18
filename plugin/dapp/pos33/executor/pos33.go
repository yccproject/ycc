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

func (act *Action) updateConsignee(addr string, consignee *ty.Pos33Consignee) []*types.KeyValue {
	return []*types.KeyValue{{Key: ConsigneeKey(addr), Value: types.Encode(consignee)}}
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
	needTransfer := float64(mp.RewardTransfer)

	var kvs []*types.KeyValue
	var logs []*types.ReceiptLog

	consignee, err := act.getConsignee(addr)
	if err != nil {
		return nil, err
	}
	feeRate := float64(consignee.FeeRatePersent) / 100.
	needFee := float64(needTransfer) * feeRate

	rr1 := mineReward / consignee.Amount
	for _, cr := range consignee.Consignors {
		crr := rr1 * cr.Amount
		cr.Reward += crr
		cr.RemainReward += crr
		if cr.RemainReward >= int64(needTransfer+needFee) {
			receipt, err := act.coinsAccount.Transfer(act.execaddr, cr.Address, mp.RewardTransfer)
			if err != nil {
				tlog.Error("Pos33Miner.ExecDeposit error", "voter", addr, "execaddr", act.execaddr)
				return nil, err
			}
			cr.RemainReward -= int64(needTransfer + needFee)
			consignee.FeeReward += int64(needFee)
			logs = append(logs, receipt.Logs...)
			kvs = append(kvs, receipt.KV...)
			tlog.Info("reward transfer to", "addr", cr.Address, "height", act.height)
		}
	}
	kvs = append(kvs, &types.KeyValue{Key: ConsigneeKey(addr), Value: types.Encode(consignee)})
	return &types.Receipt{KV: kvs, Logs: logs, Ty: types.ExecOk}, nil
}

func (act *Action) voteReward(rds []*rewards, voteReward int64) (*types.Receipt, error) {
	chain33Cfg := act.api.GetConfig()
	mp := ty.GetPos33MineParam(chain33Cfg, act.height)
	needTransfer := float64(mp.RewardTransfer)
	// needFee := needTransfer * float64(mp.MinerFeePersent) / 100

	var kvs []*types.KeyValue
	var logs []*types.ReceiptLog

	for _, rd := range rds {
		addr := rd.addr
		consignee, err := act.getConsignee(addr)
		if err != nil {
			return nil, err
		}
		feeRate := float64(consignee.FeeRatePersent) / 100.
		needFee := float64(needTransfer) * feeRate
		r := voteReward * int64(rd.count)
		rr1 := r / consignee.Amount
		for _, cr := range consignee.Consignors {
			crr := rr1 * cr.Amount
			cr.Reward += crr
			cr.RemainReward += crr
			if cr.RemainReward >= int64(needTransfer+needFee) {
				receipt, err := act.coinsAccount.Transfer(act.execaddr, cr.Address, mp.RewardTransfer)
				if err != nil {
					tlog.Error("Pos33Miner.ExecDeposit error", "voter", addr, "execaddr", act.execaddr)
					return nil, err
				}
				cr.RemainReward -= int64(needTransfer + needFee)
				consignee.FeeReward += int64(needFee)
				logs = append(logs, receipt.Logs...)
				kvs = append(kvs, receipt.KV...)
				tlog.Info("reward transfer to", "addr", cr.Address, "height", act.height)
			}
		}
		kvs = append(kvs, &types.KeyValue{Key: ConsigneeKey(addr), Value: types.Encode(consignee)})
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
	tlog.Info("Pos33BlsBind", "blsaddr", pm.BlsAddr, "minerAddr", pm.MinerAddr)
	miner := action.fromaddr
	if action.height == 0 {
		miner = pm.MinerAddr
	}
	return &types.Receipt{KV: []*types.KeyValue{{Key: BlsKey(pm.BlsAddr), Value: []byte(miner)}}, Ty: types.ExecOk}, nil
}

func (action *Action) migrateAllCount() *types.KeyValue {
	chain33Cfg := action.api.GetConfig()
	mp := ty.GetPos33MineParam(chain33Cfg, action.height)
	count := getAllCount(action.db)
	amount := mp.GetTicketPrice() * int64(count)
	value := []byte(fmt.Sprintf("%d", amount))
	tlog.Info("Pos33 mirate All tickets Count to frozen ycc", "height", action.height, "allAmount", amount)
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

	// use new price calculate amount
	req := &types.ReqBalance{Addresses: []string{d.Raddr}, Execer: ty.Pos33TicketX}
	accs, err := action.coinsAccount.GetBalance(action.api, req)
	if err != nil {
		tlog.Error("GetBalance error", "err", err, "height", action.height)
		return nil, err
	}
	acc := accs[0]
	chain33Cfg := action.api.GetConfig()
	mp := ty.GetPos33MineParam(chain33Cfg, action.height)
	feeRate := mp.MinerFeePersent
	amount := acc.Frozen

	// set net entrust
	consignor := &ty.Consignor{Address: d.Raddr, Amount: amount, Reward: d.Reward}
	consignee, err := action.getConsignee(pm.Miner)
	if err != nil {
		consignee = &ty.Pos33Consignee{Address: pm.Miner, FeeRatePersent: int32(feeRate)}
	}
	consignee.Amount += amount
	consignee.Consignors = append(consignee.Consignors, consignor)
	tlog.Info("Pos33Migrate", "miner", pm.Miner, "raddr", d.Raddr, "amount", amount)

	var kvs []*types.KeyValue
	kvs = append(kvs, action.updateConsignee(pm.Miner, consignee)...)
	kvs = append(kvs, action.migrateAllCount())
	return &types.Receipt{KV: kvs, Ty: types.ExecOk}, nil
}

func (action *Action) Pos33Entrust(pe *ty.Pos33Entrust) (*types.Receipt, error) {
	if action.height == 0 {
		action.fromaddr = pe.Consignor
	}
	if action.fromaddr != pe.Consignor {
		return nil, types.ErrFromAddr
	}
	if pe.Amount == 0 {
		return nil, types.ErrAmount
	}

	consignee, err := action.getConsignee(pe.Consignee)
	if err != nil {
		tlog.Error("Pos33Entrust error", "err", err, "height", action.height, "consignee", pe.Consignee)
		consignee = &ty.Pos33Consignee{Address: pe.Consignee}
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

	var receipt *types.Receipt
	if pe.Amount > 0 {
		receipt, err = action.coinsAccount.ExecFrozen(pe.Consignor, action.execaddr, pe.Amount)
		if err != nil {
			tlog.Error("Pos33Entrust error", "err", err, "height", action.height, "consignor", pe.Consignor, "fromaddr", action.fromaddr)
			return nil, err
		}
	} else {
		receipt, err = action.coinsAccount.ExecActive(pe.Consignor, action.execaddr, pe.Amount)
		if err != nil {
			return nil, err
		}
	}

	consignee.Amount += pe.Amount
	consignor.Amount += pe.Amount
	kvs := action.updateConsignor(consignor, pe.Consignee)
	kvs = append(kvs, setNewCount(action.db, int(pe.Amount)))
	kvs = append(kvs, action.updateConsignee(pe.Consignee, consignee)...)
	receipt.KV = append(receipt.KV, kvs...)

	tlog.Info("Pos33Entrust set entrust", "consignor", consignor.Address[:16], "consignee", consignee.Address[:16], "amount", pe.Amount, "consignor amount", consignor.Amount, "consignee amount", consignee.Amount)
	return receipt, nil
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
	kv := action.updateConsignee(consignee.Address, consignee)
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
	needFee := float64(amount*int64(consignee.FeeRatePersent)) / 100.
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
	kv := action.updateConsignee(wr.Consignee, consignee)
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
	consignee.FeeRatePersent = fr.FeeRatePersent
	kv := action.updateConsignee(action.fromaddr, consignee)
	return &types.Receipt{KV: kv}, nil
}
