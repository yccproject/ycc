// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package executor

import (
	"github.com/33cn/chain33/types"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

func (t *Pos33Ticket) execDelLocal(receiptData *types.ReceiptData) (*types.LocalDBSet, error) {
	dbSet := &types.LocalDBSet{}
	return dbSet, nil
}

// ExecDelLocal_Genesis exec del local genesis
func (t *Pos33Ticket) ExecDelLocal_Genesis(payload *ty.Pos33TicketGenesis, tx *types.Transaction, receiptData *types.ReceiptData, index int) (*types.LocalDBSet, error) {
	tlog.Info("ExecDelLocal_Genesis", "height", t.GetHeight())
	return t.execDelLocal(receiptData)
}

// ExecDelLocal_Topen exec del local open
func (t *Pos33Ticket) ExecDelLocal_Topen(payload *ty.Pos33TicketOpen, tx *types.Transaction, receiptData *types.ReceiptData, index int) (*types.LocalDBSet, error) {
	tlog.Info("ExecDelLocal_Topen", "height", t.GetHeight())
	return t.execDelLocal(receiptData)
}

// ExecDelLocal_Tclose exec del local close
func (t *Pos33Ticket) ExecDelLocal_Tclose(payload *ty.Pos33TicketClose, tx *types.Transaction, receiptData *types.ReceiptData, index int) (*types.LocalDBSet, error) {
	tlog.Info("ExecDelLocal_Tclose", "height", t.GetHeight())
	return t.execDelLocal(receiptData)
}

// ExecDelLocal_Miner exec del local miner
func (t *Pos33Ticket) ExecDelLocal_Miner(payload *ty.Pos33MinerMsg, tx *types.Transaction, receiptData *types.ReceiptData, index int) (*types.LocalDBSet, error) {
	tlog.Info("ExecDelLocal_Miner", "height", t.GetHeight())
	return t.execDelLocal(receiptData)
}

// ExecDelLocal_Bind exec del local miner
func (t *Pos33Ticket) ExecDelLocal_Bind(payload *ty.Pos33TicketBind, tx *types.Transaction, receiptData *types.ReceiptData, index int) (*types.LocalDBSet, error) {
	tlog.Info("ExecDelLocal_Miner", "height", t.GetHeight())
	return t.execDelLocal(receiptData)
}
