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

// GetAllPos33TicketAmount get count
func (g *channelClient) getAllPos33TicketAmount(ctx context.Context, in *types.ReqNil) (*types.Int64, error) {
	msg, err := g.Query(ty.Pos33TicketX, "AllPos33TicketAmount", in)
	if err != nil {
		return nil, err
	}
	return msg.(*types.Int64), nil
}

// // GetAllPos33TicketAmount get count
// func (j *Jrpc) GetAllPos33TicketAmount(in *types.ReqNil) (*types.Int64, error) {
// 	a, err := j.cli.GetAllPos33TicketAmount(context.Background(), in)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return a, nil
// }

// GetPos33Info get pos33 ticket price and all ticket count
func (c *Jrpc) GetPos33Info(in *types.ReqNil, result *interface{}) error {
	resp, err := c.cli.GetPos33Info(context.Background(), in)
	if err != nil {
		return err
	}
	*result = resp
	return nil
}

// GetPos33Info get pos33 ticket price and all ticket count
func (g *channelClient) GetPos33Info(ctx context.Context, in *types.ReqNil) (*ty.ReplyPos33Info, error) {
	header, err := g.GetLastHeader()
	if err != nil {
		return nil, err
	}
	height := header.Height
	cfg33 := g.GetConfig()
	mp := ty.GetPos33MineParam(cfg33, height)
	tprice := mp.GetTicketPrice()

	useEntrust := cfg33.IsDappFork(height, ty.Pos33TicketX, "UseEntrust")
	if useEntrust {
		amount, err := g.getAllPos33TicketAmount(ctx, in)
		if err != nil {
			return nil, err
		}
		count := amount.Data / tprice
		return &ty.ReplyPos33Info{Price: tprice, AllCount: count}, nil
	}

	msg, err := g.Query(ty.Pos33TicketX, "AllPos33TicketCount", in)
	if err != nil {
		return nil, err
	}
	return &ty.ReplyPos33Info{AllCount: msg.(*types.Int64).Data, Price: tprice}, nil
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

// GetPos33TicketCount get count
func (g *channelClient) GetPos33TicketCount(ctx context.Context, in *types.ReqAddr) (*types.Int64, error) {
	msg, err := g.Query(ty.Pos33TicketX, "Pos33TicketCount", in)
	if err != nil {
		return nil, err
	}
	return msg.(*types.Int64), nil
}

// BlsBind
func (c *Jrpc) Pos33Migrate(in *types.ReqNil, result *interface{}) error {
	resp, err := c.cli.Migrate(context.Background(), in)
	if err != nil {
		return err
	}
	*result = &rpctypes.ReplyHash{Hash: common.ToHex(resp.Hash)}
	return nil
}

// BlsBind
func (g *channelClient) Migrate(ctx context.Context, in *types.ReqNil) (*types.ReplyHash, error) {
	data, err := g.ExecWalletFunc(ty.Pos33TicketX, "Migrate", in)
	if err != nil {
		return nil, err
	}
	return data.(*types.ReplyHash), nil
}

// BlsBind
func (c *Jrpc) BlsBind(in *types.ReqNil, result *interface{}) error {
	resp, err := c.cli.BlsBind(context.Background(), in)
	if err != nil {
		return err
	}
	*result = &rpctypes.ReplyHash{Hash: common.ToHex(resp.Hash)}
	return nil
}

// BlsBind
func (g *channelClient) BlsBind(ctx context.Context, in *types.ReqNil) (*types.ReplyHash, error) {
	data, err := g.ExecWalletFunc(ty.Pos33TicketX, "BlsBind", in)
	if err != nil {
		return nil, err
	}
	return data.(*types.ReplyHash), nil
}

// SetMinerFeeRate
func (c *Jrpc) SetMinerFeeRate(in *ty.Pos33MinerFeeRate, result *interface{}) error {
	resp, err := c.cli.SetMinerFeeRate(context.Background(), in)
	if err != nil {
		return err
	}
	*result = &rpctypes.ReplyHash{Hash: common.ToHex(resp.Hash)}
	return nil
}

// SetMinerFeeRate
func (g *channelClient) SetMinerFeeRate(ctx context.Context, in *ty.Pos33MinerFeeRate) (*types.ReplyHash, error) {
	data, err := g.ExecWalletFunc(ty.Pos33TicketX, "SetMinerFeeRate", in)
	if err != nil {
		return nil, err
	}
	return data.(*types.ReplyHash), nil
}

// query consignor entrust info
func (g *channelClient) GetPos33ConsignorEntrust(ctx context.Context, in *types.ReqAddr) (*ty.Pos33Consignor, error) {
	msg, err := g.Query(ty.Pos33TicketX, "Pos33ConsignorEntrust", in)
	if err != nil {
		return nil, err
	}
	return msg.(*ty.Pos33Consignor), nil
}

// GetPos33ConsignorEntrust get ticket list info
func (c *Jrpc) GetPos33ConsignorEntrust(in *types.ReqAddr, result *interface{}) error {
	resp, err := c.cli.GetPos33ConsignorEntrust(context.Background(), in)
	if err != nil {
		return err
	}
	*result = resp
	return nil
}

// GetPos33ConsigneeEntrust get ticket list info
func (c *Jrpc) GetPos33ConsigneeEntrust(in *types.ReqAddr, result *interface{}) error {
	resp, err := c.cli.GetPos33ConsigneeEntrust(context.Background(), in)
	if err != nil {
		return err
	}
	*result = resp
	return nil
}

// query consignee entrust info
func (g *channelClient) GetPos33ConsigneeEntrust(ctx context.Context, in *types.ReqAddr) (*ty.Pos33Consignee, error) {
	msg, err := g.Query(ty.Pos33TicketX, "Pos33ConsigneeEntrust", in)
	if err != nil {
		return nil, err
	}
	return msg.(*ty.Pos33Consignee), nil
}

// SetEntrust create entrust
func (g *channelClient) SetPos33Entrust(ctx context.Context, in *ty.Pos33Entrust) (*ty.ReplyTxHex, error) {
	cfg := g.GetConfig()
	data, err := types.CallCreateTx(cfg, cfg.ExecName(ty.Pos33TicketX), "entrust", in)
	if err != nil {
		return nil, err
	}

	hex := common.ToHex(data)
	return &ty.ReplyTxHex{TxHex: hex}, nil
}

// SetEntrust create entrust
func (c *Jrpc) SetPos33Entrust(in *ty.Pos33Entrust, result *interface{}) error {
	r, err := c.cli.SetPos33Entrust(context.Background(), in)
	if err != nil {
		return err
	}
	*result = r
	return nil
}
