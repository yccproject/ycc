// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package types

import (
	"encoding/json"
	"reflect"

	"github.com/33cn/chain33/common/crypto"
	"github.com/33cn/chain33/system/address/eth"
	"github.com/33cn/chain33/types"
	bls33 "github.com/33cn/plugin/plugin/crypto/bls"
	"github.com/phoreproject/bls"
	"github.com/phoreproject/bls/g1pubs"
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
	TyLogPos33TicketBind = 334
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
	Pos33TicketActionBind = 17

	// Pos33ActionEntrust action entrust
	Pos33ActionEntrust = 18
	// Pos33ActionMigrate action migrate
	Pos33ActionMigrate = 19
	// Pos33ActionBlsBind action bls bind
	Pos33ActionBlsBind = 20
	// Pos33MinerFeeRate action set miner fee rate
	Pos33ActionMinerFeeRate = 21
	// Pos33ActionWithdrawReward action withdraw reward
	Pos33ActionWithdrawReward = 22
)

// Pos33TicketX dapp name
const Pos33TicketX = "pos33"
const EthAddrID = eth.ID

func init() {
	types.AllowUserExec = append(types.AllowUserExec, []byte(Pos33TicketX))
	types.RegFork(Pos33TicketX, InitFork)
	types.RegExec(Pos33TicketX, InitExecutor)
}

func InitFork(cfg *types.Chain33Config) {
	cfg.RegisterDappFork(Pos33TicketX, "Enable", 0)
	cfg.RegisterDappFork(Pos33TicketX, "ForkReward15", 725000)
	cfg.RegisterDappFork(Pos33TicketX, "ForkFixReward", 5000000)
	cfg.RegisterDappFork(Pos33TicketX, "UseEntrust", 7000000)
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
		TyLogPos33TicketBind:  {Ty: reflect.TypeOf(ReceiptPos33TicketBind{}), Name: "LogPos33TicketBind"},
	}
}

// Amount get amount
func (ticket Pos33TicketType) Amount(tx *types.Transaction) (int64, error) {
	var action Pos33TicketAction
	err := types.Decode(tx.GetPayload(), &action)
	if err != nil {
		return 0, types.ErrDecode
	}
	cfg := ticket.GetConfig()
	reward := cfg.GetCoinPrecision() * 22 / 100
	reward /= 2
	if action.Ty == Pos33TicketActionMiner && action.GetMiner() != nil {
		ticketMiner := action.GetMiner()
		if ticketMiner == nil {
			return 0, nil
		}
		nvs := len(ticketMiner.BlsPkList)
		bpr := reward * int64(nvs)
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
		"Genesis":  Pos33TicketActionGenesis,
		"Topen":    Pos33TicketActionOpen,
		"Tbind":    Pos33TicketActionBind,
		"Tclose":   Pos33TicketActionClose,
		"Miner":    Pos33TicketActionMiner,
		"Entrust":  Pos33ActionEntrust,
		"Migrate":  Pos33ActionMigrate,
		"BlsBind":  Pos33ActionBlsBind,
		"Withdraw": Pos33ActionWithdrawReward,
	}
}

type Pos33MineParam struct {
	TicketPrice1    int64
	TicketPrice2    int64
	TicketPrice3    int64
	MinerFeePersent int64
	RewardTransfer  int64
	BlockReward     int64
	VoteReward      int64
	MineReward      int64

	cfg    *types.Chain33Config
	height int64
}

