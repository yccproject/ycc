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

// GetReceiptLog get receipt
func pos33ReceiptLog(typ int32, count int, addr string) *types.ReceiptLog {
	log := &types.ReceiptLog{}
	log.Ty = typ
	r := &ty.ReceiptPos33Ticket{}
	r.Addr = addr
	r.Count = int64(count)
	log.Log = types.Encode(r)
	return log
}

const allCountID = "allcountid"

//Key address to save key
func Key(id string) (key []byte) {
	key = append(key, []byte("mavl-pos33-")...)
	key = append(key, []byte(id)...)
	return key
}

// BindKey bind key
func BindKey(id string) (key []byte) {
	key = append(key, []byte("mavl-pos33-tbind-")...)
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
func setDeposit(db dbm.KV, maddr, raddr string, newCount, newReward, height int64) *types.KeyValue {
	d, err := getDeposit(db, maddr)
	if err != nil {
		d = &ty.Pos33DepositMsg{Maddr: maddr, Raddr: raddr, Count: newCount, Reward: newReward}
	} else {
		if newCount == 0 && newReward == 0 {
			return nil
		}
		if raddr != "" {
			d.Raddr = raddr
		}
		if newCount < 0 {
			d.PreCount = d.Count
			d.CloseHeight = height
		}
		d.Count += newCount
		d.Reward += newReward
	}
	tlog.Debug("setDeposit", "maddr", maddr, "count", d.Count, "precount", d.PreCount, "closeheight", d.CloseHeight, "reward", d.Reward)
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

// GenesisInit init genesis
func (action *Action) GenesisInit(genesis *ty.Pos33TicketGenesis) (*types.Receipt, error) {
	chain33Cfg := action.api.GetConfig()
	cfg := ty.GetPos33TicketMinerParam(chain33Cfg, action.height)

	//冻结子账户资金
	receipt, err := action.coinsAccount.ExecFrozen(genesis.ReturnAddress, action.execaddr, cfg.Pos33TicketPrice*int64(genesis.Count))
	if err != nil {
		tlog.Error("GenesisInit.Frozen", "addr", genesis.ReturnAddress, "execaddr", action.execaddr)
		panic(err)
	}

	receipt1, err := action.coinsAccount.ExecIssueCoins(action.execaddr, ty.Pos33BlockReward)
	if err != nil {
		tlog.Error("Pos33TicketMiner.ExecIssueCoins fund to autonomy fund", "addr", action.execaddr, "error", err)
		panic(err)
	}

	receipt.KV = append(receipt.KV, receipt1.KV...)
	receipt.Logs = append(receipt.Logs, receipt1.Logs...)

	tlog.Info("genesis init", "count", genesis.Count)
	receipt.KV = append(receipt.KV, setNewCount(action.db, int(genesis.Count)))
	receipt.KV = append(receipt.KV, setDeposit(action.db, genesis.MinerAddress, genesis.ReturnAddress, int64(genesis.Count), 0, 0))
	receipt.Logs = append(receipt.Logs, pos33ReceiptLog(ty.TyLogNewPos33Ticket, int(genesis.Count), genesis.MinerAddress))
	return receipt, nil
}

// Pos33TicketOpen ticket open
func (action *Action) Pos33TicketOpen(topen *ty.Pos33TicketOpen) (*types.Receipt, error) {
	chain33Cfg := action.api.GetConfig()
	cfg := ty.GetPos33TicketMinerParam(chain33Cfg, action.height)
	//冻结子账户资金
	receipt, err := action.coinsAccount.ExecFrozen(topen.ReturnAddress, action.execaddr, cfg.Pos33TicketPrice*int64(topen.Count))
	if err != nil {
		tlog.Error("Pos33TicketOpen.Frozen", "addr", topen.ReturnAddress, "execaddr", action.execaddr, "n", topen.Count)
		return nil, err
	}

	tlog.Info("new deposit", "count", topen.Count, "height", action.height)
	receipt.KV = append(receipt.KV, setNewCount(action.db, int(topen.Count)))
	receipt.KV = append(receipt.KV, setDeposit(action.db, topen.MinerAddress, topen.ReturnAddress, int64(topen.Count), 0, action.height))
	receipt.Logs = append(receipt.Logs, pos33ReceiptLog(ty.TyLogNewPos33Ticket, int(topen.Count), topen.MinerAddress))
	return receipt, nil
}

func saddr(sig *types.Signature) string {
	if sig == nil {
		return ""
	}
	return address.PubKeyToAddress(sig.Pubkey).String()
}

// Pos33TicketMiner ticket miner
func (action *Action) Pos33TicketMiner(miner *ty.Pos33TicketMiner, index int) (*types.Receipt, error) {
	if index != 0 {
		return nil, types.ErrCoinBaseIndex
	}
	chain33Cfg := action.api.GetConfig()
	sumw := len(miner.GetVotes())

	var kvs []*types.KeyValue
	var logs []*types.ReceiptLog

	// first issue to fund all reward
	{
		var receipt *types.Receipt
		var err error
		// issue coins to exec addr
		addr := chain33Cfg.MGStr("mver.consensus.fundKeyAddr", action.height)
		receipt, err = action.coinsAccount.ExecIssueCoins(action.execaddr, ty.Pos33BlockReward)
		if err != nil {
			tlog.Error("Pos33TicketMiner.ExecIssueCoins fund to autonomy fund", "addr", addr, "error", err)
			return nil, err
		}
		logs = append(logs, receipt.Logs...)
		kvs = append(kvs, receipt.KV...)
	}

	// reward voters
	for _, v := range miner.Votes {
		maddr := saddr(v.Sig)
		receipt, err := action.coinsAccount.ExecDeposit(maddr, action.execaddr, ty.Pos33VoteReward)
		if err != nil {
			tlog.Error("Pos33TicketMiner.ExecDeposit error", "voter", maddr, "execaddr", action.execaddr)
			return nil, err
		}

		logs = append(logs, receipt.Logs...)
		kvs = append(kvs, receipt.KV...)
		kvs = append(kvs, setDeposit(action.db, maddr, "", 0, ty.Pos33VoteReward, action.height))
	}

	// bp reward
	bpReward := ty.Pos33BpReward * int64(sumw)
	if bpReward > 0 {
		receipt, err := action.coinsAccount.ExecDeposit(action.fromaddr, action.execaddr, bpReward)
		if err != nil {
			tlog.Error("Pos33TicketMiner.ExecDeposit error", "error", err, "bp", action.fromaddr, "value", bpReward)
			return nil, err
		}

		logs = append(logs, receipt.Logs...)
		kvs = append(kvs, receipt.KV...)
		kvs = append(kvs, setDeposit(action.db, action.fromaddr, "", 0, ty.Pos33VoteReward, action.height))
		tlog.Info("block reward", "height", action.height, "reward", bpReward, "from", action.fromaddr[:16], "nv", sumw)
	}

	// fund reward
	fundReward := ty.Pos33BlockReward - (ty.Pos33VoteReward+ty.Pos33BpReward)*int64(sumw)
	tlog.Debug("fund rerward", "height", action.height, "reward", fundReward)

	return &types.Receipt{Ty: types.ExecOk, KV: kvs, Logs: logs}, nil
}

// Pos33TicketClose close tick
func (action *Action) Pos33TicketClose(tclose *ty.Pos33TicketClose) (*types.Receipt, error) {
	chain33Cfg := action.api.GetConfig()
	cfg := ty.GetPos33TicketMinerParam(chain33Cfg, action.height)
	price := cfg.Pos33TicketPrice

	d, err := getDeposit(action.db, action.fromaddr)
	if err != nil {
		return nil, err
	}
	if action.height-d.CloseHeight <= ty.Pos33SortitionSize {
		return nil, errors.New("close deposit too ofen")
	}

	count := int(tclose.Count)
	receipt, err := action.coinsAccount.ExecActive(d.Raddr, action.execaddr, price*int64(count))
	if err != nil {
		tlog.Error("close deposit error", "addr", d.Raddr, "execaddr", action.execaddr, "value", price*int64(count))
		return nil, err
	}
	tlog.Info("close deposit", "count", count, "height", action.height)
	receipt.KV = append(receipt.KV, setNewCount(action.db, -count))
	receipt.KV = append(receipt.KV, setDeposit(action.db, action.fromaddr, "", int64(-count), 0, action.height))
	return receipt, nil
}
