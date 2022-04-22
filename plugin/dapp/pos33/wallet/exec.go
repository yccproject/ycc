// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"github.com/33cn/chain33/types"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

// On_Migrate
func (policy *ticketPolicy) On_Migrate(req *types.ReqNil) (types.Message, error) {
	operater := policy.getWalletOperate()
	priv, _, err := policy.getMiner("")
	if err != nil {
		return nil, err
	}

	hash, err := policy.migrate(priv)
	if err != nil || hash == nil {
		bizlog.Error("migrate error", "err", err.Error())
	} else {
		go func() {
			if hash != nil {
				operater.WaitTxs([][]byte{hash})
			}
		}()
	}
	return &types.ReplyHash{Hash: hash}, nil
}

// On_BlsBind
func (policy *ticketPolicy) On_BlsBind(req *types.ReqNil) (types.Message, error) {
	operater := policy.getWalletOperate()
	priv, _, err := policy.getMiner("")
	if err != nil {
		return nil, err
	}

	hash, err := policy.blsBind(priv)
	if err != nil || hash == nil {
		bizlog.Error("BlsBind error", "err", err.Error())
	} else {
		go func() {
			if hash != nil {
				operater.WaitTxs([][]byte{hash})
			}
		}()
	}
	return &types.ReplyHash{Hash: hash}, nil
}

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

	hash, err := policy.setMinerFeeRate(priv, req)
	if err != nil || hash == nil {
		bizlog.Error("onClosePos33Tickets", "forceClosePos33Ticket error", err.Error())
	} else {
		go func() {
			if hash != nil {
				operater.WaitTxs([][]byte{hash.Hash})
			}
		}()
	}
	return hash, err
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
			if reply != nil {
				operater.WaitTxs([][]byte{reply.Hash})
				// FlushPos33Ticket(policy.getAPI())
			}
		}()
	}
	return reply, err
}

// On_WalletGetPos33Tickets get ticket
func (policy *ticketPolicy) On_WalletGetMiner(req *types.ReqNil) (types.Message, error) {
	priv, _, err := policy.getMiner("")
	if err != nil {
		return nil, err
	}
	// addr := address.PubKeyToAddr(address.DefaultID, priv.PubKey().Bytes())
	return &types.ReplyString{Data: string(priv.Bytes())}, nil
}

// On_WalletAutoMiner auto mine
// func (policy *ticketPolicy) On_WalletAutoMiner(req *ty.Pos33MinerFlag) (types.Message, error) {
// 	policy.store.SetAutoMinerFlag(req.Flag)
// 	policy.setAutoMining(req.Flag)
// 	FlushPos33Ticket(policy.getAPI())
// 	return &types.Reply{IsOk: true}, nil
// }
