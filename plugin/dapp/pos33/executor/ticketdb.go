// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package executor

//database opeartion for execs ticket
import (

	//"bytes"

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

//var genesisKey = []byte("mavl-acc-genesis")
//var addrSeed = []byte("address seed bytes for public key")

// DB db
// type DB struct {
// 	ty.Pos33Ticket
// 	prevstatus int32
// }

// //GetRealPrice 获取真实的价格
// func (t *DB) GetRealPrice(cfg *types.Chain33Config) int64 {
// 	if t.GetPrice() == 0 {
// 		cfg := ty.GetPos33TicketMinerParam(cfg, cfg.GetFork("ForkChainParamV1"))
// 		return cfg.Pos33TicketPrice
// 	}
// 	return t.GetPrice()
// }

// // NewDB new instance
// func NewDB(cfg *types.Chain33Config, id, minerAddress, returnWallet string, blocktime, height, price int64, isGenesis bool) *DB {
// 	t := &DB{}
// 	t.TicketId = id
// 	t.MinerAddress = minerAddress
// 	t.ReturnAddress = returnWallet
// 	t.Status = ty.Pos33TicketOpened
// 	t.IsGenesis = isGenesis
// 	t.prevstatus = ty.Pos33TicketInit
// 	//height == 0 的情况下，不去改变 genesis block
// 	if cfg.IsFork(height, "ForkChainParamV2") && height > 0 {
// 		t.Price = price
// 	}
// 	return t
// }

//ticket 的状态变化：
//1. status == 1 (NewPos33Ticket的情况)
//3. status == 3 (Close的情况)

//add prevStatus:  便于回退状态，以及删除原来状态
//list 保存的方法:
//minerAddress:status:ticketId=ticketId

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

// // GetKVSet get kv set
// func (t *DB) GetKVSet() (kvset []*types.KeyValue) {
// 	value := types.Encode(&t.Pos33Ticket)
// 	kvset = append(kvset, &types.KeyValue{Key: Key(t.TicketId), Value: value})
// 	return kvset
// }

// // Save save
// func (t *DB) Save(db dbm.KV) {
// 	set := t.GetKVSet()
// 	for i := 0; i < len(set); i++ {
// 		db.Set(set[i].GetKey(), set[i].Value)
// 	}
// }

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
		tlog.Error("getDeposit error", "err", err)
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
func setDeposit(db dbm.KV, maddr, raddr string, newCount int64, newReward int64) *types.KeyValue {
	d, err := getDeposit(db, maddr)
	if err != nil {
		d = &ty.Pos33DepositMsg{Maddr: maddr, Raddr: raddr, Count: newCount, Reward: newReward}
	} else {
		if raddr != "" {
			d.Raddr = raddr
		}
		d.Count += newCount
		d.Reward += newReward
	}
	tlog.Debug("setDeposit", "maddr", maddr, "count", d.Count)
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

	tlog.Info("GenesisInit", "count", genesis.Count)
	receipt.KV = append(receipt.KV, setNewCount(action.db, int(genesis.Count)))
	receipt.KV = append(receipt.KV, setDeposit(action.db, genesis.MinerAddress, genesis.ReturnAddress, int64(genesis.Count), 0))
	receipt.Logs = append(receipt.Logs, pos33ReceiptLog(ty.TyLogNewPos33Ticket, int(genesis.Count), genesis.MinerAddress))
	return receipt, nil
}

// func saveBind(db dbm.KV, tbind *ty.Pos33TicketBind) {
// 	set := getBindKV(tbind)
// 	for i := 0; i < len(set); i++ {
// 		db.Set(set[i].GetKey(), set[i].Value)
// 	}
// }

// func getBindKV(tbind *ty.Pos33TicketBind) (kvset []*types.KeyValue) {
// 	value := types.Encode(tbind)
// 	kvset = append(kvset, &types.KeyValue{Key: BindKey(tbind.ReturnAddress), Value: value})
// 	return kvset
// }

// func getBindLog(tbind *ty.Pos33TicketBind, old string) *types.ReceiptLog {
// 	log := &types.ReceiptLog{}
// 	log.Ty = ty.TyLogPos33TicketBind
// 	r := &ty.ReceiptPos33TicketBind{}
// 	r.ReturnAddress = tbind.ReturnAddress
// 	r.OldMinerAddress = old
// 	r.NewMinerAddress = tbind.MinerAddress
// 	log.Log = types.Encode(r)
// 	return log
// }

// func (action *Action) getBind(addr string) string {
// 	value, err := action.db.Get(BindKey(addr))
// 	if err != nil || value == nil {
// 		return ""
// 	}
// 	var bind ty.Pos33TicketBind
// 	err = types.Decode(value, &bind)
// 	if err != nil {
// 		panic(err)
// 	}
// 	return bind.MinerAddress
// }

// //Pos33TicketBind 授权某个地址进行挖矿
// func (action *Action) Pos33TicketBind(tbind *ty.Pos33TicketBind) (*types.Receipt, error) {
// 	//todo: query address is a minered address
// 	if action.fromaddr != tbind.ReturnAddress {
// 		return nil, types.ErrFromAddr
// 	}
// 	//"" 表示设置为空
// 	if len(tbind.MinerAddress) > 0 {
// 		if err := address.CheckAddress(tbind.MinerAddress); err != nil {
// 			return nil, err
// 		}
// 	}
// 	var logs []*types.ReceiptLog
// 	var kvs []*types.KeyValue
// 	oldbind := action.getBind(tbind.ReturnAddress)
// 	log := getBindLog(tbind, oldbind)
// 	logs = append(logs, log)
// 	saveBind(action.db, tbind)
// 	kv := getBindKV(tbind)
// 	kvs = append(kvs, kv...)
// 	receipt := &types.Receipt{Ty: types.ExecOk, KV: kvs, Logs: logs}
// 	return receipt, nil
// }

// Pos33TicketOpen ticket open
func (action *Action) Pos33TicketOpen(topen *ty.Pos33TicketOpen) (*types.Receipt, error) {
	// if action.fromaddr != topen.ReturnAddress {
	// 	mineraddr := action.getBind(topen.ReturnAddress)
	// 	if mineraddr != action.fromaddr {
	// 		return nil, ty.ErrMinerNotPermit
	// 	}
	// 	if topen.MinerAddress != mineraddr {
	// 		return nil, ty.ErrMinerAddr
	// 	}
	// }

	chain33Cfg := action.api.GetConfig()
	cfg := ty.GetPos33TicketMinerParam(chain33Cfg, action.height)
	//冻结子账户资金
	receipt, err := action.coinsAccount.ExecFrozen(topen.ReturnAddress, action.execaddr, cfg.Pos33TicketPrice*int64(topen.Count))
	if err != nil {
		tlog.Error("Pos33TicketOpen.Frozen", "addr", topen.ReturnAddress, "execaddr", action.execaddr, "n", topen.Count)
		return nil, err
	}

	tlog.Info("@@@@@@@ pos33.ticket open", "ntid", topen.Count, "height", action.height)
	receipt.KV = append(receipt.KV, setNewCount(action.db, int(topen.Count)))
	receipt.KV = append(receipt.KV, setDeposit(action.db, topen.MinerAddress, topen.ReturnAddress, int64(topen.Count), 0))
	receipt.Logs = append(receipt.Logs, pos33ReceiptLog(ty.TyLogNewPos33Ticket, int(topen.Count), topen.MinerAddress))
	return receipt, nil
}

// func readPos33Ticket(db dbm.KV, id string) (*ty.Pos33Ticket, error) {
// 	data, err := db.Get(Key(id))
// 	if err != nil {
// 		return nil, err
// 	}
// 	var ticket ty.Pos33Ticket
// 	//decode
// 	err = types.Decode(data, &ticket)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return &ticket, nil
// }

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
		kvs = append(kvs, setDeposit(action.db, maddr, "", 0, ty.Pos33VoteReward))
	}

	// bp reward
	bpReward := ty.Pos33BpReward * int64(sumw)
	if bpReward > 0 {
		receipt, err := action.coinsAccount.ExecDeposit(action.fromaddr, action.execaddr, bpReward)
		if err != nil {
			tlog.Error("Pos33TicketMiner.ExecDeposit error", "error", err, "bp", action.fromaddr, "value", bpReward)
			return nil, err
		}

		tlog.Info("bp rerward", "height", action.height, "reward", bpReward)
		logs = append(logs, receipt.Logs...)
		kvs = append(kvs, receipt.KV...)
		kvs = append(kvs, setDeposit(action.db, action.fromaddr, "", 0, ty.Pos33VoteReward))
	}

	// fund reward
	fundReward := ty.Pos33BlockReward - (ty.Pos33VoteReward+ty.Pos33BpReward)*int64(sumw)
	tlog.Info("fund rerward", "height", action.height, "reward", fundReward)

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

	count := int(tclose.Count)
	receipt, err := action.coinsAccount.ExecActive(d.Raddr, action.execaddr, price*int64(count))
	if err != nil {
		tlog.Error("Pos33TicketClose.ExecActive user", "addr", d.Raddr, "execaddr", action.execaddr, "value", price)
		return nil, err
	}
	tlog.Info("@@@@@@@ pos33.ticket close", "count", count, "height", action.height)
	receipt.KV = append(receipt.KV, setNewCount(action.db, count))
	receipt.KV = append(receipt.KV, setDeposit(action.db, action.fromaddr, "", int64(-count), 0))
	return receipt, nil
}

/*
// List list db
func List(db dbm.Lister, db2 dbm.KV, tlist *ty.Pos33TicketList) (types.Message, error) {
	values, err := db.List(calcPos33TicketPrefix(tlist.Addr, tlist.Status), nil, 0, 0)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return &ty.ReplyPos33TicketList{}, nil
	}
	var ids ty.Pos33TicketInfos
	for i := 0; i < len(values); i++ {
		ids.TicketIds = append(ids.TicketIds, string(values[i]))
	}
	return &ids, nil
}

// Infos info
func Infos(db dbm.KV, tinfos *ty.Pos33TicketInfos) (types.Message, error) {
	var tickets []*ty.Pos33Ticket
	for i := 0; i < len(tinfos.TicketIds); i++ {
		id := tinfos.TicketIds[i]
		ticket, err := readPos33Ticket(db, id)
		//数据库可能会不一致，读的过程中可能会有写
		if err != nil {
			continue
		}
		tickets = append(tickets, ticket)
	}
	return &ty.ReplyPos33TicketList{Tickets: tickets}, nil
}
*/
