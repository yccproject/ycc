// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"github.com/33cn/chain33/common/address"
	"github.com/33cn/chain33/types"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

// On_SetMinerFeeRate set miner fee rate
func (policy *ticketPolicy) On_SetMinerFeeRate(req *ty.Pos33MinerFeeRate) (types.Message, error) {
	operater := policy.getWalletOperate()
	bizlog.Info("On_SetMinerFeeRate", "maddr", req.MinerAddr, "fee rate persent", req.FeeRatePersent)
	priv, maddr, err := policy.getMiner("")
	if err != nil {
		return nil, err
	}
	if req.MinerAddr == "" {
		req.MinerAddr = maddr
	}

	reply, err := policy.setMinerFeeRate(priv, req)
	if err != nil || reply == nil {
		bizlog.Error("onClosePos33Tickets", "forceClosePos33Ticket error", err.Error())
	} else {
		go func() {
			if len(reply.Hashes) > 0 {
				operater.WaitTxs(reply.Hashes)
			}
		}()
	}
	return reply, err
}

// On_ClosePos33Tickets close ticket
func (policy *ticketPolicy) On_ClosePos33Tickets(req *ty.Pos33TicketClose) (types.Message, error) {
	operater := policy.getWalletOperate()
	bizlog.Info("On_ClosePos33Tickets", "maddr", req.MinerAddress, "count", req.Count)
	priv, maddr, err := policy.getMiner("")
	if err != nil {
		return nil, err
	}
	if req.MinerAddress != "" {
		maddr = req.MinerAddress
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
	addr := address.PubKeyToAddr(address.DefaultID, priv.PubKey().Bytes())
	api := policy.getAPI()
	height := policy.walletOperate.GetLastHeader().Height

	count := int64(0)
	chain33Cfg := api.GetConfig()
	if chain33Cfg.IsDappFork(height, ty.Pos33TicketX, "UseEntrust") {
		msg, err := api.Query(ty.Pos33TicketX, "Pos33ConsigneeEntruct", &types.ReqAddr{Addr: addr})
		if err != nil {
			bizlog.Error("getPos33Tickets", "addr", addr, "Query error", err)
			return nil, err
		}
		count = msg.(*ty.Pos33Consignee).Amount
	} else {
		msg, err := api.Query(ty.Pos33TicketX, "Pos33TicketCount", &types.ReqAddr{Addr: addr})
		if err != nil {
			bizlog.Error("getPos33Tickets", "addr", addr, "Query error", err)
			return nil, err
		}
		count = msg.(*types.Int64).Data
	}
	tks := &ty.ReplyWalletPos33Count{Count: count, Privkey: priv.Bytes()}
	return tks, err
}

// On_WalletAutoMiner auto mine
func (policy *ticketPolicy) On_WalletAutoMiner(req *ty.Pos33MinerFlag) (types.Message, error) {
	policy.store.SetAutoMinerFlag(req.Flag)
	policy.setAutoMining(req.Flag)
	FlushPos33Ticket(policy.getAPI())
	return &types.Reply{IsOk: true}, nil
}
