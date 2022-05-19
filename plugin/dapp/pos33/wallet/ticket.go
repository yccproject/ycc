// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/33cn/chain33/client"
	"github.com/33cn/chain33/common/address"
	"github.com/33cn/chain33/common/crypto"
	"github.com/33cn/chain33/common/db"
	"github.com/33cn/chain33/common/log/log15"
	"github.com/33cn/chain33/types"
	wcom "github.com/33cn/chain33/wallet/common"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

const ethID = ty.EthAddrID

var (
	bizlog = log15.New("module", "wallet.pos33")
)

func init() {
	wcom.RegisterPolicy(ty.Pos33TicketX, New())
}

// New new instance
func New() wcom.WalletBizPolicy {
	return &ticketPolicy{mtx: &sync.Mutex{}}
}

type ticketPolicy struct {
	mtx                 *sync.Mutex
	walletOperate       wcom.WalletOperate
	isPos33TicketLocked int32
	cfg                 *subConfig
}

type subConfig struct {
	MinerWaitTime  string   `json:"minerWaitTime"`
	ForceMining    bool     `json:"forceMining"`
	Minerdisable   bool     `json:"minerdisable"`
	Minerwhitelist []string `json:"minerwhitelist"`
}

func (policy *ticketPolicy) setWalletOperate(walletBiz wcom.WalletOperate) {
	policy.mtx.Lock()
	defer policy.mtx.Unlock()
	policy.walletOperate = walletBiz
}

func (policy *ticketPolicy) getWalletOperate() wcom.WalletOperate {
	policy.mtx.Lock()
	defer policy.mtx.Unlock()
	return policy.walletOperate
}

func (policy *ticketPolicy) getAPI() client.QueueProtocolAPI {
	policy.mtx.Lock()
	defer policy.mtx.Unlock()
	return policy.walletOperate.GetAPI()
}

// IsAutoMining check auto mining
func (policy *ticketPolicy) IsAutoMining() bool {
	// return policy.isAutoMining()
	return false
}

// IsPos33TicketLocked check lock status
func (policy *ticketPolicy) IsTicketLocked() bool {
	return atomic.LoadInt32(&policy.isPos33TicketLocked) != 0
}

// PolicyName return policy name
func (policy *ticketPolicy) PolicyName() string {
	return ty.Pos33TicketX
}

// Init initial
func (policy *ticketPolicy) Init(walletBiz wcom.WalletOperate, sub []byte) {
	policy.setWalletOperate(walletBiz)
	policy.isPos33TicketLocked = 1
	var subcfg subConfig
	if sub != nil {
		types.MustDecode(sub, &subcfg)
	}
	policy.cfg = &subcfg
	walletBiz.RegisterMineStatusReporter(policy)
}

// OnClose close
func (policy *ticketPolicy) OnClose() {
}

// OnSetQueueClient on set queue client
func (policy *ticketPolicy) OnSetQueueClient() {

}

// Call call
func (policy *ticketPolicy) Call(funName string, in types.Message) (ret types.Message, err error) {
	err = types.ErrNotSupport
	return
}

