// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package executor

import (
	"github.com/33cn/chain33/types"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

func (t *Pos33Ticket) execLocal(receiptData *types.ReceiptData) (*types.LocalDBSet, error) {
	dbSet := &types.LocalDBSet{}
	return dbSet, nil
}

// ExecLocal_Genesis exec local genesis
func (t *Pos33Ticket) ExecLocal_Genesis(payload *ty.Pos33TicketGenesis, tx *types.Transaction, receiptData *types.ReceiptData, index int) (*types.LocalDBSet, error) {
	tlog.Debug("ExecLocal_Genesis", "height", t.GetHeight())
	return t.execLocal(receiptData)
}

// ExecLocal_Topen exec local open
func (t *Pos33Ticket) ExecLocal_Topen(payload *ty.Pos33TicketOpen, tx *types.Transaction, receiptData *types.ReceiptData, index int) (*types.LocalDBSet, error) {
	tlog.Debug("ExecLocal_Topen", "height", t.GetHeight())
	return t.execLocal(receiptData)
}

// ExecLocal_Tclose exec local close
func (t *Pos33Ticket) ExecLocal_Tclose(payload *ty.Pos33TicketClose, tx *types.Transaction, receiptData *types.ReceiptData, index int) (*types.LocalDBSet, error) {
	tlog.Debug("ExecLocal_Tclose", "height", t.GetHeight())
	return t.execLocal(receiptData)
}

// ExecLocal_Miner exec local miner
func (t *Pos33Ticket) ExecLocal_Miner(payload *ty.Pos33MinerMsg, tx *types.Transaction, receiptData *types.ReceiptData, index int) (*types.LocalDBSet, error) {
	tlog.Debug("ExecLocal_Miner", "height", t.GetHeight())
	dbSet, err := t.execLocal(receiptData)
	if err != nil {
		return nil, err
	}
	return dbSet, nil
}

// ExecLocal_Miner exec local miner
func (t *Pos33Ticket) ExecLocal_Bind(payload *ty.Pos33TicketBind, tx *types.Transaction, receiptData *types.ReceiptData, index int) (*types.LocalDBSet, error) {
	tlog.Debug("ExecLocal_Bind", "height", t.GetHeight())
	dbSet, err := t.execLocal(receiptData)
	if err != nil {
		return nil, err
	}
	return dbSet, nil
}
