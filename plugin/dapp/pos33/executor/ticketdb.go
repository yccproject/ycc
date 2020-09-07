// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package executor

//database opeartion for execs ticket
import (

	//"bytes"

	"fmt"

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
type DB struct {
	ty.Pos33Ticket
	prevstatus int32
}

//GetRealPrice 获取真实的价格
func (t *DB) GetRealPrice(cfg *types.Chain33Config) int64 {
	if t.GetPrice() == 0 {
		cfg := ty.GetPos33TicketMinerParam(cfg, cfg.GetFork("ForkChainParamV1"))
		return cfg.Pos33TicketPrice
	}
	return t.GetPrice()
}

// NewDB new instance
func NewDB(cfg *types.Chain33Config, id, minerAddress, returnWallet string, blocktime, height, price int64, isGenesis bool) *DB {
	t := &DB{}
	t.TicketId = id
	t.MinerAddress = minerAddress
	t.ReturnAddress = returnWallet
	t.Status = ty.Pos33TicketOpened
	t.IsGenesis = isGenesis
	t.prevstatus = ty.Pos33TicketInit
	//height == 0 的情况下，不去改变 genesis block
	if cfg.IsFork(height, "ForkChainParamV2") && height > 0 {
		t.Price = price
	}
	return t
}

//ticket 的状态变化：
//1. status == 1 (NewPos33Ticket的情况)
//3. status == 3 (Close的情况)

//add prevStatus:  便于回退状态，以及删除原来状态
//list 保存的方法:
//minerAddress:status:ticketId=ticketId

// GetReceiptLog get receipt
func (t *DB) GetReceiptLog(typ int32) *types.ReceiptLog {
	log := &types.ReceiptLog{}
	log.Ty = typ
	r := &ty.ReceiptPos33Ticket{}
	r.TicketId = t.TicketId
	r.Status = t.Status
	r.PrevStatus = t.prevstatus
	r.Addr = t.MinerAddress
	log.Log = types.Encode(r)
	return log
}

// GetKVSet get kv set
func (t *DB) GetKVSet() (kvset []*types.KeyValue) {
	value := types.Encode(&t.Pos33Ticket)
	kvset = append(kvset, &types.KeyValue{Key: Key(t.TicketId), Value: value})
	return kvset
}

// Save save
func (t *DB) Save(db dbm.KV) {
	set := t.GetKVSet()
	for i := 0; i < len(set); i++ {
		db.Set(set[i].GetKey(), set[i].Value)
	}
}

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

// GenesisInit init genesis
func (action *Action) GenesisInit(genesis *ty.Pos33TicketGenesis) (*types.Receipt, error) {
	chain33Cfg := action.api.GetConfig()
	prefix := fmt.Sprintf("%s-%d-", genesis.MinerAddress[:8], action.height)
	var logs []*types.ReceiptLog
	var kv []*types.KeyValue
	cfg := ty.GetPos33TicketMinerParam(chain33Cfg, action.height)
	for i := 0; i < int(genesis.Count); i++ {
		id := prefix + fmt.Sprintf("%010d", i)
		db := NewDB(chain33Cfg, id, genesis.MinerAddress, genesis.ReturnAddress, action.blocktime, action.height, cfg.Pos33TicketPrice, true)
		//冻结子账户资金
		receipt, err := action.coinsAccount.ExecFrozen(genesis.ReturnAddress, action.execaddr, cfg.Pos33TicketPrice)
		if err != nil {
			tlog.Error("GenesisInit.Frozen", "addr", genesis.ReturnAddress, "execaddr", action.execaddr)
			panic(err)
		}
		db.Save(action.db)
		logs = append(logs, db.GetReceiptLog(ty.TyLogNewPos33Ticket))
		kv = append(kv, db.GetKVSet()...)
		logs = append(logs, receipt.Logs...)
		kv = append(kv, receipt.KV...)
	}
	tlog.Info("GenesisInit", "count", genesis.Count)
	receipt := &types.Receipt{Ty: types.ExecOk, KV: kv, Logs: logs}
	return receipt, nil
}

func saveBind(db dbm.KV, tbind *ty.Pos33TicketBind) {
	set := getBindKV(tbind)
	for i := 0; i < len(set); i++ {
		db.Set(set[i].GetKey(), set[i].Value)
	}
}

func getBindKV(tbind *ty.Pos33TicketBind) (kvset []*types.KeyValue) {
	value := types.Encode(tbind)
	kvset = append(kvset, &types.KeyValue{Key: BindKey(tbind.ReturnAddress), Value: value})
	return kvset
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
	var bind ty.Pos33TicketBind
	err = types.Decode(value, &bind)
	if err != nil {
		panic(err)
	}
	return bind.MinerAddress
}

//Pos33TicketBind 授权某个地址进行挖矿
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
	log := getBindLog(tbind, oldbind)
	logs = append(logs, log)
	saveBind(action.db, tbind)
	kv := getBindKV(tbind)
	kvs = append(kvs, kv...)
	receipt := &types.Receipt{Ty: types.ExecOk, KV: kvs, Logs: logs}
	return receipt, nil
}

