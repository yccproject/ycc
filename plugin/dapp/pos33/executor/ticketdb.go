// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package executor

//database opeartion for execs ticket
import (

	//"bytes"

	"errors"
	"fmt"
	"strconv"

	"github.com/33cn/chain33/account"
	"github.com/33cn/chain33/client"
	"github.com/33cn/chain33/common/address"
	dbm "github.com/33cn/chain33/common/db"
	log "github.com/33cn/chain33/common/log/log15"
	"github.com/33cn/chain33/system/dapp"
	"github.com/33cn/chain33/types"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

var tlog = log.New("module", "pos33db")

// // GetReceiptLog get receipt
// func pos33ReceiptLog(typ int32, count int, addr string) *types.ReceiptLog {
// 	log := &types.ReceiptLog{}
// 	log.Ty = typ
// 	r := &ty.ReceiptPos33Ticket{}
// 	r.Addr = addr
// 	r.Count = int64(count)
// 	log.Log = types.Encode(r)
// 	return log
// }

const allCountID = "allcountid"

//Key address to save key
func Key(id string) (key []byte) {
	key = append(key, []byte("mavl-pos33-")...)
	key = append(key, []byte(id)...)
	return key
}

// BindKey bind key
func BlsKey(id string) (key []byte) {
	key = append(key, []byte("mavl-pos33-bls-")...)
	key = append(key, []byte(id)...)
	return key
}

// BindKey bind key
func BindKey(id string) (key []byte) {
	key = append(key, []byte("mavl-pos33-bind-")...)
	key = append(key, []byte(id)...)
	return key
}

// Action action type
type Action struct {
	coinsAccount *account.DB
	db           dbm.KV
	txhash       []byte
	fromaddr     string
	blocktime    int64
	height       int64
	execaddr     string
	api          client.QueueProtocolAPI
}

// NewAction new action type
func NewAction(t *Pos33Ticket, tx *types.Transaction) *Action {
	hash := tx.Hash()
	fromaddr := tx.From()
	return &Action{t.GetCoinsAccount(), t.GetStateDB(), hash, fromaddr,
		t.GetBlockTime(), t.GetHeight(), dapp.ExecAddress(string(tx.Execer)), t.GetAPI()}
}

func getDeposit(db dbm.KV, addr string) (*ty.Pos33DepositMsg, error) {
	key := Key(addr)
	value, err := db.Get(key)
	if err != nil {
		if err != types.ErrNotFound {
			tlog.Error("getDeposit error", "err", err)
		}
		return nil, err
	}
	var dep ty.Pos33DepositMsg
	err = types.Decode(value, &dep)
	if err != nil {
		tlog.Error("getDeposit error", "err", err)
		return nil, err
	}
	return &dep, nil
}
func setDeposit(db dbm.KV, maddr, raddr string, newCount, newReward, height int64, changeRaddr bool) *types.KeyValue {
	d, err := getDeposit(db, maddr)
	if err != nil {
		d = &ty.Pos33DepositMsg{Maddr: maddr, Raddr: raddr, Count: newCount, Reward: newReward}
	} else {
		// if newCount == 0 && newReward == 0 {
		// 	return nil
		// }
		if changeRaddr {
			d.Raddr = raddr
		}
		if newCount < 0 {
			d.PreCount = d.Count
			d.CloseHeight = height
		}
		d.Count += newCount
		if d.Count < 0 {
			d.Count = 0
		}
		d.Reward += newReward
	}
	return &types.KeyValue{Key: Key(maddr), Value: types.Encode(d)}
}

func getCount(db dbm.KV, addr string) int {
	dep, err := getDeposit(db, addr)
	if err != nil {
		return 0
	}
	return int(dep.Count)
}

func getAllCount(db dbm.KV) int {
	key := Key(allCountID)
	value, err := db.Get(key)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(string(value))
	if err != nil {
		return 0
	}
	return n
}

func setNewCount(db dbm.KV, n int) *types.KeyValue {
	value := []byte(fmt.Sprintf("%d", getAllCount(db)+n))
	return &types.KeyValue{Key: Key(allCountID), Value: value}
}

func depositReceipt(t int, addr string, newCount int64) *types.ReceiptLog {
	r := &ty.ReceiptPos33Deposit{Addr: addr, Count: newCount}
	return &types.ReceiptLog{Log: types.Encode(r), Ty: int32(t)}
}

func minerReceipt(t int, addr string, newReward int64) *types.ReceiptLog {
	r := &ty.ReceiptPos33Miner{Addr: addr, Reward: newReward}
	return &types.ReceiptLog{Log: types.Encode(r), Ty: int32(t)}
}

// GenesisInit init genesis
func (action *Action) GenesisInit(genesis *ty.Pos33TicketGenesis) (*types.Receipt, error) {
	chain33Cfg := action.api.GetConfig()
	cfg := ty.GetPos33TicketMinerParam(chain33Cfg, action.height)

	Coin := chain33Cfg.GetCoinPrecision()
	// Pos33BlockReward 区块奖励
	var Pos33BlockReward = Coin * 30

	//冻结子账户资金
	receipt, err := action.coinsAccount.ExecFrozen(genesis.ReturnAddress, action.execaddr, cfg.Pos33TicketPrice*int64(genesis.Count))
	if err != nil {
		tlog.Error("GenesisInit.Frozen", "addr", genesis.MinerAddress, "execaddr", action.execaddr)
		panic(err)
	}

	receipt1, err := action.coinsAccount.ExecIssueCoins(action.execaddr, Pos33BlockReward)
	if err != nil {
		tlog.Error("Pos33TicketMiner.ExecIssueCoins fund to autonomy fund", "addr", action.execaddr, "error", err)
		panic(err)
	}

	receipt.KV = append(receipt.KV, receipt1.KV...)
	receipt.Logs = append(receipt.Logs, receipt1.Logs...)

	tlog.Info("genesis init", "count", genesis.Count, "blsAddr", genesis.ReturnAddress, "addr", genesis.MinerAddress)
	receipt.KV = append(receipt.KV, setNewCount(action.db, int(genesis.Count)))
	receipt.KV = append(receipt.KV, &types.KeyValue{Key: BlsKey(genesis.BlsAddress), Value: []byte(genesis.MinerAddress)})
	receipt.KV = append(receipt.KV, setDeposit(action.db, genesis.MinerAddress, genesis.ReturnAddress, int64(genesis.Count), 0, 0, true))
	receipt.Logs = append(receipt.Logs, depositReceipt(ty.TyLogNewPos33Ticket, genesis.MinerAddress, int64(genesis.Count)))
	return receipt, nil
}

// Pos33TicketOpen ticket open
func (action *Action) Pos33TicketOpen(topen *ty.Pos33TicketOpen) (*types.Receipt, error) {
	if action.fromaddr != topen.MinerAddress {
		return nil, errors.New("address NOT match, from address must == miner address")
	}

	d, _ := getDeposit(action.db, topen.MinerAddress)
	if d != nil && d.Raddr != topen.ReturnAddress {
		return nil, errors.New("open return address NOT match")
	}

	chain33Cfg := action.api.GetConfig()
	cfg := ty.GetPos33TicketMinerParam(chain33Cfg, action.height)

	//冻结子账户资金
	receipt, err := action.coinsAccount.ExecFrozen(topen.ReturnAddress, action.execaddr, cfg.Pos33TicketPrice*int64(topen.Count))
	if err != nil {
		tlog.Error("Pos33TicketOpen.Frozen", "addr", topen.ReturnAddress, "execaddr", action.execaddr, "n", topen.Count)
		return nil, err
	}

	tlog.Info("new deposit", "count", topen.Count, "height", action.height)
	receipt.KV = append(receipt.KV, &types.KeyValue{Key: BlsKey(topen.BlsAddress), Value: []byte(topen.MinerAddress)})
	receipt.KV = append(receipt.KV, setNewCount(action.db, int(topen.Count)))
	receipt.KV = append(receipt.KV, setDeposit(action.db, topen.MinerAddress, topen.ReturnAddress, int64(topen.Count), 0, action.height, false))
	receipt.Logs = append(receipt.Logs, depositReceipt(ty.TyLogNewPos33Ticket, topen.MinerAddress, int64(topen.Count)))
	return receipt, nil
}

func saddr(sig *types.Signature) string {
	if sig == nil {
		return ""
	}
	return address.PubKeyToAddress(sig.Pubkey).String()
}

func (action *Action) Pos33Miner(miner *ty.Pos33MinerMsg, index int) (*types.Receipt, error) {
	if index != 0 {
		return nil, types.ErrCoinBaseIndex
	}
	chain33Cfg := action.api.GetConfig()

	Coin := chain33Cfg.GetCoinPrecision()
	// Pos33BlockReward 区块奖励
	var Pos33BlockReward = Coin * 30
	// Pos33VoteReward 每ticket区块voter奖励
	var Pos33VoteReward = Coin / 2 // 0.5 ycc
	// Pos33MakerReward 每ticket区块bp奖励
	var Pos33MakerReward = Coin * 22 / 100 // 0.22 ycc

	var kvs []*types.KeyValue
	var logs []*types.ReceiptLog

	// first issue to fund all reward
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

	// reward voters
	for _, pk := range miner.BlsPkList {
		val, err := action.db.Get(BlsKey(address.PubKeyToAddr(pk)))
		if err != nil {
			return nil, err
		}
		vaddr := string(val)
		d, err := getDeposit(action.db, vaddr)
		if err != nil {
			return nil, err
		}
		raddr := vaddr
		if d.Raddr != "" {
			raddr = d.Raddr
		}
		receipt, err := action.coinsAccount.ExecDeposit(raddr, action.execaddr, Pos33VoteReward)
		if err != nil {
			tlog.Error("Pos33TicketMiner.ExecDeposit error", "voter", vaddr, "execaddr", action.execaddr)
			return nil, err
		}

		logs = append(logs, receipt.Logs...)
		kvs = append(kvs, receipt.KV...)
		kvs = append(kvs, setDeposit(action.db, vaddr, "", 0, Pos33VoteReward, action.height, false))
	}

	// bp reward
	bpReward := Pos33MakerReward * int64(len(miner.BlsPkList))
	if bpReward > 0 {
		d, err := getDeposit(action.db, action.fromaddr)
		if err != nil {
			return nil, err
		}
		raddr := action.fromaddr
		if d.Raddr != "" {
			raddr = d.Raddr
		}
		receipt, err := action.coinsAccount.ExecDeposit(raddr, action.execaddr, bpReward)
		if err != nil {
			tlog.Error("Pos33TicketMiner.ExecDeposit error", "error", err, "bp", action.fromaddr, "value", bpReward)
			return nil, err
		}

		logs = append(logs, receipt.Logs...)
		kvs = append(kvs, receipt.KV...)
		kvs = append(kvs, setDeposit(action.db, action.fromaddr, "", 0, Pos33VoteReward, action.height, false))
		tlog.Info("block reward", "height", action.height, "reward", bpReward, "from", action.fromaddr[:16], "nv", len(miner.BlsPkList))
	}

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

// Pos33TicketClose close tick
func (action *Action) Pos33TicketClose(tclose *ty.Pos33TicketClose) (*types.Receipt, error) {
	chain33Cfg := action.api.GetConfig()
	cfg := ty.GetPos33TicketMinerParam(chain33Cfg, action.height)
	price := cfg.Pos33TicketPrice

	d, err := getDeposit(action.db, action.fromaddr)
	if err != nil {
		tlog.Error("close ticket error: getDeposit error", "err", err, "fromaddr", action.fromaddr)
		return nil, err
	}
	if tclose.MinerAddress != action.fromaddr && tclose.MinerAddress != d.Raddr {
		return nil, types.ErrFromAddr
	}

	if d.Count == 0 {
		return nil, errors.New("your ticket count is 0")
	}
	if action.height < 60000 && action.height-d.CloseHeight <= ty.Pos33SortBlocks {
		return nil, errors.New("close deposit too often")
	}

	count := int(tclose.Count)
	if count <= 0 || count > int(d.Count) {
		count = int(d.Count)
	}
	receipt, err := action.coinsAccount.ExecActive(tclose.MinerAddress, action.execaddr, price*int64(count))
	if err != nil {
		tlog.Error("close deposit error", "addr", d.Raddr, "execaddr", action.execaddr, "value", price*int64(count))
		return nil, err
	}
	tlog.Info("close deposit", "count", count, "height", action.height, "addr", action.fromaddr)
	receipt.KV = append(receipt.KV, setNewCount(action.db, -count))
	receipt.KV = append(receipt.KV, setDeposit(action.db, action.fromaddr, "", int64(-count), 0, action.height, false))
	receipt.Logs = append(receipt.Logs, depositReceipt(ty.TyLogClosePos33Ticket, action.fromaddr, int64(tclose.Count)))
	return receipt, nil
}

func getBindLog(tbind *ty.Pos33TicketBind, old string) *types.ReceiptLog {
	log := &types.ReceiptLog{}
	log.Ty = ty.TyLogPos33TicketBind
	r := &ty.ReceiptPos33TicketBind{}
	r.ReturnAddress = tbind.ReturnAddress
	r.OldMinerAddress = old
	r.NewMinerAddress = tbind.MinerAddress
	log.Log = types.Encode(r)
	return log
}

func (action *Action) getBind(addr string) string {
	value, err := action.db.Get(BindKey(addr))
	if err != nil || value == nil {
		return ""
	}
	return string(value)
}

//TicketBind 授权某个地址进行挖矿
func (action *Action) Pos33TicketBind(tbind *ty.Pos33TicketBind) (*types.Receipt, error) {
	//todo: query address is a minered address
	if action.fromaddr != tbind.ReturnAddress {
		return nil, types.ErrFromAddr
	}
	//"" 表示设置为空
	if len(tbind.MinerAddress) > 0 {
		if err := address.CheckAddress(tbind.MinerAddress); err != nil {
			return nil, err
		}
	}
	var logs []*types.ReceiptLog
	var kvs []*types.KeyValue
	oldbind := action.getBind(tbind.ReturnAddress)
	if oldbind != "nil" {
		tlog.Info("pos33 bind", "maddr", tbind.MinerAddress, "raddr", tbind.ReturnAddress, "oldbind", oldbind)
		if oldbind == tbind.MinerAddress {
			return nil, nil
		}
		d, err := getDeposit(action.db, oldbind)
		if err != nil {
			tlog.Error("bind getDeposit error", "err", err, "oldbind", oldbind)
		} else if d.Count != 0 {
			return nil, errors.New("bind new MUST close all oldbind tickts")
		}
		kvs = append(kvs, setDeposit(action.db, oldbind, oldbind, 0, 0, action.height, true))
	}

	log := getBindLog(tbind, oldbind)
	logs = append(logs, log)

	maddr := tbind.MinerAddress
	if maddr == "" {
		maddr = "nil"
	}
	kvs = append(kvs, &types.KeyValue{Key: BindKey(tbind.ReturnAddress), Value: []byte(maddr)})
	tlog.Info("pos33 bind", "maddr", tbind.MinerAddress, "raddr", tbind.ReturnAddress)

	if tbind.MinerAddress != "" {
		d, err := getDeposit(action.db, tbind.MinerAddress)
		if err != nil {
			tlog.Error("bind getDeposit error", "err", err, "maddr", tbind.MinerAddress)
		} else if d.Count != 0 {
			return nil, errors.New("bind new MUST close all tickts")
		}
		kvs = append(kvs, setDeposit(action.db, tbind.MinerAddress, tbind.ReturnAddress, 0, 0, action.height, true))
	}

	receipt := &types.Receipt{Ty: types.ExecOk, KV: kvs, Logs: logs}
	return receipt, nil
}
