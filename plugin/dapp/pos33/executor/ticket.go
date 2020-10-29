// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package executor

/*
coins 是一个货币的exec。内置货币的执行器。

主要提供两种操作：

EventTransfer -> 转移资产
*/

//package none execer for unknow execer
//all none transaction exec ok, execept nofee
//nofee transaction will not pack into block

import (
	log "github.com/33cn/chain33/common/log/log15"
	drivers "github.com/33cn/chain33/system/dapp"
	"github.com/33cn/chain33/types"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

var clog = log.New("module", "execs.pos33")
var driverName = "pos33"

// Init initial
func Init(name string, cfg *types.Chain33Config, sub []byte) {
	drivers.Register(cfg, GetName(), newPos33Ticket, cfg.GetDappFork(driverName, "Enable"))
	InitExecType()
}

// InitExecType reg types
func InitExecType() {
	ety := types.LoadExecutorType(driverName)
	ety.InitFuncList(types.ListMethod(&Pos33Ticket{}))
}

// GetName get name
func GetName() string {
	return newPos33Ticket().GetName()
}

// Pos33Ticket driver type
type Pos33Ticket struct {
	drivers.DriverBase
}

func newPos33Ticket() drivers.Driver {
	t := &Pos33Ticket{}
	t.SetChild(t)
	t.SetExecutorType(types.LoadExecutorType(driverName))
	return t
}

// GetDriverName ...
func (t *Pos33Ticket) GetDriverName() string {
	return driverName
}

// IsFriend check is fri
func (t *Pos33Ticket) IsFriend(myexec, writekey []byte, tx *types.Transaction) bool {
	return true
	//clog.Error("pos33.ticket  IsFriend", "myex", string(myexec), "writekey", string(writekey))
	//不允许平行链
	//return false
}

// CheckTx check tx
func (t *Pos33Ticket) CheckTx(tx *types.Transaction, index int) error {
	//index == -1 only when check in mempool
	if index == -1 {
		var action ty.Pos33TicketAction
		err := types.Decode(tx.Payload, &action)
		if err != nil {
			return err
		}
		if action.Ty == ty.Pos33TicketActionMiner && action.GetMiner() != nil {
			return ty.ErrMinerTx
		}
	}
	return nil
}

// CheckReceiptExecOk return true to check if receipt ty is ok
func (t *Pos33Ticket) CheckReceiptExecOk() bool {
	return true
}
