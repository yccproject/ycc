// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"testing"

	"github.com/33cn/chain33/client"
	"github.com/33cn/chain33/client/mocks"
	rpctypes "github.com/33cn/chain33/rpc/types"
	"github.com/33cn/chain33/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
	"golang.org/x/net/context"
)

func newGrpc(api client.QueueProtocolAPI) *channelClient {
	return &channelClient{
		ChannelClient: rpctypes.ChannelClient{QueueProtocolAPI: api},
	}
}

func newJrpc(api client.QueueProtocolAPI) *Jrpc {
	return &Jrpc{cli: newGrpc(api)}
}

func TestChannelClient_BindMiner(t *testing.T) {
	cfg := types.NewChain33Config(cfgstring)
	api := new(mocks.QueueProtocolAPI)
	api.On("GetConfig", mock.Anything).Return(cfg, nil)
	client := newGrpc(api)
	client.Init("pos33", nil, nil, nil)
	head := &types.Header{Height: 2, StateHash: []byte("sdfadasds")}
	api.On("GetLastHeader").Return(head, nil).Times(4)

	var acc = &types.Account{Addr: "1Jn2qu84Z1SUUosWjySggBS9pKWdAP3tZt", Balance: 100000 * types.DefaultCoinPrecision}
	accv := types.Encode(acc)
	storevalue := &types.StoreReplyValue{}
	storevalue.Values = append(storevalue.Values, accv)
	api.On("StoreGet", mock.Anything).Return(storevalue, nil).Twice()

	//var addrs = make([]string, 1)
	//addrs = append(addrs, "1Jn2qu84Z1SUUosWjySggBS9pKWdAP3tZt")
	var in = &ty.ReqBindPos33Miner{
		BindAddr:     "1Jn2qu84Z1SUUosWjySggBS9pKWdAP3tZt",
		OriginAddr:   "1Jn2qu84Z1SUUosWjySggBS9pKWdAP3tZt",
		Amount:       10000 * types.DefaultCoinPrecision,
		CheckBalance: false,
	}
	_, err := client.CreateBindMiner(context.Background(), in)
	assert.Nil(t, err)
}

