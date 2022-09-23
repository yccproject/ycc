package main

//ycc 这部分配置随代码发布，不能修改
var yccconfig = `
TxHeight = true
FixTime = false

[log]
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
maxFetchBlockNum=128
timeoutSeconds=1
batchBlockNum=128
isStrongConsistency=false
onChainTimeout=1
driver="leveldb"
defCacheSize = 256

[p2p]
enable=true
msgCacheSize=10240
driver="leveldb"
types = ["dht"]
waitPid = false
dbCache = 16

[p2p.sub.gossip]
innerBounds = 300
innerSeedEnable = true
isSeed = false
port = 13702
useGithub = false
serverStart=true

[p2p.sub.dht]
#dht 版本还不稳定，暂时限定较小的连接数
maxConnectNum = 50
# 是否配置为全节点模式，全节点保存所有分片数据，种子节点应配置为true
# 全节点可以切换为分片节点，暂不支持分片节点切换为全节点
isFullNode = true
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
disablePubSubMsgSign = true

[address]
defaultDriver="eth"
[address.enableHeight]
eth=0

[mempool]
minTxFeeRate = 100000
maxTxFeeRate = 10000000
isLevelFee = false
maxTxFee=100000000
name = "price"
enableEthCheck=true

[mempool.sub.score]
poolCacheSize=1024000
minTxFee=100000
maxTxNumPerAccount=100000
timeParam=1      #时间占价格比例
priceConstant=1544  #手续费相对于时间的一个合适的常量,取当前unxi时间戳前四位数,排序时手续费高1e-5~=快1s
pricePower=1     #常量比例

[mempool.sub.price]
poolCacheSize = 1024000

[consensus]
name="pos33"
minerstart=true
genesisBlockTime=1652797628
genesis="0xf39e69a8f2c1041edd7616cf079c7084bb7a5242"
minerExecs=["pos33"]

[consensus.sub.pos33]
onlyVoter = false

[[consensus.sub.pos33.genesis]]
minerAddr="0x991fb09dc31a44b3177673f330c582ac2ea168e0"
returnAddr="0xf39e69a8f2c1041edd7616cf079c7084bb7a5242"
blsAddr="0x6da47c11230e44adb384fed56554bfd55dde1750"
count=1000

[mver.consensus]
addWalletTx = false
fundKeyAddr = "0x92dd51393c77fd07c5840ae28076b7e0f072c289"
maxTxNumber = 30000
powLimitBits = "0x1f00ffff"

[mver.consensus.pos33]
ticketPrice1=10000
ticketPrice2=100000
minerFeePersent=10 
rewardTransfer=1
blockReward=15
voteRewardPersent=25
mineRewardPersent=11

[store]
dbCache = 256
driver = "leveldb"
name = "kvmvcc"

[store.sub.kvmvcc]
enableMVCC = false
enableMavlPrefix = false

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

[crypto]

[crypto.sub.secp256k1eth]
evmChainID=3999

[wallet]
dbCache = 16
driver = "leveldb"
minFee = 100000
signType = "secp256k1"

[exec]
#disableAddrIndex = true
#disableFeeIndex = true
#disableTxIndex = true
enableMVCC = false
enableStat = false

[exec.sub.coins]
disableAddrReceiver = true
disableCheckTxAmount = true
friendExecer=["evm"]

[exec.sub.evm]
ethMapFromExecutor="coins"
ethMapFromSymbol="ycc"
addressDriver="eth"


[exec.sub.token]
saveTokenTxList = false
#配置一个空值，防止配置文件被覆盖
tokenApprs=[]
[exec.sub.relay]
genesis="0x6b0a9bdf4d994c359fe02a74b538cf3f27b83f0a"

[exec.sub.manage]
superManager=[
    "0xf27a1b1a548b7bf380c5011364670b5218edc54b", 
]

[exec.sub.paracross]
nodeGroupFrozenCoins=0
#平行链共识停止后主链等待的高度
paraConsensusStopBlocks=30000

[exec.sub.autonomy]
total="0x6950e4d7a94947b1f36265828de26c13ba3dee69"
useBalance=false

[mver.autonomy]
#最小委员会数量
minBoards=20
#最大委员会数量
maxBoards=40
#公示一周时间，以区块高度计算
publicPeriod=120960
#单张票价
ticketPrice=3000
#重大项目公示金额阈值
largeProjectAmount=1000000
#创建者消耗金额bty
proposalAmount=500
#董事会成员赞成率，以%计，可修改
boardApproveRatio=51
#全体持票人参与率，以%计
pubAttendRatio=51
#全体持票人赞成率，以%计
pubApproveRatio=51
#全体持票人否决率，以%计
pubOpposeRatio=33
#提案开始结束最小周期高度
startEndBlockPeriod=720
#提案高度 结束高度最大周期 100W
propEndBlockPeriod=1000000
#最小董事会赞成率
minBoardApproveRatio=50
#最大董事会赞成率
maxBoardApproveRatio=66
#最小全体持票人否决率
minPubOpposeRatio=33
#最大全体持票人否决率
maxPubOpposeRatio=50
#可以调整，但是可能需要进行范围的限制：参与率最低设置为 50%， 最高设置为 80%，赞成率，最低 50.1%，最高80%
#不能设置太低和太高，太低就容易作弊，太高则有可能很难达到
#最小全体持票人参与率
minPubAttendRatio=50
#最大全体持票人参与率
maxPubAttendRatio=80
#最小全体持票人赞成率
minPubApproveRatio=50
#最大全体持票人赞成率
maxPubApproveRatio=80
#最小公示周期
minPublicPeriod=120960
#最大公示周期
maxPublicPeriod=241920
#最小重大项目阈值(coin)
minLargeProjectAmount=1000000
#最大重大项目阈值(coin)
maxLargeProjectAmount=3000000
#最小提案金(coin)
minProposalAmount=20
#最大提案金(coin)
maxProposalAmount=2000	
#每个时期董事会审批最大额度300万
maxBoardPeriodAmount =3000000
#时期为一个月
boardPeriod=518400
#4w高度，大概2天 (未生效)
itemWaitBlockNumber=40000 

#系统中所有的fork,默认用chain33的测试网络的
#但是我们可以替换
[fork.system]
ForkChainParamV1=0
ForkCheckTxDup=0
ForkBlockHash=5000000
ForkMinerTime=0
ForkTransferExec=0
ForkExecKey=0
ForkTxGroup=0
ForkResetTx0=0
ForkWithdraw=0
ForkExecRollback=0
ForkCheckBlockTime=0
ForkTxHeight=0
ForkTxGroupPara=0
ForkChainParamV2=0
ForkMultiSignAddress=0
ForkStateDBSet=0
ForkLocalDBAccess=0
ForkBlockCheck=0
ForkBase58AddressCheck=0
#平行链上使能平行链执行器如user.p.x.coins执行器的注册，缺省为0，对已有的平行链需要设置一个fork高度
ForkEnableParaRegExec=0
ForkCacheDriver=0
ForkTicketFundAddrV1=-1 #fork6.3
#主链和平行链都使用同一个fork高度
ForkRootHash=0 
ForkFormatAddressKey=0

[fork.sub.coins]
Enable=0
ForkFriendExecer=4864713

[fork.sub.pos33]
Enable=0
ForkReward15=0 
ForkFixReward=0
UseEntrust=0

[fork.sub.none]
ForkUseTimeDelay=0

[fork.sub.evmxgo]
Enable=0

[fork.sub.ticket]
Enable=0
ForkTicketId = 0 
ForkTicketVrf = 0

[fork.sub.retrieve]
Enable=0
ForkRetrive=0
ForkRetriveAsset=0

[fork.sub.hashlock]
Enable=0
ForkBadRepeatSecret=0

[fork.sub.manage]
Enable=0
ForkManageExec=0
ForkManageAutonomyEnable=-1

[fork.sub.token]
Enable=0
ForkTokenBlackList=0
ForkBadTokenSymbol=0
ForkTokenPrice=300000
ForkTokenSymbolWithNumber=0
ForkTokenCheck=0

[fork.sub.trade]
Enable=0
ForkTradeBuyLimit=0
ForkTradeAsset=0
ForkTradeID=0
ForkTradeFixAssetDB=0
ForkTradePrice=0

[fork.sub.paracross]
Enable=0
ForkParacrossWithdrawFromParachain=0
ForkParacrossCommitTx=0
ForkLoopCheckCommitTxDone=0
#fork for 6.4
ForkParaAssetTransferRbk=0
ForkParaSelfConsStages=0
#仅平行链适用
ForkParaFullMinerHeight=-1
ForkParaRootHash=0
ForkParaSupervision=0
ForkParaAutonomySuperGroup = -1
ForkParaFreeRegister = 0

[fork.sub.multisig]
Enable=0

[fork.sub.autonomy]
Enable=0
ForkAutonomyDelRule=0
ForkAutonomyEnableItem=0

[fork.sub.evm]
Enable=0
ForkEVMState=0
ForkEVMABI=0
ForkEVMFrozen=0
ForkEVMKVHash=0
ForkEVMYoloV1=0
ForkEVMTxGroup=0

[fork.sub.unfreeze]
Enable=0
ForkTerminatePart=0
ForkUnfreezeIDX=0

[fork.sub.store-kvmvccmavl]
ForkKvmvccmavl=0

[health]
checkInterval=1
unSyncMaxTimes=2

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