// Pos33TicketOpen ticket open
func (action *Action) Pos33TicketOpen(topen *ty.Pos33TicketOpen) (*types.Receipt, error) {
	chain33Cfg := action.api.GetConfig()
	prefix := fmt.Sprintf("%s-%d-", topen.MinerAddress[:8], action.height)
	var logs []*types.ReceiptLog
	var kv []*types.KeyValue
	//addr from
	if action.fromaddr != topen.ReturnAddress {
		mineraddr := action.getBind(topen.ReturnAddress)
		if mineraddr != action.fromaddr {
			return nil, ty.ErrMinerNotPermit
		}
		if topen.MinerAddress != mineraddr {
			return nil, ty.ErrMinerAddr
		}
	}
	//action.fromaddr == topen.ReturnAddress or mineraddr == action.fromaddr
	cfg := ty.GetPos33TicketMinerParam(chain33Cfg, action.height)
	for i := 0; i < int(topen.Count); i++ {
		id := prefix + fmt.Sprintf("%010d", i)
		t := NewDB(chain33Cfg, id, topen.MinerAddress, topen.ReturnAddress, action.blocktime, action.height, cfg.Pos33TicketPrice, false)
		//冻结子账户资金
		receipt, err := action.coinsAccount.ExecFrozen(topen.ReturnAddress, action.execaddr, cfg.Pos33TicketPrice)
		if err != nil {
			tlog.Error("Pos33TicketOpen.Frozen", "addr", topen.ReturnAddress, "execaddr", action.execaddr, "n", topen.Count)
			return nil, err
		}
		t.Save(action.db)
		logs = append(logs, t.GetReceiptLog(ty.TyLogNewPos33Ticket))
		kv = append(kv, t.GetKVSet()...)
		logs = append(logs, receipt.Logs...)
		kv = append(kv, receipt.KV...)
	}
	tlog.Info("@@@@@@@ pos33.ticket open", "ntid", topen.Count, "height", action.height)
	receipt := &types.Receipt{Ty: types.ExecOk, KV: kv, Logs: logs}
	return receipt, nil
}

func readPos33Ticket(db dbm.KV, id string) (*ty.Pos33Ticket, error) {
	data, err := db.Get(Key(id))
	if err != nil {
		return nil, err
	}
	var ticket ty.Pos33Ticket
	//decode
	err = types.Decode(data, &ticket)
	if err != nil {
		return nil, err
	}
	return &ticket, nil
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

	// reward voters
	for _, v := range miner.Votes {
		r := v.Sort
		tid := r.SortHash.Tid
		t, err := readPos33Ticket(action.db, tid)
		if err != nil {
			return nil, err
		}

		// if closed, not reward
		if t.Status == ty.Pos33TicketClosed {
			continue
		}

		receipt, err := action.coinsAccount.ExecDepositFrozen(t.ReturnAddress, action.execaddr, ty.Pos33VoteReward)
		if err != nil {
			tlog.Error("Pos33TicketMiner.ExecDepositFrozen error", "voter", t.ReturnAddress, "execaddr", action.execaddr)
			return nil, err
		}

		t.MinerValue += ty.Pos33VoteReward
		prevStatus := t.Status
		db := &DB{*t, prevStatus}
		db.Save(action.db)
		logs = append(logs, db.GetReceiptLog(ty.TyLogMinerPos33Ticket))
		kvs = append(kvs, db.GetKVSet()...)
		logs = append(logs, receipt.Logs...)
		kvs = append(kvs, receipt.KV...)
	}

	// bp reward
	bpReward := ty.Pos33BpReward * int64(sumw)
	if bpReward > 0 {
		tid := miner.Sort.SortHash.Tid
		t, err := readPos33Ticket(action.db, tid)
		if err != nil {
			return nil, err
		}

		// reward if only opened
		if t.Status == ty.Pos33TicketOpened {
			receipt, err := action.coinsAccount.ExecDepositFrozen(t.ReturnAddress, action.execaddr, bpReward)
			if err != nil {
				tlog.Error("Pos33TicketMiner.ExecDepositFrozen error", "error", err, "bp", t.ReturnAddress, "value", bpReward)
				return nil, err
			}

			tlog.Info("bp rerward", "height", action.height, "tid", t.TicketId, "reward", bpReward)
			t.MinerValue += bpReward
			prevStatus := t.Status
			db := &DB{*t, prevStatus}
			db.Save(action.db)
			logs = append(logs, db.GetReceiptLog(ty.TyLogMinerPos33Ticket))
			kvs = append(kvs, db.GetKVSet()...)
			logs = append(logs, receipt.Logs...)
			kvs = append(kvs, receipt.KV...)
		}
	}

	// fund reward
	fundReward := ty.Pos33BlockReward - (ty.Pos33VoteReward+ty.Pos33BpReward)*int64(sumw)
	tlog.Info("fund rerward", "height", action.height, "reward", fundReward)
	if fundReward > 0 {
		var receipt *types.Receipt
		var err error
		// issue coins to exec addr
		addr := chain33Cfg.MGStr("mver.consensus.fundKeyAddr", action.height)
		receipt, err = action.coinsAccount.ExecIssueCoins(addr, fundReward)
		if err != nil {
			tlog.Error("Pos33TicketMiner.ExecIssueCoins fund to autonomy fund", "addr", addr, "error", err)
			return nil, err
		}
		logs = append(logs, receipt.Logs...)
		kvs = append(kvs, receipt.KV...)
	}

	return &types.Receipt{Ty: types.ExecOk, KV: kvs, Logs: logs}, nil
}

