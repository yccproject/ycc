// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/33cn/chain33/client"
	"github.com/33cn/chain33/common"
	"github.com/33cn/chain33/common/address"
	"github.com/33cn/chain33/common/crypto"
	"github.com/33cn/chain33/common/db"
	"github.com/33cn/chain33/common/log/log15"
	"github.com/33cn/chain33/types"
	wcom "github.com/33cn/chain33/wallet/common"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

var (
	minerAddrWhiteList = make(map[string]bool)
	bizlog             = log15.New("module", "wallet.pos33")
)

func init() {
	wcom.RegisterPolicy(ty.Pos33TicketX, New())
}

// New new instance
func New() wcom.WalletBizPolicy {
	return &ticketPolicy{mtx: &sync.Mutex{}}
}

type ticketPolicy struct {
	mtx                     *sync.Mutex
	walletOperate           wcom.WalletOperate
	store                   *ticketStore
	needFlush               bool
	miningPos33TicketTicker *time.Ticker
	autoMinerFlag           int32
	isPos33TicketLocked     int32
	minertimeout            *time.Timer
	cfg                     *subConfig
}

type subConfig struct {
	MinerWaitTime  string   `json:"minerWaitTime"`
	ForceMining    bool     `json:"forceMining"`
	Minerdisable   bool     `json:"minerdisable"`
	Minerwhitelist []string `json:"minerwhitelist"`
}

func (policy *ticketPolicy) initMingPos33TicketTicker(wait time.Duration) {
	policy.mtx.Lock()
	defer policy.mtx.Unlock()
	bizlog.Debug("initMingPos33TicketTicker", "Duration", wait)
	policy.miningPos33TicketTicker = time.NewTicker(wait)
}

func (policy *ticketPolicy) getMingPos33TicketTicker() *time.Ticker {
	policy.mtx.Lock()
	defer policy.mtx.Unlock()
	return policy.miningPos33TicketTicker
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
	return policy.isAutoMining()
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
	policy.store = newStore(walletBiz.GetDBStore())
	policy.needFlush = false
	policy.isPos33TicketLocked = 1
	policy.autoMinerFlag = policy.store.GetAutoMinerFlag()
	var subcfg subConfig
	if sub != nil {
		types.MustDecode(sub, &subcfg)
	}
	policy.cfg = &subcfg
	// policy.initMinerWhiteList(walletBiz.GetConfig())
	wait := 10 * time.Second
	if subcfg.MinerWaitTime != "" {
		d, err := time.ParseDuration(subcfg.MinerWaitTime)
		if err == nil {
			wait = d
		}
	}
	policy.initMingPos33TicketTicker(wait)
	walletBiz.RegisterMineStatusReporter(policy)
	// 启动自动挖矿
	walletBiz.GetWaitGroup().Add(1)
	go policy.autoMining()
}

// OnClose close
func (policy *ticketPolicy) OnClose() {
	policy.getMingPos33TicketTicker().Stop()
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
	return policy.onAddOrDeleteBlockTx(block, tx, index, dbbatch, true)
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
		address := address.PubKeyToAddress(pubkey)
		//from addr
		fromaddress := address.String()
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
		for _, v := range mact.Vs {
			pubkey := v.Sig.Pubkey
			addr := address.PubKeyToAddr(pubkey)
			if len(addr) != 0 && policy.walletOperate.AddrInWallet(addr) {
				wtxdetail.Fromaddr = addr
				n++
			}
		}
		if !isMaker {
			wtxdetail.Amount = 0
		}
		wtxdetail.Amount += ty.Pos33VoteReward * n
	}

	if policy.checkNeedFlushPos33Ticket(tx, receipt) {
		policy.needFlush = true
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
	// 钱包锁住时，不允许挖矿
	atomic.CompareAndSwapInt32(&policy.isPos33TicketLocked, 0, 1)
	FlushPos33Ticket(policy.getAPI())
}

//解锁超时处理，需要区分整个钱包的解锁或者只挖矿的解锁
func (policy *ticketPolicy) resetTimeout(Timeout int64) {
	if policy.minertimeout == nil {
		policy.minertimeout = time.AfterFunc(time.Second*time.Duration(Timeout), func() {
			//wallet.isPos33TicketLocked = true
			atomic.CompareAndSwapInt32(&policy.isPos33TicketLocked, 0, 1)
		})
	} else {
		policy.minertimeout.Reset(time.Second * time.Duration(Timeout))
	}
}

