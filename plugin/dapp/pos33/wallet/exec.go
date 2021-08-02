// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"github.com/33cn/chain33/common/address"
	"github.com/33cn/chain33/types"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

// On_ClosePos33Tickets close ticket
func (policy *ticketPolicy) On_ClosePos33Tickets(req *ty.Pos33TicketClose) (types.Message, error) {
	operater := policy.getWalletOperate()
	bizlog.Info("On_ClosePos33Tickets", "maddr", req.MinerAddress, "count", req.Count)
	priv, maddr, err := policy.getMiner(req.MinerAddress)
	if err != nil {
		return nil, err
	}
	reply, err := policy.closePos33Tickets(priv, maddr, int(req.Count))
	if err != nil || reply == nil {
		bizlog.Error("onClosePos33Tickets", "forceClosePos33Ticket error", err.Error())
	} else {
		go func() {
			if len(reply.Hashes) > 0 {
				operater.WaitTxs(reply.Hashes)
				FlushPos33Ticket(policy.getAPI())
			}
		}()
	}
	return reply, err
}

// On_WalletGetPos33Tickets get ticket
func (policy *ticketPolicy) On_WalletGetPos33Count(req *types.ReqNil) (types.Message, error) {
	priv, _, err := policy.getMiner("")
	if err != nil {
		return nil, err
	}
	addr := address.PubKeyToAddr(priv.PubKey().Bytes())
	api := policy.getAPI()
	msg, err := api.Query(ty.Pos33TicketX, "Pos33TicketCount", &types.ReqAddr{Addr: addr})
	if err != nil {
		bizlog.Error("getPos33Tickets", "addr", addr, "Query error", err)
		return nil, err
	}
	tks := &ty.ReplyWalletPos33Count{Count: (msg.(*types.Int64).Data), Privkey: priv.Bytes()}
	return tks, err
}

// On_WalletAutoMiner auto mine
func (policy *ticketPolicy) On_WalletAutoMiner(req *ty.Pos33MinerFlag) (types.Message, error) {
	policy.store.SetAutoMinerFlag(req.Flag)
	policy.setAutoMining(req.Flag)
	FlushPos33Ticket(policy.getAPI())
	return &types.Reply{IsOk: true}, nil
}
