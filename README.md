# ycc
Yuan Chain Project
# pos33 运行

## 运行节点
	$ ./ycc 
或者
	
	$ nohup ./ycc &

## 挖矿
1. 创建mining 挖矿账户
	
		$ bash wallet-init.sh
	
2. 给mining账户打币作为手续费
3. 给委托账户打币用来委托挖矿
4.  委托账户抵押到合约
	
		$ bash deposit.sh 委托账户 数量
5. 委托挖矿
	
		$ bash entrust.sh 挖矿账号 委托账号 委托私钥 数量

