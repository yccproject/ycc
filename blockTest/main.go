package main

import (
	"flag"
	"log"

	"github.com/33cn/chain33/common/address"
	jrpc "github.com/33cn/chain33/rpc/jsonclient"
	rpctypes "github.com/33cn/chain33/rpc/types"
	"github.com/33cn/chain33/types"

	pt "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

var rpcURL = flag.String("u", "http://localhost:9901", "rpc url")
var height = flag.Int64("h", 3000000, "block height")

var jClient *jrpc.JSONClient

func main() {
	flag.Parse()

	jclient, err := jrpc.NewJSONClient(*rpcURL)
	if err != nil {
		log.Fatal(err)
	}
	jClient = jclient

	mp, _ := getRewardMap(*height)
	for addr, r := range mp {
		log.Println(addr, ":", r.TotolValue, r.Count, r.IsMiner)
	}
}

type rewardT struct {
	TotolValue int64
	Count      int
	IsMiner    bool
}

func getBlockByRpc(height int64) (*rpctypes.Block, error) {
	var bd rpctypes.BlockDetails
	err := jClient.Call("Chain33.GetBlocks", &rpctypes.BlockParam{Start: height, End: height}, &bd)
	if err != nil {
		panic(err)
	}
	return bd.Items[0].Block, nil
}

func getRewardMap(height int64) (map[string]*rewardT, error) {
	b, err := getBlockByRpc(height)
	if err != nil {
		panic(err)
	}
	if len(b.Txs) == 0 {
		panic("No tx in the block")
	}
	// 解析第一个交易
	tx := b.Txs[0]
	var pact pt.Pos33TicketAction
	err = types.JSONToPBUTF8(tx.Payload, &pact)
	if err != nil {
		panic(err)
	}
	ma := pact.GetMiner()

	// 遍历投票
	mp := make(map[string]*rewardT)
	for _, v := range ma.Vs {
		addr := address.PubKeyToAddr(address.DefaultID, v.Sig.Pubkey)
		r, ok := mp[addr]
		if !ok {
			r = &rewardT{}
			mp[addr] = r
		}
		// 投票奖励
		r.TotolValue += pt.Pos33VoteReward
		r.Count += 1
	}

	// 解析出块证明
	nv := len(ma.Vs)
	minerAddr := address.PubKeyToAddr(address.DefaultID, ma.Sort.Proof.Pubkey)
	r, ok := mp[minerAddr]
	if !ok {
		r = &rewardT{}
		mp[minerAddr] = r
	}
	// 挖矿奖励
	r.TotolValue += pt.Pos33MakerReward * int64(nv)
	r.IsMiner = true
	return mp, nil
}