// OnWalletUnlocked process unlock event
func (policy *ticketPolicy) OnWalletUnlocked(param *types.WalletUnLock) {
	if param.WalletOrTicket {
		atomic.CompareAndSwapInt32(&policy.isPos33TicketLocked, 1, 0)
		if param.Timeout != 0 {
			policy.resetTimeout(param.Timeout)
		}
	}
	// 钱包解锁时，需要刷新，通知挖矿
	FlushPos33Ticket(policy.getAPI())
}

// OnCreateNewAccount process create new account event
func (policy *ticketPolicy) OnCreateNewAccount(acc *types.Account) {
}

// OnImportPrivateKey 导入key的时候flush ticket
func (policy *ticketPolicy) OnImportPrivateKey(acc *types.Account) {
	FlushPos33Ticket(policy.getAPI())
}

// OnAddBlockFinish process finish block
func (policy *ticketPolicy) OnAddBlockFinish(block *types.BlockDetail) {
	if policy.needFlush {
		// 新增区块，由于ticket具有锁定期，所以这里不需要刷新
	}
	policy.needFlush = false
}

// OnDeleteBlockFinish process finish block
func (policy *ticketPolicy) OnDeleteBlockFinish(block *types.BlockDetail) {
	if policy.needFlush {
		FlushPos33Ticket(policy.getAPI())
	}
	policy.needFlush = false
}

// FlushPos33Ticket flush ticket
func FlushPos33Ticket(api client.QueueProtocolAPI) {
	bizlog.Debug("wallet FLUSH TICKET")
	api.Notify("consensus", types.EventConsensusQuery, &types.ChainExecutor{
		Driver:   "pos33",
		FuncName: "FlushPos33Ticket",
		Param:    types.Encode(&types.ReqNil{}),
	})
}

func (policy *ticketPolicy) needFlushPos33Ticket(tx *types.Transaction, receipt *types.ReceiptData) bool {
	pubkey := tx.Signature.GetPubkey()
	addr := address.PubKeyToAddress(pubkey)
	return policy.store.checkAddrIsInWallet(addr.String())
}

func (policy *ticketPolicy) checkNeedFlushPos33Ticket(tx *types.Transaction, receipt *types.ReceiptData) bool {
	if receipt.Ty != types.ExecOk {
		return false
	}
	return policy.needFlushPos33Ticket(tx, receipt)
}

func (policy *ticketPolicy) closePos33Tickets(priv crypto.PrivKey, count int) (*types.ReplyHashes, error) {
	bizlog.Debug("closePos33Tickets", "count", count)
	max := 1000
	if count == 0 || count > max {
		count = max
	}
	ta := &ty.Pos33TicketAction{}
	tclose := &ty.Pos33TicketClose{Count: int32(count)}
	ta.Value = &ty.Pos33TicketAction_Tclose{Tclose: tclose}
	ta.Ty = ty.Pos33TicketActionClose
	hash, err := policy.getWalletOperate().SendTransaction(ta, []byte(ty.Pos33TicketX), priv, "")
	if err != nil {
		return nil, err
	}
	return &types.ReplyHashes{Hashes: [][]byte{hash}}, nil
}

func (policy *ticketPolicy) setAutoMining(flag int32) {
	atomic.StoreInt32(&policy.autoMinerFlag, flag)
}

func (policy *ticketPolicy) isAutoMining() bool {
	return atomic.LoadInt32(&policy.autoMinerFlag) == 1
}

func (policy *ticketPolicy) processFee(priv crypto.PrivKey) error {
	addr := address.PubKeyToAddress(priv.PubKey().Bytes()).String()
	operater := policy.getWalletOperate()
	acc1, err := operater.GetBalance(addr, "coins")
	if err != nil {
		return err
	}
	acc2, err := operater.GetBalance(addr, ty.Pos33TicketX)
	if err != nil {
		return err
	}
	toaddr := address.ExecAddress(ty.Pos33TicketX)
	//如果acc2 的余额足够，那题withdraw 部分钱做手续费
	if (acc1.Balance < (types.Coin)) && (acc2.Balance > types.Coin) {
		_, err := operater.SendToAddress(priv, toaddr, -types.Coin, "pos33->coins", false, "")
		if err != nil {
			return err
		}
	}
	return nil
}

//手续费处理
func (policy *ticketPolicy) processFees() error {
	keys, err := policy.getWalletOperate().GetAllPrivKeys()
	if err != nil {
		return err
	}
	for _, key := range keys {
		e := policy.processFee(key)
		if e != nil {
			err = e
		}
	}
	return err
}

