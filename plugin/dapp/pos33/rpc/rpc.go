// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"github.com/33cn/chain33/common"
	rpctypes "github.com/33cn/chain33/rpc/types"
	"github.com/33cn/chain33/types"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
	"golang.org/x/net/context"
)

// SetAutoMining set auto mining
func (g *channelClient) SetAutoMining(ctx context.Context, in *ty.Pos33MinerFlag) (*types.Reply, error) {
	data, err := g.ExecWalletFunc(ty.Pos33TicketX, "WalletAutoMiner", in)
	if err != nil {
		return nil, err
	}
	return data.(*types.Reply), nil
}

// GetPos33TicketCount get count
func (g *channelClient) GetPos33TicketCount(ctx context.Context, in *types.ReqNil) (*types.Int64, error) {
	data, err := g.QueryConsensusFunc(ty.Pos33TicketX, "GetPos33TicketCount", &types.ReqNil{})
	if err != nil {
		return nil, err
	}
	return data.(*types.Int64), nil
}

// ClosePos33Tickets close ticket
func (g *channelClient) ClosePos33Tickets(ctx context.Context, in *ty.Pos33TicketClose) (*types.ReplyHashes, error) {
	inn := *in
	data, err := g.ExecWalletFunc(ty.Pos33TicketX, "ClosePos33Tickets", &inn)
	if err != nil {
		return nil, err
	}
	return data.(*types.ReplyHashes), nil
}

// GetPos33TicketCount get ticket count
func (c *Jrpc) GetPos33TicketCount(in *types.ReqNil, result *int64) error {
	resp, err := c.cli.GetPos33TicketCount(context.Background(), &types.ReqNil{})
	if err != nil {
		return err
	}
	*result = resp.GetData()
	return nil

}

// ClosePos33Tickets close ticket
func (c *Jrpc) ClosePos33Tickets(in *ty.Pos33TicketClose, result *interface{}) error {
	resp, err := c.cli.ClosePos33Tickets(context.Background(), in)
	if err != nil {
		return err
	}
	var hashes rpctypes.ReplyHashes
	for _, has := range resp.Hashes {
		hashes.Hashes = append(hashes.Hashes, common.ToHex(has))
	}
	*result = &hashes
	return nil
}

// SetAutoMining set auto mining
func (c *Jrpc) SetAutoMining(in *ty.Pos33MinerFlag, result *rpctypes.Reply) error {
	resp, err := c.cli.SetAutoMining(context.Background(), in)
	if err != nil {
		return err
	}
	var reply rpctypes.Reply
	reply.IsOk = resp.GetIsOk()
	reply.Msg = string(resp.GetMsg())
	*result = reply
	return nil
}

// GetPos33Deposit get ticket list info
func (g *channelClient) GetPos33Deposit(ctx context.Context, in *types.ReqAddr) (*ty.Pos33DepositMsg, error) {
	data, err := g.Query(ty.Pos33TicketX, "Pos33Deposit", in)
	if err != nil {
		return nil, err
	}

	return data.(*ty.Pos33DepositMsg), nil
}

// GetPos33TicketList get ticket list info
func (c *Jrpc) GetPos33Deposit(in *types.ReqAddr, result *interface{}) error {
	resp, err := c.cli.GetPos33Deposit(context.Background(), in)
	if err != nil {
		return err
	}
	*result = resp
	return nil
}

// GetPos33TicketList get ticket list info
func (g *channelClient) GetPos33TicketReward(ctx context.Context, in *ty.Pos33TicketReward) (*ty.ReplyPos33TicketReward, error) {
	data, err := g.QueryConsensusFunc(ty.Pos33TicketX, "GetPos33Reward", in)
	if err != nil {
		return nil, err
	}
	return data.(*ty.ReplyPos33TicketReward), nil
}

// GetPos33TicketList get ticket list info
func (c *Jrpc) GetPos33TicketReward(in *ty.Pos33TicketReward, result *interface{}) error {
	resp, err := c.cli.GetPos33TicketReward(context.Background(), in)
	if err != nil {
		return err
	}
	*result = resp
	return nil
}
