package executor

import (
	"errors"

	"github.com/33cn/chain33/common/address"
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

func (action *Action) updateConsignor(pe *ty.Pos33Entrust) ([]*types.KeyValue, error) {
	key := ConsignorKey(pe.Consignor)
	val, err := action.db.Get(key)
	if err != nil {
		return nil, err
	}
	consignor := new(ty.Pos33Consignor)
	err = types.Decode(val, consignor)
	if err != nil {
		return nil, err
	}

	var consignee *ty.Consignee
	for _, ce := range consignor.Consignees {
		if ce.Address == pe.Consignee {
			consignee = ce
			break
		}
	}
	if consignee == nil {
		consignee = &ty.Consignee{Address: pe.Consignee, Amount: 0}
		consignor.Consignees = append(consignor.Consignees, consignee)
	}
	consignee.Amount += pe.Amount

	return []*types.KeyValue{{Key: key, Value: types.Encode(consignor)}}, nil
}

func (action *Action) getConsignee(addr string) (*ty.Pos33Consignee, error) {
	val, err := action.db.Get(ConsigneeKey(addr))
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
	return &types.Receipt{KV: kvs, Logs: logs}, nil
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
	return &types.Receipt{KV: kvs, Logs: logs}, nil
}

func (act *Action) getFromBls(pk []byte) (string, error) {
	val, err := act.db.Get(BlsKey(address.PubKeyToAddr(pk)))
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
		var receipt *types.Receipt
		var err error
		// issue coins to exec addr
		receipt, err = action.coinsAccount.ExecIssueCoins(action.execaddr, Pos33BlockReward)
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
	action.voteReward(mp, Pos33VoteReward)

	// bp reward
	bpReward := Pos33MakerReward * int64(len(miner.BlsPkList))
	action.minerReward(action.fromaddr, bpReward)

	// fund reward
	fundReward := Pos33BlockReward - (Pos33VoteReward+Pos33MakerReward)*int64(len(miner.BlsPkList))
	fundaddr := chain33Cfg.MGStr("mver.consensus.fundKeyAddr", action.height)
	tlog.Info("fund rerward", "fundaddr", fundaddr, "height", action.height, "reward", fundReward)

	receipt, err := action.coinsAccount.Transfer(action.execaddr, fundaddr, fundReward)
	if err != nil {
		tlog.Error("fund reward error", "error", err, "fund", fundaddr, "value", fundReward)
		return nil, err
	}
	logs = append(logs, receipt.Logs...)
	kvs = append(kvs, receipt.KV...)

	return &types.Receipt{Ty: types.ExecOk, KV: kvs, Logs: logs}, nil
}

func (action *Action) Pos33BlsBind(pm *ty.Pos33BlsBind) (*types.Receipt, error) {
	chain33Cfg := action.api.GetConfig()
	if !chain33Cfg.IsDappFork(action.height, ty.Pos33TicketX, "UseEntrust") {
		return nil, errors.New("config exec.ycc.UseEntrust error")
	}
	return &types.Receipt{KV: []*types.KeyValue{{Key: BlsKey(pm.BlsAddr), Value: []byte(action.fromaddr)}}}, nil
}

func (action *Action) Pos33Migrate(pm *ty.Pos33Migrate) (*types.Receipt, error) {
	chain33Cfg := action.api.GetConfig()
	if !chain33Cfg.IsDappFork(action.height, ty.Pos33TicketX, "UseEntrust") {
		return nil, errors.New("config exec.ycc.UseEntrust error")
	}

	d, err := getDeposit(action.db, pm.Miner)
	if err != nil {
		return nil, err
	}
	consignor := &ty.Consignor{Address: d.Raddr, Amount: d.Count, Reward: d.Reward}
	consignee, err := action.getConsignee(pm.Miner)
	if err != nil {
		consignee = &ty.Pos33Consignee{Address: pm.Miner, Amount: d.Count}
	}
	consignee.Consignors = append(consignee.Consignors, consignor)
	return &types.Receipt{KV: action.updateConsignee(pm.Miner, consignee)}, nil
}

func (action *Action) Pos33Entrust(pe *ty.Pos33Entrust) (*types.Receipt, error) {
	chain33Cfg := action.api.GetConfig()
	if !chain33Cfg.IsDappFork(action.height, ty.Pos33TicketX, "UseEntrust") {
		return nil, errors.New("config exec.ycc.UseEntrust error")
	}

	if action.fromaddr != pe.Consignor {
		return nil, types.ErrFromAddr
	}
	if pe.Amount == 0 {
		return nil, types.ErrAmount
	}

	consignee, err := action.getConsignee(pe.Consignee)
	if err != nil {
		return nil, err
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

	cfg := ty.GetPos33TicketMinerParam(chain33Cfg, action.height)
	realAmount := cfg.Pos33TicketPrice * pe.Amount

	var receipt *types.Receipt
	if pe.Amount > 0 {
		receipt, err = action.coinsAccount.TransferToExec(action.fromaddr, action.execaddr, realAmount)
		if err != nil {
			return nil, err
		}
		receipt1, err := action.coinsAccount.ExecFrozen(action.fromaddr, action.execaddr, realAmount)
		if err != nil {
			return nil, err
		}
		receipt.KV = append(receipt.KV, receipt1.KV...)
		receipt.Logs = append(receipt.Logs, receipt1.Logs...)
	} else {
		receipt, err = action.coinsAccount.ExecActive(action.fromaddr, action.execaddr, -realAmount)
		if err != nil {
			return nil, err
		}
		receipt1, err := action.coinsAccount.TransferWithdraw(action.fromaddr, action.execaddr, -realAmount)
		if err != nil {
			return nil, err
		}
		receipt.KV = append(receipt.KV, receipt1.KV...)
		receipt.Logs = append(receipt.Logs, receipt1.Logs...)
	}

	setNewCount(action.db, int(pe.Amount))
	consignee.Amount += pe.Amount
	consignor.Amount += pe.Amount
	kvs, err := action.updateConsignor(pe)
	if err != nil {
		return nil, err
	}
	kvs = append(kvs, action.updateConsignee(pe.Consignee, consignee)...)
	receipt.KV = append(receipt.KV, kvs...)
	return receipt, nil
}