func (policy *ticketPolicy) withdrawFromPos33TicketOne(priv crypto.PrivKey) ([]byte, error) {
	addr := address.PubKeyToAddress(priv.PubKey().Bytes()).String()
	operater := policy.getWalletOperate()
	acc, err := operater.GetBalance(addr, ty.Pos33TicketX)
	if err != nil {
		return nil, err
	}
	// 避免频繁的发送 withdraw tx，所以限定 1000
	if acc.Balance > 1000 {
		bizlog.Debug("withdraw", "amount", acc.Balance)
		hash, err := operater.SendToAddress(priv, address.ExecAddress(ty.Pos33TicketX), -acc.Balance, "autominer->withdraw", false, "")
		if err != nil {
			return nil, err
		}
		return hash.GetHash(), nil
	}
	return nil, nil
}

func (policy *ticketPolicy) openticket(mineraddr, returnaddr string, priv crypto.PrivKey, count int32) ([]byte, error) {
	bizlog.Debug("openticket", "mineraddr", mineraddr, "returnaddr", returnaddr, "count", count)
	if count > ty.Pos33TicketCountOpenOnce {
		count = ty.Pos33TicketCountOpenOnce
		bizlog.Debug("openticket", "Update count", "wait for another open")
	}

	ta := &ty.Pos33TicketAction{}
	topen := &ty.Pos33TicketOpen{MinerAddress: mineraddr, ReturnAddress: returnaddr, Count: count}
	ta.Value = &ty.Pos33TicketAction_Topen{Topen: topen}
	ta.Ty = ty.Pos33TicketActionOpen
	return policy.walletOperate.SendTransaction(ta, []byte(ty.Pos33TicketX), priv, "")
}

func (policy *ticketPolicy) buyPos33TicketOne(height int64, priv crypto.PrivKey) ([]byte, int, error) {
	//ticket balance and coins balance
	addr := address.PubKeyToAddress(priv.PubKey().Bytes()).String()
	operater := policy.getWalletOperate()
	acc1, err := operater.GetBalance(addr, "coins")
	if err != nil {
		return nil, 0, err
	}
	acc2, err := operater.GetBalance(addr, ty.Pos33TicketX)
	if err != nil {
		return nil, 0, err
	}
	//留一个币作为手续费，如果手续费不够了，不能挖矿
	//判断手续费是否足够，如果不足要及时补充。
	chain33Cfg := policy.walletOperate.GetAPI().GetConfig()
	cfg := ty.GetPos33TicketMinerParam(chain33Cfg, height)
	fee := types.Coin * 10
	if acc1.Balance+acc2.Balance-2*fee >= cfg.Pos33TicketPrice {
		// 如果可用余额+冻结余额，可以凑成新票，则转币到冻结余额
		if (acc1.Balance+acc2.Balance-2*fee)/cfg.Pos33TicketPrice > acc2.Balance/cfg.Pos33TicketPrice {
			//第一步。转移币到 ticket
			toaddr := address.ExecAddress(ty.Pos33TicketX)
			amount := acc1.Balance - 2*fee
			//必须大于0，才需要转移币
			var hash *types.ReplyHash
			if amount > 0 {
				bizlog.Debug("buyPos33TicketOne.send", "toaddr", toaddr, "amount", amount)
				hash, err = policy.walletOperate.SendToAddress(priv, toaddr, amount, "coins->pos33", false, "")

				if err != nil {
					return nil, 0, err
				}
				bizlog.Info("buyticket tx hash", "hash", common.HashHex(hash.Hash))
				operater.WaitTx(hash.Hash)
				bizlog.Info("buyticket send ok")
			}
		}

		acc, err := operater.GetBalance(addr, ty.Pos33TicketX)
		if err != nil {
			return nil, 0, err
		}
		count := acc.Balance / cfg.Pos33TicketPrice
		bizlog.Debug("go here", "acc.balance", acc.Balance, "count", count)
		if count > 0 {
			txhash, err := policy.openticket(addr, addr, priv, int32(count))
			return txhash, int(count), err
		}
	}
	return nil, 0, nil
}

func (policy *ticketPolicy) buyPos33Ticket(height int64) ([][]byte, int, error) {
	minerPriv, minerAddr, err := policy.getMiner()
	if err != nil {
		return nil, 0, err
	}
	count := 0
	var hashes [][]byte
	hash, n, err := policy.buyPos33TicketOne(height, minerPriv)
	if err != nil {
		bizlog.Error("ticketPolicy buyPos33Ticket buyPos33TicketOne", "err", err)
		return nil, 0, err
	}
	count += n
	if hash != nil {
		hashes = append(hashes, hash)
	}
	bizlog.Debug("ticketPolicy buyPos33Ticket", "Address", minerAddr, "txhash", hex.EncodeToString(hash), "n", n)
	return hashes, count, nil
}

