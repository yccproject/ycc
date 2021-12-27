// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package executor

import (
	"github.com/33cn/chain33/types"
)

// Query_AllPos33TicketCount query all ticket count
func (ticket *Pos33Ticket) Query_AllPos33TicketCount(*types.ReqNil) (types.Message, error) {
	count := getAllCount(ticket.GetStateDB())
	return &types.Int64{Data: int64(count)}, nil
}

// Query_Pos33TicketCount query tick info
func (ticket *Pos33Ticket) Query_Pos33TicketCount(param *types.ReqAddr) (types.Message, error) {
	count := getCount(ticket.GetStateDB(), param.Addr)
	return &types.Int64{Data: int64(count)}, nil
}

// Query_Pos33BindAddr query tick info
func (ticket *Pos33Ticket) Query_Pos33BindAddr(param *types.ReqAddr) (types.Message, error) {
	val, err := ticket.GetStateDB().Get(BindKey(param.Addr))
	if err != nil {
		return nil, err
	}
	return &types.ReplyString{Data: string(val)}, nil
}

// Query_Pos33BindAddr query tick info
func (ticket *Pos33Ticket) Query_Pos33BlsAddr(param *types.ReqAddr) (types.Message, error) {
	val, err := ticket.GetStateDB().Get(BlsKey(param.Addr))
	if err != nil {
		return nil, err
	}
	return &types.ReplyString{Data: string(val)}, nil
}

// Query_Pos33Deposit query tick info
func (ticket *Pos33Ticket) Query_Pos33Deposit(param *types.ReqAddr) (types.Message, error) {
	d, err := getDeposit(ticket.GetStateDB(), param.Addr)
	if err != nil {
		return nil, err
	}
	return d, nil
}
