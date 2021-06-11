// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package types

import (
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/33cn/chain33/common/crypto"
	log "github.com/33cn/chain33/common/log/log15"
	"github.com/33cn/chain33/types"
)

var tlog = log.New("module", "pos33.types")

const (
	//Pos33TicketInit ticket　init status
	Pos33TicketInit = iota
	//Pos33TicketOpened ticket opened status
	Pos33TicketOpened
	//Pos33TicketMined ticket mined status
	//Pos33TicketMined
	//Pos33TicketClosed ticket closed status
	Pos33TicketClosed
)

const (
	//log for ticket

	//TyLogNewPos33Ticket new ticket log type
	TyLogNewPos33Ticket = 331
	// TyLogClosePos33Ticket close ticket log type
	TyLogClosePos33Ticket = 332
	// TyLogMinerPos33Ticket miner ticket log type
	TyLogMinerPos33Ticket = 333
	// TyLogVoterPos33Ticket miner ticket log type
	TyLogVoterPos33Ticket = 335
	// TyLogPos33TicketBind bind ticket log type
	// TyLogPos33TicketBind = 334
)

//ticket
const (
	// Pos33TicketActionGenesis action type
	Pos33TicketActionGenesis = 11
	// Pos33TicketActionOpen action type
	Pos33TicketActionOpen = 12
	// Pos33TicketActionClose action type
	Pos33TicketActionClose = 13
	// Pos33TicketActionList  action type
	Pos33TicketActionList = 14 //读的接口不直接经过transaction
	// Pos33TicketActionInfos action type
	Pos33TicketActionInfos = 15 //读的接口不直接经过transaction
	// Pos33TicketActionMiner action miner
	Pos33TicketActionMiner = 16
	// Pos33TicketActionBind action bind
	// Pos33TicketActionBind = 17
)

// Pos33TicketOldParts old tick type
const Pos33TicketOldParts = 3

// Pos33TicketCountOpenOnce count open once
const Pos33TicketCountOpenOnce = 1000

// ErrOpenPos33TicketPubHash err type
var ErrOpenPos33TicketPubHash = errors.New("ErrOpenPos33TicketPubHash")

// Pos33TicketX dapp name
var Pos33TicketX = "pos33"

func init() {
	types.AllowUserExec = append(types.AllowUserExec, []byte(Pos33TicketX))
	types.RegFork(Pos33TicketX, InitFork)
	types.RegExec(Pos33TicketX, InitExecutor)
}

func InitFork(cfg *types.Chain33Config) {
	cfg.RegisterDappFork(Pos33TicketX, "Enable", 0)
	//cfg.RegisterDappFork(Pos33TicketX, "ForkTicketId", 1062000)
	//cfg.RegisterDappFork(Pos33TicketX, "ForkPos33TicketVrf", 1770000)
}

func InitExecutor(cfg *types.Chain33Config) {
	types.RegistorExecutor(Pos33TicketX, NewType(cfg))
}

// Pos33TicketType ticket exec type
type Pos33TicketType struct {
	types.ExecTypeBase
}

// NewType new type
func NewType(cfg *types.Chain33Config) *Pos33TicketType {
	c := &Pos33TicketType{}
	c.SetChild(c)
	c.SetConfig(cfg)
	return c
}

// GetPayload get payload
func (ticket *Pos33TicketType) GetPayload() types.Message {
	return &Pos33TicketAction{}
}

// GetLogMap get log map
func (ticket *Pos33TicketType) GetLogMap() map[int64]*types.LogInfo {
	return map[int64]*types.LogInfo{
		TyLogNewPos33Ticket:   {Ty: reflect.TypeOf(ReceiptPos33Deposit{}), Name: "LogNewPos33Ticket"},
		TyLogClosePos33Ticket: {Ty: reflect.TypeOf(ReceiptPos33Deposit{}), Name: "LogClosePos33Ticket"},
		TyLogMinerPos33Ticket: {Ty: reflect.TypeOf(ReceiptPos33Miner{}), Name: "LogMinerPos33Ticket"},
		TyLogVoterPos33Ticket: {Ty: reflect.TypeOf(ReceiptPos33Miner{}), Name: "LogVoterPos33Ticket"},
		// TyLogPos33TicketBind:  {Ty: reflect.TypeOf(ReceiptPos33TicketBind{}), Name: "LogPos33TicketBind"},
	}
}

// Amount get amount
func (ticket Pos33TicketType) Amount(tx *types.Transaction) (int64, error) {
	var action Pos33TicketAction
	err := types.Decode(tx.GetPayload(), &action)
	if err != nil {
		return 0, types.ErrDecode
	}
	if action.Ty == Pos33TicketActionMiner && action.GetMiner() != nil {
		ticketMiner := action.GetMiner()
		if ticketMiner == nil {
			return 0, nil
		}
		nvs := len(ticketMiner.Votes)
		bpr := Pos33MakerReward * int64(nvs)
		return bpr, nil
	}
	return 0, nil
}

