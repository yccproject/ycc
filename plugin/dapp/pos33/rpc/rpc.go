// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"github.com/33cn/chain33/common"
	"github.com/33cn/chain33/common/address"
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

// GetAllPos33TicketCount get count
func (g *channelClient) GetAllPos33TicketCount(ctx context.Context, in *types.ReqNil) (*types.Int64, error) {
	msg, err := g.Query(ty.Pos33TicketX, "AllPos33TicketCount", in)
	if err != nil {
		return nil, err
	}
	return msg.(*types.Int64), nil
}

// GetPos33TicketCount get count
func (g *channelClient) GetPos33TicketCount(ctx context.Context, in *types.ReqAddr) (*types.Int64, error) {
	msg, err := g.Query(ty.Pos33TicketX, "Pos33TicketCount", in)
	if err != nil {
		return nil, err
	}
	return msg.(*types.Int64), nil
}

// ClosePos33Tickets close ticket
func (g *channelClient) ClosePos33Tickets(ctx context.Context, in *ty.Pos33TicketClose) (*types.ReplyHashes, error) {
	// inn := *in
	data, err := g.ExecWalletFunc(ty.Pos33TicketX, "ClosePos33Tickets", in)
	if err != nil {
		return nil, err
	}
	return data.(*types.ReplyHashes), nil
}

// GetAllPos33TicketCount get ticket count
func (c *Jrpc) GetAllPos33TicketCount(in *types.ReqNil, result *int64) error {
	resp, err := c.cli.GetAllPos33TicketCount(context.Background(), in)
	if err != nil {
		return err
	}
	*result = resp.GetData()
	return nil

}

// GetPos33TicketCount get ticket count
func (c *Jrpc) GetPos33TicketCount(in *types.ReqAddr, result *int64) error {
	resp, err := c.cli.GetPos33TicketCount(context.Background(), in)
	if err != nil {
		return err
	}
	*result = resp.GetData()
	return nil

}

// CreateBindMiner create bind miner
func (c *Jrpc) CreateBindMiner(in *ty.ReqBindPos33Miner, result *interface{}) error {
	reply, err := c.cli.CreateBindMiner(context.Background(), in)
	if err != nil {
		return err
	}
	*result = reply
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

func bindMiner(cfg *types.Chain33Config, param *ty.ReqBindPos33Miner) (*ty.ReplyBindPos33Miner, error) {
	tBind := &ty.Pos33TicketBind{
		MinerAddress:  param.BindAddr,
		ReturnAddress: param.OriginAddr,
	}
	data, err := types.CallCreateTx(cfg, cfg.ExecName(ty.Pos33TicketX), "Tbind", tBind)
	if err != nil {
		return nil, err
	}
	hex := common.ToHex(data)
	return &ty.ReplyBindPos33Miner{TxHex: hex}, nil
}

// CreateBindMiner 创建绑定挖矿
func (g *channelClient) CreateBindMiner(ctx context.Context, in *ty.ReqBindPos33Miner) (*ty.ReplyBindPos33Miner, error) {
	if in.BindAddr != "" {
		err := address.CheckAddress(in.BindAddr, -1)
		if err != nil {
			return nil, err
		}
	}
	err := address.CheckAddress(in.OriginAddr, -1)
	if err != nil {
		return nil, err
	}

	cfg := g.GetConfig()
	if in.CheckBalance {
		header, err := g.GetLastHeader()
		if err != nil {
			return nil, err
		}
		price := ty.GetPos33TicketMinerParam(cfg, header.Height).Pos33TicketPrice
		if price == 0 {
			return nil, types.ErrInvalidParam
		}
		if in.Amount%ty.GetPos33TicketMinerParam(cfg, header.Height).Pos33TicketPrice != 0 || in.Amount < 0 {
			return nil, types.ErrAmount
		}

		getBalance := &types.ReqBalance{Addresses: []string{in.OriginAddr}, Execer: cfg.GetCoinExec(), AssetSymbol: "ycc", AssetExec: cfg.GetCoinExec()}
		balances, err := g.GetCoinsAccountDB().GetBalance(g, getBalance)
		if err != nil {
			return nil, err
		}
		if len(balances) == 0 {
			return nil, types.ErrInvalidParam
		}
		if balances[0].Balance < in.Amount+2*cfg.GetCoinPrecision() {
			return nil, types.ErrNoBalance
		}
	}
	return bindMiner(cfg, in)
}
