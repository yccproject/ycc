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
		GetDepositCmd(),
		GetConsigneeCmd(),
		GetConsignorCmd(),
		SetEntrustCmd(),
		SetMinerFeeRateCmd(),
		WithdrawRewardCmd(),
		BlsBind(),
		Migrate(),
	)

	return cmd
}

func BlsBind() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blsbind",
		Short: "bls bind opration",
		Run:   blsBind,
	}
	// addSetEntrustFlags(cmd)
	return cmd
}

func blsBind(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	rpcCall(rpcLaddr, "pos33.BlsBind", &types.ReqNil{})
}

func Migrate() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate ",
		Short: "migrate opration",
		Run:   migrate,
	}
	// addSetEntrustFlags(cmd)
	return cmd
}

func migrate(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	rpcCall(rpcLaddr, "pos33.Pos33Migrate", &types.ReqNil{})
}

func SetEntrustCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entrust",
		Short: "set entrust opration",
		Run:   setEntrust,
	}
	addSetEntrustFlags(cmd)
	return cmd
}

func addSetEntrustFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("consignor", "r", "", "address for consignor")
	cmd.Flags().StringP("consignee", "e", "", "address for consignee")
	cmd.Flags().IntP("amount", "a", 1000, "amount of entrust, if < 0, unentrust")
	cmd.MarkFlagRequired("consignor")
	cmd.MarkFlagRequired("consignee")
	cmd.MarkFlagRequired("amount")
}

func setEntrust(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	paraName, _ := cmd.Flags().GetString("paraName")

	cfg, err := cmdtypes.GetChainConfig(rpcLaddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, errors.Wrapf(err, "GetChainConfig"))
		return
	}

	consignor, _ := cmd.Flags().GetString("consignor")
	consignee, _ := cmd.Flags().GetString("consignee")
	amount, _ := cmd.Flags().GetInt("amount")
	realAmount := cfg.CoinPrecision * int64(amount)

	entrust := &ty.Pos33Entrust{
		Consignor: consignor,
		Consignee: consignee,
		Amount:    realAmount,
	}
	act := &ty.Pos33TicketAction{
		Ty:    ty.Pos33ActionEntrust,
		Value: &ty.Pos33TicketAction_Entrust{Entrust: entrust},
	}

	rawTx := &types.Transaction{Payload: types.Encode(act)}
	tx, err := types.FormatTxExt(cfg.ChainID, len(paraName) > 0, cfg.MinTxFeeRate, ty.Pos33TicketX, rawTx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	txHex := types.Encode(tx)
	fmt.Println(hex.EncodeToString(txHex))
}

func addGetConsignorFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("addr", "a", "", "address for deposit")
	cmd.MarkFlagRequired("addr")
}

func GetConsigneeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "consignee",
		Short: "get consignee info",
		Run:   getConsignee,
	}
	addGetConsigneeFlags(cmd)
	return cmd
}

func getConsignee(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	addr, _ := cmd.Flags().GetString("addr")

	var res ty.Pos33Consignee
	ctx := jsonclient.NewRPCCtx(rpcLaddr, "pos33.GetPos33ConsigneeEntrust", &types.ReqAddr{Addr: addr}, &res)
	ctx.SetResultCbExt(parseConsigneeRes)
	cfg, err := cmdtypes.GetChainConfig(rpcLaddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	ctx.RunExt(cfg)
}

type Pos33Consignor struct {
	Address string
	Miners  []*Consignee
}

type Consignee struct {
	Address string
	Amount  string
}

type Consignor struct {
	Address      string
	Amount       string
	Reward       string
	RemainReward string
}

type Pos33Consignee struct {
	Address    string
	Amount     string
	FeeReward  string
	FeePensent string
	Consignors []*Consignor
}

func parseConsigneeRes(arg ...interface{}) (interface{}, error) {
	res := arg[0].(*ty.Pos33Consignee)
	cfg := arg[1].(*rpctypes.ChainConfigInfo)
	result := new(Pos33Consignee)
	result.Address = res.Address
	result.FeeReward = types.FormatAmount2FloatDisplay(res.FeeReward, cfg.CoinPrecision, false)
	result.Amount = types.FormatAmount2FloatDisplay(res.Amount, cfg.CoinPrecision, false)
	result.FeePensent = fmt.Sprintf("%d%%", res.FeePersent)
	for _, co := range res.Consignors {
		sco := &Consignor{
			Address:      co.Address,
			Amount:       types.FormatAmount2FloatDisplay(co.Amount, cfg.CoinPrecision, false),
			Reward:       types.FormatAmount2FloatDisplay(co.Reward, cfg.CoinPrecision, false),
			RemainReward: types.FormatAmount2FloatDisplay(co.RemainReward, cfg.CoinPrecision, false),
		}
		result.Consignors = append(result.Consignors, sco)
	}
	return result, nil
}

func parseConsignorRes(arg ...interface{}) (interface{}, error) {
	res := arg[0].(*ty.Pos33Consignor)
	cfg := arg[1].(*rpctypes.ChainConfigInfo)
	result := new(Pos33Consignor)
	result.Address = res.Address
	for _, co := range res.Consignees {
		sco := &Consignee{
			Address: co.Address,
			Amount:  types.FormatAmount2FloatDisplay(co.Amount, cfg.CoinPrecision, false),
		}
		result.Miners = append(result.Miners, sco)
	}
	return result, nil
}

func getConsignor(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	addr, _ := cmd.Flags().GetString("addr")

	var res ty.Pos33Consignor
	ctx := jsonclient.NewRPCCtx(rpcLaddr, "pos33.GetPos33ConsignorEntrust", &types.ReqAddr{Addr: addr}, &res)
	ctx.SetResultCbExt(parseConsignorRes)
	cfg, err := cmdtypes.GetChainConfig(rpcLaddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	ctx.RunExt(cfg)
}

func addGetConsigneeFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("addr", "a", "", "address for deposit")
	cmd.MarkFlagRequired("addr")
}

func GetConsignorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "consignor",
		Short: "get consignor info",
		Run:   getConsignor,
	}
	addGetConsignorFlags(cmd)
	return cmd
}

func GetDepositCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deposit",
		Short: "get deposit info",
		Run:   getDeposit,
	}
	addGetDepositFlags(cmd)
	return cmd
}

func getDeposit(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	addr, _ := cmd.Flags().GetString("addr")

	var res ty.Pos33DepositMsg
	ctx := jsonclient.NewRPCCtx(rpcLaddr, "pos33.GetPos33Deposit", &types.ReqAddr{Addr: addr}, &res)
	ctx.Run()
}

func addGetDepositFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("addr", "a", "", "address for deposit")
	cmd.MarkFlagRequired("addr")
}

// WithdrawRewardCmd withdraw reward
func WithdrawRewardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw",
		Short: "withdraw reward",
		Run:   withdrawReward,
	}
	addWithdrawRewardFlags(cmd)
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

func addWithdrawRewardFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("consignor", "o", "", "consignor address")
	cmd.MarkFlagRequired("consignor")

	cmd.Flags().StringP("consignee", "e", "", "consignee address")
	cmd.MarkFlagRequired("origin_addr")

	cmd.Flags().IntP("amount", "a", 10, "amount of withdraw reward")
	cmd.MarkFlagRequired("amount")
}

func withdrawReward(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	consignorAddr, _ := cmd.Flags().GetString("consignor")
	consigneeAddr, _ := cmd.Flags().GetString("consignee")
	amount, _ := cmd.Flags().GetInt("amount")

	cfg, err := cmdtypes.GetChainConfig(rpcLaddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, errors.Wrapf(err, "GetChainConfig"))
		return
	}
	realAmount := cfg.CoinPrecision * int64(amount)
	ta := &ty.Pos33TicketAction{}
	tBind := &ty.Pos33WithdrawReward{
		Consignee: consigneeAddr,
		Consignor: consignorAddr,
		Amount:    realAmount,
	}
	ta.Value = &ty.Pos33TicketAction_Withdraw{Withdraw: tBind}
	ta.Ty = ty.Pos33ActionWithdrawReward

	rawTx := &types.Transaction{Payload: types.Encode(ta)}
	tx, err := types.FormatTxExt(cfg.ChainID, false, cfg.MinTxFeeRate, ty.Pos33TicketX, rawTx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	txHex := types.Encode(tx)
	fmt.Println(hex.EncodeToString(txHex))
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
	fmt.Println(address.PubKeyToAddr(address.DefaultID, blsSk.PubKey().Bytes()))
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

	addPos33TicketCountFlags(cmd)
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

func addPos33TicketCountFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("addr", "a", "", `who address`)
	cmd.MarkFlagRequired("addr")
}

func countPos33Ticket(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	addr, _ := cmd.Flags().GetString("addr")
	var res int64
	ctx := jsonclient.NewRPCCtx(rpcLaddr, "pos33.GetPos33TicketCount", &types.ReqAddr{Addr: addr}, &res)
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

// SetMinerFeeCmd  set miner entrust fee
func SetMinerFeeRateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fee",
		Short: "set miner fee",
		Run:   setMinerFeeRate,
	}
	addSetMinerFeeRateFlags(cmd)
	return cmd
}

func addSetMinerFeeRateFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("addr", "a", "", "address for miner")
	cmd.Flags().Int32P("fee", "f", 10, "miner entrust mine fee rate persent (default 10% if nil)")
	cmd.MarkFlagRequired("fee")
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
	cmd.Flags().StringP("addr", "a", "", "address for close")

	cmd.Flags().Int32P("count", "c", 0, "close ticket count (default 0 is all)")
	cmd.MarkFlagRequired("count")

	// cmd.Flags().BoolP("miner", "m", true, "if addr is miner")
}

func setMinerFeeRate(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	miner, _ := cmd.Flags().GetString("addr")
	fee, _ := cmd.Flags().GetInt32("fee")

	fr := &ty.Pos33MinerFeeRate{MinerAddr: miner, FeeRatePersent: fee}
	rpcCall(rpcLaddr, "pos33.SetMinerFeeRate", fr)
}

func closePos33Ticket(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	bindAddr, _ := cmd.Flags().GetString("addr")
	count, _ := cmd.Flags().GetInt32("count")
	// isMiner, _ := cmd.Flags().GetBool("miner")

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
	rpcCall(rpcLaddr, "pos33.ClosePos33Tickets", tClose)
}

func rpcCall(rpcLaddr, method string, param interface{}) {
	rpc, err := jsonclient.NewJSONClient(rpcLaddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	res := new(rpctypes.ReplyHash)
	err = rpc.Call(method, param, res)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	data, err := json.MarshalIndent(&res, "", "    ")
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
