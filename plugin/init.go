package plugin

import (
	_ "github.com/yccproject/ycc/plugin/consensus/init" //register consensus init package
	_ "github.com/yccproject/ycc/plugin/crypto/init"
	_ "github.com/yccproject/ycc/plugin/dapp/init"
	_ "github.com/yccproject/ycc/plugin/mempool/init"
	_ "github.com/yccproject/ycc/plugin/p2p/init"
	_ "github.com/yccproject/ycc/plugin/store/init"
)
