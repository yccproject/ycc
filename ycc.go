package main

//ycc 这部分配置随代码发布，不能修改
var yccconfig = `
TestNet=false
version="6.4.2"
CoinSymbol="YCC"

[blockchain]
defCacheSize=128
maxFetchBlockNum=128
timeoutSeconds=5
batchBlockNum=128
driver="leveldb"
isStrongConsistency=false
singleMode=true

[p2p]
enable=true
msgCacheSize=10240
driver="leveldb"

[p2p.sub.gossip]
serverStart=true

[p2p.sub.dht]
#bootstraps是内置不能修改的引导节点

[mempool]
name="price"
poolCacheSize=102400
minTxFeeRate=100000
maxTxFee=1000000000
isLevelFee=true

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
genesisBlockTime=1514533394
genesis="14KEKbYtKKQm4wMthSK9J4La4nAiidGozt"
minerExecs=["pos33"]

[consensus.sub.pos33]
genesisBlockTime=1514533394
listenPort="10901"
# bootPeers=["/ip4/183.129.226.76/tcp/10901/p2p/16Uiu2HAmErmNhtS145Lv5fe9FWrHSrNjPkp1eMLeLgi6t3sdr1of"]

[[consensus.sub.pos33.genesis]]
minerAddr="12qyocayNF7Lv6C9qW4avxs2E7U41fKSfv"
returnAddr="14KEKbYtKKQm4wMthSK9J4La4nAiidGozt"
count=10000

[mver.consensus]
fundKeyAddr = "1Wj2mPoBwJMVwAQLKPNDseGpDNibDt9Vq"
powLimitBits="0x1f00ffff"
maxTxNumber=10000

[mver.consensus.ForkChainParamV1]
maxTxNumber=10000

[mver.consensus.ForkChainParamV2]
powLimitBits = "0x1f2fffff"

[mver.consensus.ForkTicketFundAddrV1]
fundKeyAddr = "1Wj2mPoBwJMVwAQLKPNDseGpDNibDt9Vq"

[mver.consensus.pos33]
coinReward = 18
coinDevFund = 12
ticketPrice = 10000
retargetAdjustmentFactor = 4
futureBlockTime = 15
ticketFrozenTime = 43200
ticketWithdrawTime = 172800
ticketMinerWaitTime = 7200
targetTimespan = 2160
targetTimePerBlock = 15

[store]
name="kvmvccmavl"
driver="leveldb"
storedbVersion="2.0.0"

[wallet]
minFee=100000
driver="leveldb"
signType="secp256k1"

[exec]
[exec.sub.token]
#配置一个空值，防止配置文件被覆盖
tokenApprs = []
[exec.sub.relay]
genesis="14KEKbYtKKQm4wMthSK9J4La4nAiidGozt"

[exec.sub.manage]
superManager=[
    "1JmFaA6unrCFYEWPGRi7uuXY1KthTJxJEP", 
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
ForkChainParamV1= 0
ForkCheckTxDup=0
ForkBlockHash= 1
ForkMinerTime= 0
ForkTransferExec= 100000
ForkExecKey= 200000
ForkTxGroup= 200000
ForkResetTx0= 200000
ForkWithdraw= 200000
ForkExecRollback= 450000
ForkCheckBlockTime=2270000
ForkTxHeight= -1
ForkTxGroupPara= 2270000
ForkChainParamV2= 2270000
ForkMultiSignAddress=2270000
ForkStateDBSet=2270000
ForkLocalDBAccess=2270000
ForkBlockCheck=2270000
ForkBase58AddressCheck=2270000
ForkEnableParaRegExec=2270000
ForkCacheDriver=4320000
ForkTicketFundAddrV1=4320000
#fork for 6.4
ForkRootHash=7200000           

[fork.sub.coins]
Enable=0

[fork.sub.ticket]
Enable=0
ForkTicketId = 1600000
ForkTicketVrf = 2270000

[fork.sub.retrieve]
Enable=0
ForkRetrive=0
ForkRetriveAsset=4320000

[fork.sub.hashlock]
Enable=0
ForkBadRepeatSecret=4320000

[fork.sub.manage]
Enable=0
ForkManageExec=100000

[fork.sub.token]
Enable=0
ForkTokenBlackList= 0
ForkBadTokenSymbol= 0
ForkTokenPrice= 300000
ForkTokenSymbolWithNumber=1600000
ForkTokenCheck= 2270000

[fork.sub.trade]
Enable=0
ForkTradeBuyLimit= 0
ForkTradeAsset= 2270000
ForkTradeID = 2270000
ForkTradeFixAssetDB=4320000
ForkTradePrice=4320000

[fork.sub.paracross]
Enable=1600000
ForkParacrossWithdrawFromParachain=1600000
ForkParacrossCommitTx=2270000
ForkLoopCheckCommitTxDone=4320000
#fork for 6.4
ForkParaAssetTransferRbk=7200000    
ForkParaSelfConsStages=7200000

[fork.sub.multisig]
Enable=1600000

[fork.sub.autonomy]
Enable=7200000

[fork.sub.unfreeze]
Enable=1600000
ForkTerminatePart=1600000
ForkUnfreezeIDX= 2270000

[fork.sub.store-kvmvccmavl]
ForkKvmvccmavl=2270000
`
