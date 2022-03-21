package executor

import (
	"errors"

	"github.com/33cn/chain33/common/address"
	dbm "github.com/33cn/chain33/common/db"
	"github.com/33cn/chain33/types"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

// Consignee key
func ConsigneeKey(addr string) (key []byte) {
	return []byte("mavl-pos33-consignee-" + addr)
}

// Consignor  key
func ConsignorKey(addr string) (key []byte) {
	return []byte("mavl-pos33-consignor-" + addr)
}

func (act *Action) updateConsignee(addr string, consignee *ty.Pos33Consignee) []*types.KeyValue {
	return []*types.KeyValue{{Key: ConsigneeKey(addr), Value: types.Encode(consignee)}}
}

func (action *Action) updateConsignor(cr *ty.Consignor, consignee string) []*types.KeyValue {
	key := ConsignorKey(cr.Address)
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

func (act *Action) minerReward(addr string, r int64) (*types.Receipt, error) {
	var kvs []*types.KeyValue
	var logs []*types.ReceiptLog

	consignee, err := act.getConsignee(addr)
	if err != nil {
		return nil, err
	}
	fee := r * int64(consignee.FeeRates)
	rr := r - fee
	rr1 := rr / consignee.Amount
	for _, cr := range consignee.Consignors {
		crr := rr1 * cr.Amount
		receipt, err := act.coinsAccount.Transfer(act.execaddr, cr.Address, crr)
		if err != nil {
			tlog.Error("Pos33Miner.ExecDeposit error", "voter", addr, "execaddr", act.execaddr)
			return nil, err
		}
		logs = append(logs, receipt.Logs...)
		kvs = append(kvs, receipt.KV...)
		cr.Reward += crr
	}
	kvs = append(kvs, &types.KeyValue{Key: ConsigneeKey(addr), Value: types.Encode(consignee)})
	return &types.Receipt{KV: kvs, Logs: logs, Ty: types.ExecOk}, nil
}

func (act *Action) voteReward(mp map[string]int, reward int64) (*types.Receipt, error) {
	var kvs []*types.KeyValue
	var logs []*types.ReceiptLog

	for addr, n := range mp {
		consignee, err := act.getConsignee(addr)
		if err != nil {
			return nil, err
		}
		r := reward * int64(n)
		fee := r * int64(consignee.FeeRates)
		rr := r - fee
		rr1 := rr / consignee.Amount
		for _, cr := range consignee.Consignors {
			crr := rr1 * cr.Amount
			receipt, err := act.coinsAccount.Transfer(act.execaddr, cr.Address, crr)
			if err != nil {
				tlog.Error("Pos33Miner.ExecDeposit error", "voter", addr, "execaddr", act.execaddr)
				return nil, err
			}
			logs = append(logs, receipt.Logs...)
			kvs = append(kvs, receipt.KV...)
			cr.Reward += crr
		}
		consignee.FeeReward += fee
		kvs = append(kvs, &types.KeyValue{Key: ConsigneeKey(addr), Value: types.Encode(consignee)})
	}
	return &types.Receipt{KV: kvs, Logs: logs, Ty: types.ExecOk}, nil
}

func (act *Action) getFromBls(pk []byte) (string, error) {
	val, err := act.db.Get(BlsKey(address.PubKeyToAddr(address.DefaultID, pk)))
	if err != nil {
		return "", err
	}
	return string(val), nil
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
	receipt, err := action.voteReward(mp, Pos33VoteReward)
	if err != nil {
		return nil, err
	}
	kvs = append(kvs, receipt.KV...)

	// bp reward
	bpReward := Pos33MakerReward * int64(len(miner.BlsPkList))
	receipt, err = action.minerReward(action.fromaddr, bpReward)
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
	if action.height != 0 && action.fromaddr != pm.MinerAddr {
		return nil, types.ErrFromAddr
	}
	tlog.Info("Pos33BlsBind", "blsaddr", pm.BlsAddr, "minerAddr", pm.MinerAddr)
	return &types.Receipt{KV: []*types.KeyValue{{Key: BlsKey(pm.BlsAddr), Value: []byte(pm.MinerAddr)}}, Ty: types.ExecOk}, nil
}

func (action *Action) Pos33Migrate(pm *ty.Pos33Migrate) (*types.Receipt, error) {
	if action.fromaddr != pm.Miner {
		return nil, types.ErrFromAddr
	}

	d, err := getDeposit(action.db, pm.Miner)
	if err != nil {
		return nil, err
	}
	// delete old deposit
	kv := &types.KeyValue{Key: Key(pm.Miner), Value: nil}

	// set net entrust
	consignor := &ty.Consignor{Address: d.Raddr, Amount: d.Count, Reward: d.Reward}
	consignee, err := action.getConsignee(pm.Miner)
	if err != nil {
		consignee = &ty.Pos33Consignee{Address: pm.Miner, Amount: d.Count}
	}
	consignee.Consignors = append(consignee.Consignors, consignor)
	tlog.Info("Pos33Migrate", "miner", pm.Miner, "raddr", d.Raddr, "amount", d.Count)
	return &types.Receipt{KV: append(action.updateConsignee(pm.Miner, consignee), kv)}, nil
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

	chain33Cfg := action.api.GetConfig()
	cfg := ty.GetPos33TicketMinerParam(chain33Cfg, action.height)
	realAmount := cfg.Pos33TicketPrice * pe.Amount

	var receipt *types.Receipt
	if pe.Amount > 0 {
		receipt, err = action.coinsAccount.TransferToExec(pe.Consignor, action.execaddr, realAmount)
		if err != nil {
			tlog.Error("Pos33Entrust error", "err", err, "height", action.height, "consignor", pe.Consignor, "fromaddr", action.fromaddr)
			return nil, err
		}
		receipt1, err := action.coinsAccount.ExecFrozen(pe.Consignor, action.execaddr, realAmount)
		if err != nil {
			tlog.Error("Pos33Entrust error", "err", err, "height", action.height, "consignor", pe.Consignor, "fromaddr", action.fromaddr)
			return nil, err
		}
		receipt.KV = append(receipt.KV, receipt1.KV...)
		receipt.Logs = append(receipt.Logs, receipt1.Logs...)
	} else {
		receipt, err = action.coinsAccount.ExecActive(pe.Consignor, action.execaddr, -realAmount)
		if err != nil {
			return nil, err
		}
		receipt1, err := action.coinsAccount.TransferWithdraw(pe.Consignor, action.execaddr, -realAmount)
		if err != nil {
			return nil, err
		}
		receipt.KV = append(receipt.KV, receipt1.KV...)
		receipt.Logs = append(receipt.Logs, receipt1.Logs...)
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
