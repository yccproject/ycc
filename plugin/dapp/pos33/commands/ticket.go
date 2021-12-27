// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package commands

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/33cn/chain33/common"
	"github.com/33cn/chain33/common/address"
	"github.com/33cn/chain33/rpc/jsonclient"
	rpctypes "github.com/33cn/chain33/rpc/types"
	cmdtypes "github.com/33cn/chain33/system/dapp/commands/types"
	"github.com/33cn/chain33/types"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

// Pos33TicketCmd ticket command type
func Pos33TicketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pos33",
		Short: "Pos33Ticket management",
		Args:  cobra.MinimumNArgs(1),
	}
	cmd.AddCommand(
		BindMinerCmd(),
		BlsAddrFromPrivKey(),
		AutoMineCmd(),
		CountPos33TicketCmd(),
		ClosePos33TicketCmd(),
	)

	return cmd
}

// BindMinerCmd bind miner
func BindMinerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bind",
		Short: "Bind private key to miner address",
		Run:   bindMiner,
	}
	addBindMinerFlags(cmd)
	return cmd
}

func addBindMinerFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("bind_addr", "b", "", "miner address")
	cmd.MarkFlagRequired("bind_addr")

	cmd.Flags().StringP("origin_addr", "o", "", "origin address")
	cmd.MarkFlagRequired("origin_addr")
}

func bindMiner(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	paraName, _ := cmd.Flags().GetString("paraName")

	cfg, err := cmdtypes.GetChainConfig(rpcLaddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, errors.Wrapf(err, "GetChainConfig"))
		return
	}
	bindAddr, _ := cmd.Flags().GetString("bind_addr")
	originAddr, _ := cmd.Flags().GetString("origin_addr")
	//c, _ := crypto.Load(types.GetSignName(wallet.SignType))
	//a, _ := common.FromHex(key)
	//privKey, _ := c.PrivKeyFromBytes(a)
	//originAddr := account.PubKeyToAddress(privKey.PubKey().Bytes()).String()
	ta := &ty.Pos33TicketAction{}
	tBind := &ty.Pos33TicketBind{
		MinerAddress:  bindAddr,
		ReturnAddress: originAddr,
	}
	ta.Value = &ty.Pos33TicketAction_Tbind{Tbind: tBind}
	ta.Ty = ty.Pos33TicketActionBind

	rawTx := &types.Transaction{Payload: types.Encode(ta)}
	tx, err := types.FormatTxExt(cfg.ChainID, len(paraName) > 0, cfg.MinTxFeeRate, ty.Pos33TicketX, rawTx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	txHex := types.Encode(tx)
	fmt.Println(hex.EncodeToString(txHex))
}

func BlsAddrFromPrivKey() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bls_addr",
		Short: "get blk address from privkey",
		Run:   blsAddrFromPrivKey,
	}
	addBlsAddrFlags(cmd)
	return cmd
}

func blsAddrFromPrivKey(cmd *cobra.Command, args []string) {
	sk, _ := cmd.Flags().GetString("private")
	pb, err := common.FromHex(sk)
	if err != nil {
		panic(err)
	}
	blsSk := ty.Hash2BlsSk(common.Sha256(pb))
	fmt.Println(address.PubKeyToAddr(blsSk.PubKey().Bytes()))
}

func addBlsAddrFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("private", "s", "", `private key`)
	cmd.MarkFlagRequired("private")
}

// AutoMineCmd  set auto mining
func AutoMineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auto_mine",
		Short: "Set auto mine on/off",
		Run:   autoMine,
	}
	addAutoMineFlags(cmd)
	return cmd
}

func addAutoMineFlags(cmd *cobra.Command) {
	cmd.Flags().Int32P("flag", "f", 0, `auto mine(0: off, 1: on)`)
	cmd.MarkFlagRequired("flag")
}

func autoMine(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	flag, _ := cmd.Flags().GetInt32("flag")
	if flag != 0 && flag != 1 {
		err := cmd.UsageFunc()(cmd)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return
	}
	params := struct {
		Flag int32
	}{
		Flag: flag,
	}
	var res rpctypes.Reply
	ctx := jsonclient.NewRPCCtx(rpcLaddr, "pos33.SetAutoMining", params, &res)
	ctx.Run()
}

// CountPos33TicketCmd get ticket count
func CountPos33TicketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "count",
		Short: "Get ticket count",
		Run:   countPos33Ticket,
	}
	return cmd
}

// CountPos33TicketCmd get ticket count
func GetPos33RewardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reward",
		Short: "get reward",
		Run:   getPos33Reward,
	}
	addPos33RewardFlags(cmd)
	return cmd
}

func addPos33RewardFlags(cmd *cobra.Command) {
	cmd.Flags().Int64P("height", "b", 0, `block height`)
	cmd.Flags().StringP("addr", "a", "", `who reward`)
}

func countPos33Ticket(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	var res int64
	ctx := jsonclient.NewRPCCtx(rpcLaddr, "pos33.GetPos33TicketCount", nil, &res)
	ctx.Run()
}

func getPos33Reward(cmd *cobra.Command, args []string) {
	height, _ := cmd.Flags().GetInt64("height")
	addr, _ := cmd.Flags().GetString("addr")
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	var res ty.ReplyPos33TicketReward
	ctx := jsonclient.NewRPCCtx(rpcLaddr, "pos33.GetPos33TicketReward", &ty.Pos33TicketReward{Height: height, Addr: addr}, &res)
	ctx.Run()
}

// ClosePos33TicketCmd close all accessible tickets
func ClosePos33TicketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close tickets",
		Run:   closePos33Ticket,
	}
	addCloseAddr(cmd)
	return cmd
}

func addCloseAddr(cmd *cobra.Command) {
	cmd.Flags().StringP("miner_addr", "m", "", "miner address (optional)")
	cmd.Flags().Int32P("count", "c", 0, "close ticket count (optional, default 0 is all)")
}

func closePos33Ticket(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	bindAddr, _ := cmd.Flags().GetString("miner_addr")
	count, _ := cmd.Flags().GetInt32("count")
	status, err := getWalletStatus(rpcLaddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	isAutoMining := status.(rpctypes.WalletStatus).IsAutoMining
	if isAutoMining {
		fmt.Fprintln(os.Stderr, types.ErrMinerNotClosed)
		return
	}

	tClose := &ty.Pos33TicketClose{
		MinerAddress: bindAddr,
		Count:        count,
	}

	var res rpctypes.ReplyHashes
	rpc, err := jsonclient.NewJSONClient(rpcLaddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	err = rpc.Call("pos33.ClosePos33Tickets", tClose, &res)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	if len(res.Hashes) == 0 {
		fmt.Println("no ticket to be close")
		return
	}

	data, err := json.MarshalIndent(res, "", "    ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	fmt.Println(string(data))
}

func getWalletStatus(rpcAddr string) (interface{}, error) {
	rpc, err := jsonclient.NewJSONClient(rpcAddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, err
	}
	var res rpctypes.WalletStatus
	err = rpc.Call("Chain33.GetWalletStatus", nil, &res)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, err
	}

	return res, nil
}