/*

func testGetPos33TicketCountOK(t *testing.T) {
	cfg := types.NewChain33Config(types.GetDefaultCfgstring())
	api := &mocks.QueueProtocolAPI{}
	api.On("GetConfig", mock.Anything).Return(cfg, nil)
	g := newGrpc(api)
	api.On("QueryConsensusFunc", "pos33", "GetPos33TicketCount", mock.Anything).Return(&types.Int64{}, nil)
	data, err := g.GetPos33TicketCount(context.Background(), nil)
	assert.Nil(t, err, "the error should be nil")
	assert.Equal(t, data, &types.Int64{})
}

func TestGetPos33TicketCount(t *testing.T) {
	//testGetPos33TicketCountReject(t)
	testGetPos33TicketCountOK(t)
}

func testSetAutoMiningOK(t *testing.T) {
	api := &mocks.QueueProtocolAPI{}
	g := newGrpc(api)
	in := &ty.Pos33MinerFlag{}
	api.On("ExecWalletFunc", "pos33", "WalletAutoMiner", in).Return(&types.Reply{}, nil)
	data, err := g.SetAutoMining(context.Background(), in)
	assert.Nil(t, err, "the error should be nil")
	assert.Equal(t, data, &types.Reply{})

}

func TestSetAutoMining(t *testing.T) {
	//testSetAutoMiningReject(t)
	testSetAutoMiningOK(t)
}

func testClosePos33TicketsOK(t *testing.T) {
	api := &mocks.QueueProtocolAPI{}
	g := newGrpc(api)
	var in = &ty.Pos33TicketClose{}
	api.On("ExecWalletFunc", "pos33", "ClosePos33Tickets", in).Return(&types.ReplyHashes{}, nil)
	data, err := g.ClosePos33Tickets(context.Background(), in)
	assert.Nil(t, err, "the error should be nil")
	assert.Equal(t, data, &types.ReplyHashes{})
}

func TestClosePos33Tickets(t *testing.T) {
	//testClosePos33TicketsReject(t)
	testClosePos33TicketsOK(t)
}

func TestJrpc_SetAutoMining(t *testing.T) {
	api := &mocks.QueueProtocolAPI{}
	j := newJrpc(api)
	var mingResult rpctypes.Reply
	api.On("ExecWalletFunc", mock.Anything, mock.Anything, mock.Anything).Return(&types.Reply{IsOk: true, Msg: []byte("yes")}, nil)
	err := j.SetAutoMining(&ty.Pos33MinerFlag{}, &mingResult)
	assert.Nil(t, err)
	assert.True(t, mingResult.IsOk, "SetAutoMining")
}

func TestJrpc_GetPos33TicketCount(t *testing.T) {
	api := &mocks.QueueProtocolAPI{}
	j := newJrpc(api)

	var ticketResult int64
	var expectRet = &types.Int64{Data: 100}
	api.On("QueryConsensusFunc", mock.Anything, mock.Anything, mock.Anything).Return(expectRet, nil)
	err := j.GetPos33TicketCount(&types.ReqNil{}, &ticketResult)
	assert.Nil(t, err)
	assert.Equal(t, expectRet.GetData(), ticketResult, "GetPos33TicketCount")
}

func TestRPC_CallTestNode(t *testing.T) {
	cfg := types.NewChain33Config(types.GetDefaultCfgstring())
	// 测试环境下，默认配置的共识为solo，需要修改
	cfg.GetModuleConfig().Consensus.Name = "pos33"

	api := new(mocks.QueueProtocolAPI)
	api.On("GetConfig", mock.Anything).Return(cfg, nil)
	mock33 := testnode.NewWithConfig(cfg, api)
	defer func() {
		mock33.Close()
		mock.AssertExpectationsForObjects(t, api)
	}()
	g := newGrpc(api)
	g.Init("pos33", mock33.GetRPC(), newJrpc(api), g)
	time.Sleep(time.Millisecond)
	mock33.Listen()
	time.Sleep(time.Millisecond)
	ret := &types.Reply{
		IsOk: true,
		Msg:  []byte("123"),
	}
	api.On("IsSync").Return(ret, nil)
	api.On("Version").Return(&types.VersionInfo{Chain33: version.GetVersion()}, nil)
	api.On("Close").Return()
	rpcCfg := mock33.GetCfg().RPC
	jsonClient, err := jsonclient.NewJSONClient("http://" + rpcCfg.JrpcBindAddr + "/")
	assert.Nil(t, err)
	assert.NotNil(t, jsonClient)
	var result types.VersionInfo
	err = jsonClient.Call("Chain33.Version", nil, &result)
	fmt.Println(err)
	assert.Nil(t, err)
	assert.Equal(t, version.GetVersion(), result.Chain33)

	var isSnyc bool
	err = jsonClient.Call("Chain33.IsSync", &types.ReqNil{}, &isSnyc)
	assert.Nil(t, err)
	assert.Equal(t, ret.GetIsOk(), isSnyc)

	flag := &ty.Pos33MinerFlag{Flag: 1}
	//调用ticket.AutoMiner
	api.On("ExecWalletFunc", "pos33", "WalletAutoMiner", flag).Return(&types.Reply{IsOk: true}, nil)
	var res rpctypes.Reply
	err = jsonClient.Call("pos33.SetAutoMining", flag, &res)
	assert.Nil(t, err)
	assert.Equal(t, res.IsOk, true)

	//test  grpc

	ctx := context.Background()
	c, err := grpc.DialContext(ctx, rpcCfg.GrpcBindAddr, grpc.WithInsecure())
	assert.Nil(t, err)
	assert.NotNil(t, c)

	client := types.NewChain33Client(c)
	issync, err := client.IsSync(ctx, &types.ReqNil{})
	assert.Nil(t, err)
	assert.Equal(t, true, issync.IsOk)

	client2 := ty.NewPos33TicketClient(c)
	r, err := client2.SetAutoMining(ctx, flag)
	assert.Nil(t, err)
	assert.Equal(t, r.IsOk, true)
}
*/