// OnAddBlockTx add Block tx
func (policy *ticketPolicy) OnAddBlockTx(block *types.BlockDetail, tx *types.Transaction, index int32, dbbatch db.Batch) *types.WalletTxDetail {
	cfg := policy.getAPI().GetConfig()
	ok := cfg.MIsEnable("mver.consensus.addWalletTx", block.Block.Height)
	if ok {
		return policy.onAddOrDeleteBlockTx(block, tx, index, dbbatch, true)
	}
	bizlog.Debug("OnAddBlockTx mver.consensus.addWalletTx is disabled")
	return nil
}
func (policy *ticketPolicy) onAddOrDeleteBlockTx(block *types.BlockDetail, tx *types.Transaction, index int32, dbbatch db.Batch, isAdd bool) *types.WalletTxDetail {
	receipt := block.Receipts[index]
	amount, _ := tx.Amount()
	wtxdetail := &types.WalletTxDetail{
		Tx:         tx,
		Height:     block.Block.Height,
		Index:      int64(index),
		Receipt:    receipt,
		Blocktime:  block.Block.BlockTime,
		ActionName: tx.ActionName(),
		Amount:     amount,
		Payload:    nil,
	}
	isMaker := false
	if len(wtxdetail.Fromaddr) <= 0 {
		pubkey := tx.Signature.GetPubkey()
		address := address.PubKeyToAddr(ethID, pubkey)
		//from addr
		fromaddress := address
		if len(fromaddress) != 0 && policy.walletOperate.AddrInWallet(fromaddress) {
			wtxdetail.Fromaddr = fromaddress
			if index == 0 {
				isMaker = true
			}
		}
	}
	if len(wtxdetail.Fromaddr) <= 0 {
		toaddr := tx.GetTo()
		if len(toaddr) != 0 && policy.walletOperate.AddrInWallet(toaddr) {
			wtxdetail.Fromaddr = toaddr
		}
	}
	//  如果奖励交易里包含钱包地址的投票，那么fromaddr = voter.addr
	if index == 0 {
		var pact ty.Pos33TicketAction
		err := types.Decode(tx.Payload, &pact)
		if err != nil {
			bizlog.Error("pos33action decode error", "err", err)
			return nil
		}
		mact := pact.GetMiner()
		n := int64(0)
		for _, pk := range mact.BlsPkList {
			addr := address.PubKeyToAddr(ethID, pk)
			msg, err := policy.getAPI().Query(ty.Pos33TicketX, "Pos33BlsAddr", &types.ReqAddr{Addr: addr})
			if err != nil {
				break
			}
			raddr := msg.(*types.ReplyString).Data
			if len(addr) != 0 && policy.walletOperate.AddrInWallet(raddr) {
				wtxdetail.Fromaddr = raddr
				n++
			}
		}
		if !isMaker {
			wtxdetail.Amount = 0
		}
		cfg := policy.getAPI().GetConfig()
		coin := cfg.GetCoinPrecision()
		if cfg.IsDappFork(block.Block.Height, ty.Pos33TicketX, "ForkReward15") {
			wtxdetail.Amount += coin / 4 * n
		} else {
			wtxdetail.Amount += coin / 2 * n
		}
	}

	if len(wtxdetail.Fromaddr) > 0 {
		if isAdd {
			bizlog.Info("wallet block added", "height", block.Block.Height, "from", wtxdetail.Fromaddr, "amount", wtxdetail.Amount)
		} else {
			bizlog.Info("wallet block delete", "height", block.Block.Height, "from", wtxdetail.Fromaddr, "amount", wtxdetail.Amount)
		}
	}
	return wtxdetail
}

// OnDeleteBlockTx on delete block
func (policy *ticketPolicy) OnDeleteBlockTx(block *types.BlockDetail, tx *types.Transaction, index int32, dbbatch db.Batch) *types.WalletTxDetail {
	return policy.onAddOrDeleteBlockTx(block, tx, index, dbbatch, false)
}

// SignTransaction sign tx
func (policy *ticketPolicy) SignTransaction(key crypto.PrivKey, req *types.ReqSignRawTx) (needSysSign bool, signtx string, err error) {
	needSysSign = true
	return
}

// OnWalletLocked process lock event
func (policy *ticketPolicy) OnWalletLocked() {
}

// OnWalletUnlocked process unlock event
func (policy *ticketPolicy) OnWalletUnlocked(param *types.WalletUnLock) {
}

// OnCreateNewAccount process create new account event
func (policy *ticketPolicy) OnCreateNewAccount(acc *types.Account) {
}

// OnImportPrivateKey 导入key的时候flush ticket
func (policy *ticketPolicy) OnImportPrivateKey(acc *types.Account) {
}

// OnAddBlockFinish process finish block
func (policy *ticketPolicy) OnAddBlockFinish(block *types.BlockDetail) {
}

// OnDeleteBlockFinish process finish block
func (policy *ticketPolicy) OnDeleteBlockFinish(block *types.BlockDetail) {
}