func (policy *ticketPolicy) getMiner() (crypto.PrivKey, string, error) {
	accs, err := policy.getWalletOperate().GetWalletAccounts()
	if err != nil {
		bizlog.Error("getMiner.GetWalletAccounts", "err", err)
		return nil, "", err
	}
	var minerAddr string
	for _, acc := range accs {
		if acc.Label == "mining" {
			minerAddr = acc.Addr
			break
		}
	}
	if minerAddr == "" {
		err := fmt.Errorf("No mining label account in wallet")
		bizlog.Error("getMiner error", "err", err)
		return nil, "", err
	}

	privs, err := policy.getWalletOperate().GetAllPrivKeys()
	if err != nil {
		bizlog.Error("getMiner.getAllPrivKeys", "err", err)
		return nil, "", err
	}
	var minerPriv crypto.PrivKey
	for _, priv := range privs {
		if address.PubKeyToAddress(priv.PubKey().Bytes()).String() == minerAddr {
			minerPriv = priv
			break
		}
	}
	return minerPriv, minerAddr, nil
}

func (policy *ticketPolicy) withdrawFromPos33Ticket() (hashes [][]byte, err error) {
	privs, err := policy.getWalletOperate().GetAllPrivKeys()
	if err != nil {
		bizlog.Error("withdrawFromPos33Ticket.getAllPrivKeys", "err", err)
		return nil, err
	}
	for _, priv := range privs {
		hash, err := policy.withdrawFromPos33TicketOne(priv)
		if err != nil {
			bizlog.Error("withdrawFromPos33TicketOne", "err", err)
			continue
		}
		if hash != nil {
			hashes = append(hashes, hash)
		}
	}
	return hashes, nil
}

//检查周期 --> 10分
//开启挖矿：
//1. 自动把成熟的ticket关闭
//2. 查找超过1万余额的账户，自动购买ticket
//3. 查找mineraddress 和他对应的 账户的余额（不在1中），余额超过1万的自动购买ticket 挖矿
//
//停止挖矿：
//1. 自动把成熟的ticket关闭
//2. 查找ticket 可取的余额
//3. 取出ticket 里面的钱
func (policy *ticketPolicy) autoMining() {
	bizlog.Debug("Begin auto mining")
	defer bizlog.Debug("End auto mining")
	operater := policy.getWalletOperate()
	defer operater.GetWaitGroup().Done()

	// 只有ticket共识下ticket相关的操作才有效
	cfg := policy.walletOperate.GetAPI().GetConfig()
	q := types.Conf(cfg, "config.consensus")
	if q != nil {
		cons := q.GStr("name")
		if strings.Compare(strings.TrimSpace(cons), ty.Pos33TicketX) != 0 {
			bizlog.Debug("consensus is not ticket, exit mining")
			return
		}
	}

	lastHeight := int64(0)
	miningPos33TicketTicker := policy.getMingPos33TicketTicker()
	for {
		select {
		case <-miningPos33TicketTicker.C:
			if policy.cfg.Minerdisable {
				bizlog.Debug("autoMining, GetMinerdisable() is true, exit autoMining()")
				break
			}
			if !(operater.IsCaughtUp() || policy.cfg.ForceMining) {
				bizlog.Error("wallet IsCaughtUp false")
				break
			}
			//判断高度是否增长
			height := operater.GetBlockHeight()
			if height <= lastHeight {
				bizlog.Error("wallet Height not inc", "height", height, "lastHeight", lastHeight)
				break
			}
			lastHeight = height
			if policy.isAutoMining() {
				err := policy.processFees()
				if err != nil {
					bizlog.Error("processFees", "err", err)
				}
				hashs, n, err := policy.buyPos33Ticket(lastHeight + 1)
				if err != nil {
					bizlog.Error("buyPos33Ticket", "err", err)
				}
				if len(hashs) > 0 {
					operater.WaitTxs(hashs)
				}
				if n > 0 {
					FlushPos33Ticket(policy.getAPI())
				}
			} else {
				err := policy.processFees()
				if err != nil {
					bizlog.Error("processFees", "err", err)
				}
				hashes, err := policy.withdrawFromPos33Ticket()
				if err != nil {
					bizlog.Error("withdrawFromPos33Ticket", "err", err)
				}
				if len(hashes) > 0 {
					operater.WaitTxs(hashes)
				}
			}
			bizlog.Debug("END miningPos33Ticket")
		case <-operater.GetWalletDone():
			return
		}
	}
}