// GetName get name
func (ticket *Pos33TicketType) GetName() string {
	return Pos33TicketX
}

// GetTypeMap get type map
func (ticket *Pos33TicketType) GetTypeMap() map[string]int32 {
	return map[string]int32{
		"Genesis": Pos33TicketActionGenesis,
		"Topen":   Pos33TicketActionOpen,
		// "Tbind":   Pos33TicketActionBind,
		"Tclose": Pos33TicketActionClose,
		"Miner":  Pos33TicketActionMiner,
	}
}

// Pos33TicketMinerParam is ...
type Pos33TicketMinerParam struct {
	CoinDevFund              int64
	CoinReward               int64
	FutureBlockTime          int64
	Pos33TicketPrice         int64
	Pos33TicketFrozenTime    int64
	Pos33TicketWithdrawTime  int64
	Pos33TicketMinerWaitTime int64
	TargetTimespan           time.Duration
	TargetTimePerBlock       time.Duration
	RetargetAdjustmentFactor int64
}

// GetPos33TicketMinerParam 获取ticket miner config params
func GetPos33TicketMinerParam(cfg *types.Chain33Config, height int64) *Pos33TicketMinerParam {
	conf := types.Conf(cfg, "mver.consensus.pos33")
	c := &Pos33TicketMinerParam{}
	c.CoinDevFund = conf.MGInt("coinDevFund", height) * types.Coin
	c.CoinReward = conf.MGInt("coinReward", height) * types.Coin
	c.FutureBlockTime = conf.MGInt("futureBlockTime", height)
	c.Pos33TicketPrice = conf.MGInt("ticketPrice", height) * types.Coin
	c.Pos33TicketFrozenTime = conf.MGInt("ticketFrozenTime", height)
	c.Pos33TicketWithdrawTime = conf.MGInt("ticketWithdrawTime", height)
	c.Pos33TicketMinerWaitTime = conf.MGInt("ticketMinerWaitTime", height)
	c.TargetTimespan = time.Duration(conf.MGInt("targetTimespan", height)) * time.Second
	c.TargetTimePerBlock = time.Duration(conf.MGInt("targetTimePerBlock", height)) * time.Second
	c.RetargetAdjustmentFactor = conf.MGInt("retargetAdjustmentFactor", height)
	return c
}

// Pos33AllTicketCountKeyPrefix for query all ticket count
const Pos33AllTicketCountKeyPrefix = "LODB-pos33-all:"

const (
	// Pos33BlockReward 区块奖励
	Pos33BlockReward = types.Coin * 30
	// Pos33SortBlocks 多少区块做一次抽签
	Pos33SortBlocks = 10
	// Pos33VoteReward 每ticket区块voter奖励
	Pos33VoteReward = types.Coin * 15 / Pos33RewardVotes
	// Pos33MakerReward 每ticket区块bp奖励
	Pos33MakerReward = types.Coin * 5 / Pos33RewardVotes
	// Pos33MakeerSize 候选区块maker数量
	Pos33MakerSize = 15
	// Pos33VoterSize  候选区块voter数量
	Pos33VoterSize = 30
	// Pos33RewardVotes 奖励的票数
	Pos33RewardVotes = 20
	// Pos33MustVotes 必须达到的票数
	Pos33MustVotes = 11
)

// Verify is verify vote msg
func (v *Pos33VoteMsg) Verify() bool {
	s := v.Sig
	v.Sig = nil
	b := crypto.Sha256(types.Encode(v))
	v.Sig = s
	return types.CheckSign(b, "", s)
}

// Sign is sign vote msg
func (v *Pos33VoteMsg) Sign(priv crypto.PrivKey) {
	v.Sig = nil
	b := crypto.Sha256(types.Encode(v))
	sig := priv.Sign(b)
	v.Sig = &types.Signature{Ty: types.SECP256K1, Pubkey: priv.PubKey().Bytes(), Signature: sig.Bytes()}
}

// ToString is reword to string
func (act *Pos33TicketMiner) ToString() string {
	b, err := json.MarshalIndent(act, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// Sorts is for sort []*Pos33SortMsg
type Sorts []*Pos33SortMsg

func (m Sorts) Len() int { return len(m) }
func (m Sorts) Less(i, j int) bool {
	return string(m[i].SortHash.Hash) < string(m[j].SortHash.Hash)
}
func (m Sorts) Swap(i, j int) { m[i], m[j] = m[j], m[i] }

// Votes is for sort []*Pos33SortMsg
type Votes []*Pos33VoteMsg

func (m Votes) Len() int { return len(m) }
func (m Votes) Less(i, j int) bool {
	return string(m[i].Sort.SortHash.Hash) < string(m[j].Sort.SortHash.Hash)
}
func (m Votes) Swap(i, j int) { m[i], m[j] = m[j], m[i] }