func (policy *ticketPolicy) setMinerFeeRate(priv crypto.PrivKey, fr *ty.Pos33MinerFeeRate) (*types.ReplyHash, error) {
	feeRate := float64(fr.FeeRatePersent) / 100.
	bizlog.Info("setMinerFeeRate", "maddr", fr.MinerAddr, "fee rate", feeRate)
	ta := &ty.Pos33TicketAction{}
	ta.Value = &ty.Pos33TicketAction_FeeRate{FeeRate: fr}
	ta.Ty = ty.Pos33ActionMinerFeeRate
	hash, err := policy.getWalletOperate().SendTransaction(ta, []byte(ty.Pos33TicketX), priv, "")
	if err != nil {
		return nil, err
	}
	return &types.ReplyHash{Hash: hash}, nil
}

func (policy *ticketPolicy) migrate(priv crypto.PrivKey) ([]byte, error) {
	addr := address.PubKeyToAddr(ethID, priv.PubKey().Bytes())
	_, err := policy.getAPI().Query(ty.Pos33TicketX, "Pos33ConsigneeEntrust", &types.ReqAddr{Addr: addr})
	if err == nil {
		bizlog.Info("already migrate", "addr", addr)
		return nil, errors.New("already migtated")
	} else {
		bizlog.Error("migrate query error", "err", err, "addr", addr)
	}
	act := &ty.Pos33TicketAction{
		Value: &ty.Pos33TicketAction_Migrate{
			Migrate: &ty.Pos33Migrate{
				Miner: addr,
			},
		},
	}
	act.Ty = ty.Pos33ActionMigrate
	bizlog.Info("pos33 migrate", "miner", addr)
	return policy.walletOperate.SendTransaction(act, []byte(ty.Pos33TicketX), priv, "")
}

func (policy *ticketPolicy) blsBind(priv crypto.PrivKey) ([]byte, error) {
	addr := address.PubKeyToAddr(ethID, priv.PubKey().Bytes())
	blsPk := ty.Hash2BlsSk(crypto.Sha256(priv.Bytes())).PubKey()
	blsaddr := address.PubKeyToAddr(ethID, blsPk.Bytes())
	m, err := policy.getAPI().Query(ty.Pos33TicketX, "Pos33BlsAddr", &types.ReqAddr{Addr: blsaddr})
	if err == nil {
		retAddr := string(m.(*types.ReplyString).Data)
		if addr != retAddr {
			bizlog.Error("blsbind error, bind addr NOT same", "addr", addr, "retAddr", retAddr)
			return nil, errors.New("blsbind error")
		}
		bizlog.Info("already bind blsaddr", "blsaddr", blsaddr, "addr", addr)
		return nil, errors.New("already binded")
	}

	act := &ty.Pos33TicketAction{
		Value: &ty.Pos33TicketAction_BlsBind{BlsBind: &ty.Pos33BlsBind{BlsAddr: blsaddr}},
		Ty:    ty.Pos33ActionBlsBind,
	}

	cfg := policy.getAPI().GetConfig()
	tx, err := types.CreateFormatTx(cfg, "pos33", types.Encode(act))
	if err != nil {
		return nil, err
	}

	signID := types.EncodeSignID(types.SECP256K1, ethID)
	tx.Sign(signID, priv)
	bizlog.Info("bind blsaddr", "blsaddr", blsaddr, "addr", addr)
	r, err := policy.getAPI().SendTx(tx)
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

func (policy *ticketPolicy) getMiner(minerAddr string) (crypto.PrivKey, string, error) {
	accs, err := policy.getWalletOperate().GetWalletAccounts()
	if err != nil {
		// bizlog.Error("getMiner.GetWalletAccounts", "err", err)
		return nil, "", err
	}
	if minerAddr == "" {
		for _, acc := range accs {
			if acc.Label == "mining" {
				minerAddr = acc.Addr
				break
			}
		}
		if minerAddr == "" {
			err := fmt.Errorf("no mining label account in wallet")
			bizlog.Error("getMiner error", "err", err)
			return nil, "", err
		}
	}

	privs, err := policy.getWalletOperate().GetAllPrivKeys()
	if err != nil {
		bizlog.Error("getMiner.getAllPrivKeys", "err", err)
		return nil, "", err
	}
	var minerPriv crypto.PrivKey
	for _, priv := range privs {
		if address.PubKeyToAddr(ethID, priv.PubKey().Bytes()) == minerAddr {
			minerPriv = priv
			break
		}
	}
	return minerPriv, minerAddr, nil
}
