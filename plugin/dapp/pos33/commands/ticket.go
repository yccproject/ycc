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
	"github.com/33cn/chain33/common/crypto"
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
		GetPos33Info(),
		GetConsigneeCmd(),
		GetConsignorCmd(),
		SetEntrustCmd(),
		SetFeeRateCmd(),
		WithdrawRewardCmd(),
		BlsBind(),
		BlsAddr(),
		// Migrate(),
	)

	return cmd
}

func BlsAddr() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bls",
		Short: "get bls address with privakey",
		Run:   blsAddr,
	}
	addBlsAddrFlags(cmd)
	return cmd
}
func addBlsAddrFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("key", "s", "", "private key")
	cmd.MarkFlagRequired("key")
}

func HexToPrivkey(key string) crypto.PrivKey {
	cr, err := crypto.Load(types.GetSignName("", types.SECP256K1), -1)
	if err != nil {
		panic(err)
	}
	bkey, err := common.FromHex(key)
	if err != nil {
		panic(err)
	}
	priv, err := cr.PrivKeyFromBytes(bkey)
	if err != nil {
		panic(err)
	}
	return priv
}

func blsAddr(cmd *cobra.Command, args []string) {
	strPriv, _ := cmd.Flags().GetString("key")
	priv := HexToPrivkey(strPriv)
	blsPk := ty.Hash2BlsSk(crypto.Sha256(priv.Bytes())).PubKey()
	blsaddr := address.PubKeyToAddr(2, blsPk.Bytes())
	fmt.Println(blsaddr)
}

func GetPos33Info() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info",
		Short: "get pos33 info : ticket price and all ticket count",
		Run:   getPos33Info,
	}
	return cmd
}

type Pos33Info struct {
	DappAddress    string
	TicketPrice    string
	AllTicketCount int64
}

func parsePos33Info(arg ...interface{}) (interface{}, error) {
	res := arg[0].(*ty.ReplyPos33Info)
	cfg := arg[1].(*rpctypes.ChainConfigInfo)
	result := new(Pos33Info)
	result.TicketPrice = types.FormatAmount2FloatDisplay(res.Price, cfg.CoinPrecision, false)
	result.AllTicketCount = res.AllCount
	dappAddr, err := address.GetExecAddress(ty.Pos33TicketX, 2)
	if err != nil {
		return nil, err
	}
	result.DappAddress = dappAddr
	return result, nil
}

func getPos33Info(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")

	var res ty.ReplyPos33Info
	ctx := jsonclient.NewRPCCtx(rpcLaddr, "pos33.GetPos33Info", &types.ReqNil{}, &res)
	ctx.SetResultCbExt(parsePos33Info)
	cfg, err := cmdtypes.GetChainConfig(rpcLaddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	ctx.RunExt(cfg)
}

func SetFeeRateCmd() *cobra.Command {
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

func setMinerFeeRate(cmd *cobra.Command, args []string) {
	rpcLaddr, _ := cmd.Flags().GetString("rpc_laddr")
	fee, _ := cmd.Flags().GetInt32("fee")

	fr := &ty.Pos33MinerFeeRate{FeeRatePersent: fee}
	rpcCall(rpcLaddr, "pos33.SetMinerFeeRate", fr)
}

func BlsBind() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blsbind",
		Short: "bls bind opration",
		Run:   blsBind,
	}
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