// Pos33TicketClose close tick
func (action *Action) Pos33TicketClose(tclose *ty.Pos33TicketClose) (*types.Receipt, error) {
	chain33Cfg := action.api.GetConfig()
	var dbs []*DB
	for i := 0; i < len(tclose.TicketId); i++ {
		ticket, err := readPos33Ticket(action.db, tclose.TicketId[i])
		if err != nil {
			return nil, err
		}
		if ticket.Status != ty.Pos33TicketOpened {
			tlog.Debug("ticket is NOT opened", "id", ticket.GetTicketId(), "status", ticket.GetStatus())
			//return nil, ty.ErrPos33TicketClosed
			continue
		}
		//check from address
		if action.fromaddr != ticket.MinerAddress && action.fromaddr != ticket.ReturnAddress {
			return nil, types.ErrFromAddr
		}
		prevstatus := ticket.Status
		ticket.Status = ty.Pos33TicketClosed
		ticket.CloseHeight = action.height
		db := &DB{*ticket, prevstatus}
		dbs = append(dbs, db)
	}
	var logs []*types.ReceiptLog
	var kv []*types.KeyValue
	for i := 0; i < len(dbs); i++ {
		db := dbs[i]
		retValue := db.GetRealPrice(chain33Cfg) + db.MinerValue
		receipt1, err := action.coinsAccount.ExecActive(db.ReturnAddress, action.execaddr, retValue)
		if err != nil {
			tlog.Error("Pos33TicketClose.ExecActive user", "addr", db.ReturnAddress, "execaddr", action.execaddr, "value", retValue)
			return nil, err
		}
		tlog.Info("close pos33.ticket", "tid", db.TicketId, "height", action.height, "activeValue", retValue)
		logs = append(logs, db.GetReceiptLog(ty.TyLogClosePos33Ticket))
		kv = append(kv, db.GetKVSet()...)
		logs = append(logs, receipt1.Logs...)
		kv = append(kv, receipt1.KV...)
		db.Save(action.db)
	}
	tlog.Info("@@@@@@@ pos33.ticket close", "ntid", len(dbs), "height", action.height)
	receipt := &types.Receipt{Ty: types.ExecOk, KV: kv, Logs: logs}
	return receipt, nil
}

// List list db
func List(db dbm.Lister, db2 dbm.KV, tlist *ty.Pos33TicketList) (types.Message, error) {
	var err error
	var values [][]byte
	if tlist.Status > 0 {
		values, err = db.List(calcPos33TicketPrefix(tlist.Addr, tlist.Status), nil, 0, 0)
	} else {
		values, err = db.List(calcPos33TicketPrefix2(tlist.Addr), nil, 0, 0)
	}
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
	tlog.Info("GetTicketList", "len", len(values))
	return Infos(db2, &ids, tlist.Height)
}

// Infos info
func Infos(db dbm.KV, tinfos *ty.Pos33TicketInfos, height int64) (types.Message, error) {
	var tickets []*ty.Pos33Ticket
	for i := 0; i < len(tinfos.TicketIds); i++ {
		id := tinfos.TicketIds[i]
		ticket, err := readPos33Ticket(db, id)
		//数据库可能会不一致，读的过程中可能会有写
		if err != nil {
			continue
		}
		if height > 0 {
			if !ty.CheckTicketHeight(ticket, height) {
				continue
			}
		}
		tickets = append(tickets, ticket)
	}
	return &ty.ReplyPos33TicketList{Tickets: tickets}, nil
}