var cfgstring = `
FxTime = true
Title = "ycc"

[log]
# 日志级别，支持debug(dbug)/info/warn/error(eror)/crit
logConsoleLevel = "error"
loglevel = "info"
# 日志文件名，可带目录，所有生成的日志文件都放到此目录下
logFile = "logs/chain33.log"
# 单个日志文件的最大值（单位：兆）
maxFileSize = 300
# 最多保存的历史日志文件个数
maxBackups = 100
# 最多保存的历史日志消息（单位：天）
maxAge = 28
# 日志文件名是否使用本地事件（否则使用UTC时间）
localTime = true
# 历史日志文件是否压缩（压缩格式为gz）
compress = true
# 是否打印调用源文件和行号
callerFile = true
# 是否打印调用方法
callerFunction = false

[blockchain]
batchsync = false
dbCache = 128
dbPath = "datadir"
defCacheSize = 128
enableTxQuickIndex = true
isRecordBlockSequence = false
# 升级storedb是否重新执行localdb，bityuan主链升级不需要开启，平行链升级需要开启
enableReExecLocal = false
# 使能精简localdb
enableReduceLocaldb = true
singleMode = false

[mempool]
# 最小得交易手续费率，这个没有默认值，必填，一般是0.001 coins
minTxFeeRate = 100000
# 最大的交易手续费率, 0.1 coins
maxTxFeeRate = 10000000
# 单笔交易最大的手续费, 10 coins
maxTxFee = 1000000000
# disableExecCheck = true
isLevelFee = true
maxTxNumPerAccount = 1000
name = "price"
poolCacheSize = 102400

[p2p]
dbCache = 4
dbPath = "datadir/addrbook"
grpcLogFile = "grpc33.log"
types = ["dht"]
#waitPid 等待seed导入
waitPid = false

[p2p.sub.gossip]
innerBounds = 300
innerSeedEnable = true
isSeed = false
port = 13702
useGithub = false

[p2p.sub.dht]
#可以自定义设置连接节点
channel = 7
port = 13701
seeds = [
  "/ip4/139.9.43.189/tcp/13701/p2p/16Uiu2HAkvVA1m1ALGfYST6cZRNfsdyjjmAnWFrjA2zqtr1cjhtBA",
  # "/ip4/124.70.185.34/tcp/13701/p2p/16Uiu2HAm7EpC4emTJv39k7tNurbUNtWTEPKiNRdTfiYYuQ5yCqs3",
  # "/ip4/124.71.142.233/tcp/13701/p2p/16Uiu2HAkvozQc4Vrnu6rwNvLhA2gUDMnSCP918EPTQqgnR3uD6Ro",
  # "/ip4/123.60.23.154/tcp/13701/p2p/16Uiu2HAmMAGWFPWzpzG7i34XfpiM8s529Ed2vzVdAHSgnEoyjZD6",
  # "/ip4/123.60.23.251/tcp/13701/p2p/16Uiu2HAmLHbAYgaDfDJ5Jc42gfwRBjBKg7PXRN9yXALSNju9wwq1",
]

#dht 版本还不稳定，暂时限定较小的连接数
maxConnectNum = 50
#区块轻广播最低区块大小，1k
minLtBlockSize = 1
# 是否配置为全节点模式，全节点保存所有分片数据，种子节点应配置为true
# 全节点可以切换为分片节点，暂不支持分片节点切换为全节点
isFullNode = false
# 兼容老版本广播节点数，目前比特元网络已基本全面升级6.5.3，新版本不再支持广播发送至老版本
# 设为1为屏蔽默认设置5
maxBroadcastPeers = 1

[p2p.sub.dht.pubsub]
gossipSubD = 10
gossipSubDhi = 20
gossipSubDlo = 7
gossipSubHeartbeatInterval = 900
gossipSubHistoryGossip = 2
gossipSubHistoryLength = 7

[rpc]
certFile = "cert.pem"
enableTLS = false
grpcBindAddr = "localhost:7902"
grpcFuncWhitelist = ["*"]
jrpcBindAddr = "localhost:7901"
jrpcFuncWhitelist = ["*"]
keyFile = "key.pem"
whitelist = ["127.0.0.1"]

[consensus.sub.pos33]
bootPeers = [
  "/ip4/139.9.43.189/tcp/10801/p2p/16Uiu2HAmErmNhtS145Lv5fe9FWrHSrNjPkp1eMLeLgi6t3sdr1of",
  # "/ip4/124.70.185.34/tcp/10801/p2p/16Uiu2HAmJjC9TDXYgrx2DyBH7BLYiMFH8RiYA9m5twNhfPCFJ4QN",
  # "/ip4/124.71.142.233/tcp/10801/p2p/16Uiu2HAmKPNRzv7bwBvaoSaZabvrrU7dmDN7PmCGHSyCgm7RXvi6",
  # "/ip4/123.60.23.154/tcp/10801/p2p/16Uiu2HAmBUNMfDj94jt2os4xgRGqAKTPq5oosFmrCvDK9Khr2iy3",
  # "/ip4/123.60.23.251/tcp/10801/p2p/16Uiu2HAmGqCQyV1GDme86cEwLWR89sjLPocuJubsM1UqBqRY6hrj",
]
forwardServers = ["124.71.10.240:10902"]
listenPort = 10801
onlyVoter = false
# forwardPeers = true

[mver.consensus]
fundKeyAddr = "1EbDHAXpoiewjPLX9uqoz38HsKqMXayZrF"
maxTxNumber = 30000
powLimitBits = "0x1f00ffff"

[mver.consensus.ForkChainParamV1]
maxTxNumber = 30000

[store]
dbCache = 128
dbPath = "datadir/mavltree"
driver = "leveldb"
name = "kvmvccmavl"
storedbVersion = "2.0.0"

[store.sub.mavl]
enableMVCC = false
enableMavlPrefix = true
enableMavlPrune = true
enableMemTree = true
enableMemVal = true
pruneHeight = 10000
# 缓存close ticket数目，该缓存越大同步速度越快，最大设置到1500000,默认200000
tkCloseCacheLen = 200000

[store.sub.kvmvccmavl]
enableMVCC = false
enableMVCCIter = true
enableMVCCPrune = false
enableMavlPrefix = true
enableMavlPrune = true
enableMemTree = true
enableMemVal = true
pruneMVCCHeight = 10000
pruneMavlHeight = 10000
# 缓存close ticket数目，该缓存越大同步速度越快，最大设置到1500000,默认200000
tkCloseCacheLen = 200000

[wallet]
dbCache = 16
dbPath = "wallet"
driver = "leveldb"
minFee = 100000
signType = "secp256k1"

[exec]
#disableAddrIndex = true
enableMVCC = false
enableStat = false

[exec.sub.token]
saveTokenTxList = false

[metrics]
#是否使能发送metrics数据的发送
enableMetrics = false
#数据保存模式
dataEmitMode = "influxdb"

[metrics.sub.influxdb]
#以纳秒为单位的发送间隔
database = "chain33metrics"
duration = 1000000000
namespace = ""
password = ""
url = "http://influxdb:8086"
username = ""

`
