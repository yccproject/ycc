package main

//ycc 这部分配置随代码发布，不能修改
var yccconfig = `
TestNet=true
version="6.6.0.0"
CoinSymbol="ycc"
ChainID=999
CoinPrecision=10000

[blockchain]
maxFetchBlockNum=128
timeoutSeconds=1
batchBlockNum=128
driver="leveldb"
isStrongConsistency=false
disableShard=true
onChainTimeout=1

[p2p]
enable=true
msgCacheSize=10240
driver="leveldb"

[p2p.sub.gossip]
serverStart=true

[p2p.sub.dht]
#bootstraps是内置不能修改的引导节点

[mempool]


[mempool.sub.score]
poolCacheSize=102400
minTxFee=100000
maxTxNumPerAccount=100
timeParam=1      #时间占价格比例
priceConstant=1544  #手续费相对于时间的一个合适的常量,取当前unxi时间戳前四位数,排序时手续费高1e-5~=快1s
pricePower=1     #常量比例

[mempool.sub.price]
poolCacheSize=102400

[consensus]
name="pos33"
minerstart=true
genesisBlockTime=1604449783
genesis="14KEKbYtKKQm4wMthSK9J4La4nAiidGozt"
minerExecs=["pos33"]

[consensus.sub.pos33]
genesisBlockTime=1611627559
checkFutureBlockHeight=1500000

[[consensus.sub.pos33.genesis]]
minerAddr="12qyocayNF7Lv6C9qW4avxs2E7U41fKSfv"
returnAddr="14KEKbYtKKQm4wMthSK9J4La4nAiidGozt"
blsAddr="1HD3KyzUqMKT3fnKCXsFx91PZ1R2tiegXk"
count=1000


[mver.consensus.ForkChainParamV2]
powLimitBits="0x1f2fffff"


[mver.consensus.pos33]
ticketPrice1=10000
ticketPrice2=100000
minerFeePersent=10 
rewardTransfer=1
blockReward=15
voteRewardPersent=25
mineRewardPersent=11
managerAddr="14KEKbYtKKQm4wMthSK9J4La4nAiidGozt"


[store]

[crypto]
enableTypes = ["secp256k1", "none", "bls"]


[exec]

[exec.sub.token]
#配置一个空值，防止配置文件被覆盖
tokenApprs=[]
[exec.sub.relay]
genesis="14KEKbYtKKQm4wMthSK9J4La4nAiidGozt"

[exec.sub.manage]
superManager=[
    "15m3SZWnMFB9yhUfn1h5miiZbPp6bEvyQt", 
]

[exec.sub.paracross]
nodeGroupFrozenCoins=0
#平行链共识停止后主链等待的高度
paraConsensusStopBlocks=30000

[exec.sub.autonomy]
total="16htvcBNSEA7fZhAdLJphDwQRQJaHpyHTp"
useBalance=false

#系统中所有的fork,默认用chain33的测试网络的
#但是我们可以替换
[fork.system]
ForkChainParamV1=0
ForkCheckTxDup=0
ForkBlockHash=0
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

[fork.sub.coins]
Enable=0

[fork.sub.pos33]
Enable=0
#ForkReward15=0 
#ForkFixReward=5000000
#UseEntrust=9870000
ForkReward15=0 
ForkFixReward=0
UseEntrust=0


[fork.sub.evm]
Enable=0
ForkEVMYoloV1=0
ForkEVMTxGroup=0
ForkEVMState=0
ForkEVMABI=0
ForkEVMKVHash=0
ForkEVMFrozen=0

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


[fork.sub.unfreeze]
Enable=0
ForkTerminatePart=0
ForkUnfreezeIDX=0

[fork.sub.store-kvmvccmavl]
ForkKvmvccmavl=0

[health]
listenAddr="localhost:8709"
checkInterval=1
unSyncMaxTimes=2
`