// GetPos33MineParam 获取ticket miner config params
func GetPos33MineParam(cfg *types.Chain33Config, height int64) *Pos33MineParam {
	conf := types.Conf(cfg, "mver.consensus.pos33")
	c := &Pos33MineParam{}
	c.TicketPrice1 = conf.MGInt("ticketPrice1", height) * cfg.GetCoinPrecision()
	c.TicketPrice2 = conf.MGInt("ticketPrice2", height) * cfg.GetCoinPrecision()
	c.MinerFeePersent = conf.MGInt("minerFeePersent", height)
	c.RewardTransfer = conf.MGInt("rewardTransfer", height) * cfg.GetCoinPrecision()
	c.BlockReward = conf.MGInt("blockReward", height) * cfg.GetCoinPrecision()
	c.VoteReward = conf.MGInt("voteRewardPersent", height) * cfg.GetCoinPrecision() / 100
	c.MineReward = conf.MGInt("mineRewardPersent", height) * cfg.GetCoinPrecision() / 100
	c.cfg = cfg
	c.height = height
	return c
}

func (mp *Pos33MineParam) ChangeTicketPrice() bool {
	return mp.cfg.GetDappFork("pos33", "UseEntrust") == mp.height
}

func (mp *Pos33MineParam) GetTicketPrice() int64 {
	if mp.cfg.IsDappFork(mp.height, Pos33TicketX, "UseEntrust") {
		return mp.TicketPrice2
	}
	return mp.TicketPrice1
}

const (
	// Pos33SortBlocks 多少区块做一次抽签
	Pos33SortBlocks = 10
	// Pos33MakeerSize 候选区块maker数量
	Pos33MakerSize = 15
	// Pos33VoterSize  候选区块voter数量
	Pos33VoterSize = 25
	// Pos33MustVotes 必须达到的票数
	Pos33MustVotes = 17
)

// Verify is verify msg
func (v *Pos33SortsVote) Verify() bool {
	s := v.Sig
	v.Sig = nil
	b := crypto.Sha256(types.Encode(v))
	v.Sig = s
	return types.CheckSign(b, "", s, v.Height)
}

// Sign is sign msg
func (v *Pos33SortsVote) Sign(priv crypto.PrivKey) {
	v.Sig = nil
	b := crypto.Sha256(types.Encode(v))
	sig := priv.Sign(b)
	v.Sig = &types.Signature{Ty: types.SECP256K1, Pubkey: priv.PubKey().Bytes(), Signature: sig.Bytes()}
}

// Verify is verify vote msg
func (v *Pos33VoteMsg) Verify() bool {
	return types.CheckSign(v.Hash, Pos33TicketX, v.Sig, v.Sort.Proof.Input.Height)
}

// Sign is sign vote msg
func (v *Pos33VoteMsg) Sign(priv crypto.PrivKey) {
	blsSk := Hash2BlsSk(crypto.Sha256(priv.Bytes()))
	// v.Sig = nil
	// b := crypto.Sha256(types.Encode(v))
	sig := blsSk.Sign(v.Hash)
	v.Sig = &types.Signature{Ty: bls33.ID, Pubkey: blsSk.PubKey().Bytes(), Signature: sig.Bytes()}
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
	if m[i].Sort.SortHash.Num < m[j].Sort.SortHash.Num {
		return true
	}
	return string(m[i].Sort.SortHash.Hash) < string(m[j].Sort.SortHash.Hash)
}
func (m Votes) Swap(i, j int) { m[i], m[j] = m[j], m[i] }

func Hash2BlsSk(hash []byte) crypto.PrivKey {
	var h [32]byte
	copy(h[:], hash)
	re := bls.HashSecretKey(h).ToRepr()
	sk := g1pubs.KeyFromFQRepr(re)
	return bls33.PrivKeyBLS(sk.Serialize())
}

func (m *Pos33MinerMsg) Verify() error {
	if len(m.BlsPkList) == 0 {
		return nil
	}
	d := new(bls33.Driver)
	var pks []crypto.PubKey
	for _, b := range m.BlsPkList {
		pk := bls33.PubKeyBLS{}
		copy(pk[:], b)
		pks = append(pks, pk)
	}
	var sig bls33.SignatureBLS
	copy(sig[:], m.BlsSig)
	return d.VerifyAggregatedOne(pks, m.Hash, sig)
}
