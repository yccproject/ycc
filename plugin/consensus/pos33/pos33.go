package pos33

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/33cn/chain33/common/address"
	"github.com/33cn/chain33/common/crypto"
	"github.com/33cn/chain33/queue"
	drivers "github.com/33cn/chain33/system/consensus"
	driver "github.com/33cn/chain33/system/dapp"
	ct "github.com/33cn/chain33/system/dapp/coins/types"
	"github.com/33cn/chain33/types"
	pt "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

func init() {
	drivers.Reg("pos33", New)
	drivers.QueryData.Register("pos33", &Client{})
}

// Client is the pos33 consensus client
type Client struct {
	*drivers.BaseClient
	conf *subConfig
	n    *node

	// clock  sync.Mutex
	priv   crypto.PrivKey
	myAddr string

	mlock sync.Mutex
	acMap map[int64]int
	tcMap map[int64]map[string]int64

	done chan struct{}
}

// Tx is ...
type Tx = types.Transaction

type genesisTicket struct {
	MinerAddr  string `json:"minerAddr"`
	ReturnAddr string `json:"returnAddr"`
	BlsAddr    string `json:"blsAddr"`
	Count      int32  `json:"count"`
}

type subConfig struct {
	Genesis          []*genesisTicket `json:"genesis"`
	GenesisBlockTime int64            `json:"genesisBlockTime"`
	ListenPort       int              `json:"listenPort,omitempty"`
	BootPeers        []string         `json:"bootPeers,omitempty"`
	ForwardServers   []string         `json:"forwardServers,omitempty"`
	ForwardPeers     bool             `json:"forwardPeers,omitempty"`
	// if true, you can't make block and only vote
	OnlyVoter bool `json:"onlyVoter,omitempty"`
	// only for test!!! if true, delay 5 second make block
	TrubleMaker bool `json:"trubleMaker,omitempty"`
	// only for test
	CheckFutureBlockHeight int64 `json:"checkFutureBlockHeight,omitempty"`
}

// New create pos33 consensus client
func New(cfg *types.Consensus, sub []byte) queue.Module {
	c := drivers.NewBaseClient(cfg)
	var subcfg subConfig
	if sub != nil {
		types.MustDecode(sub, &subcfg)
	}
	// plog.Debug("subcfg", "cfg", string(sub))

	n := newNode(&subcfg)
	client := &Client{
		BaseClient: c,
		n:          n,
		conf:       &subcfg,
		acMap:      make(map[int64]int),
		tcMap:      make(map[int64]map[string]int64),
		done:       make(chan struct{}),
	}
	client.n.Client = client
	c.SetChild(client)
	return client
}

// Close is close the client
func (client *Client) Close() {
	client.done <- struct{}{}
	client.BaseClient.Close()
	plog.Debug("pos33 consensus closed")
}

// ProcEvent do nothing?
func (client *Client) ProcEvent(msg *queue.Message) bool {
	return false
}

// CheckBlock check block callback
func (client *Client) CheckBlock(parent *types.Block, current *types.BlockDetail) error {
	if len(current.Receipts) > 0 && current.Receipts[0].Ty != types.ExecOk {
		return types.ErrCoinBaseExecErr
	}
	return client.BlockCheck(parent, current.Block)
}
func (client *Client) BlockCheck(parent *types.Block, current *types.Block) error {
	if current.Height >= client.conf.CheckFutureBlockHeight && current.BlockTime-types.Now().Unix() > 5 {
		return types.ErrFutureBlock
	}
	return client.n.checkBlock(current, parent)
}

func (client *Client) allCount(height int64) int {
	client.mlock.Lock()
	defer client.mlock.Unlock()

	if height < 0 {
		height = 0
	}
	c, ok := client.acMap[height]
	if !ok {
		c = client.acMap[client.GetCurrentHeight()]
	}
	return c
}

func privFromBytes(privkey []byte) (crypto.PrivKey, error) {
	cr, err := crypto.Load(types.GetSignName("", types.SECP256K1), -1)
	if err != nil {
		return nil, err
	}
	if privkey == nil {
		return nil, errors.New("null privKey")
	}
	return cr.PrivKeyFromBytes(privkey)
}

func (client *Client) getPriv() crypto.PrivKey {
	if client.priv == nil {
		plog.Error("Wallet LOCKED or not Set mining account")
		return nil
	}
	return client.priv
}

func (c *Client) AddBlock(b *types.Block) error {
	c.n.addBlock(b)
	c.updateTicketCount(b)
	return nil
}

func (c *Client) updateTicketCount(b *types.Block) {
	c.mlock.Lock()
	defer c.mlock.Unlock()

	height := b.Height
	if b.Height == 0 {
		height = 0
	}
	for i, tx := range b.Txs {
		if i != 0 && string(tx.Execer) == "pos33" {
			pa := new(pt.Pos33TicketAction)
			err := types.Decode(tx.Payload, pa)
			if err != nil {
				plog.Error("pos33 action decode errorr", "err", err, "height", height)
				return
			}

			miner := ""
			switch pa.Ty {
			case pt.Pos33TicketActionGenesis:
				miner = c.conf.Genesis[0].MinerAddr
			case pt.Pos33ActionMigrate, pt.Pos33TicketActionClose, pt.Pos33TicketActionOpen:
				miner = tx.From()
			case pt.Pos33ActionEntrust:
				entrust := pa.GetEntrust()
				miner = entrust.Consignee
			}
			if miner != "" {
				plog.Debug("set entrust", "height", height, "miner", miner)
				c.queryMinerTicketCount(miner, height)
				c.queryAllPos33Count(height)
			}
		}
	}
	chain33Cfg := c.GetAPI().GetConfig()
	if pt.GetPos33MineParam(chain33Cfg, height).ChangeTicketPrice() {
		plog.Debug("update ticket count because price changed", "height", height)
		c.queryAllPos33Count(height)
		for k := range c.tcMap[height-1] {
			c.queryMinerTicketCount(k, height)
		}
	}
	_, ok := c.acMap[height]
	if !ok {
		c.acMap[height] = c.acMap[height-1]
	}

	if c.acMap[height] == 0 {
		c.queryAllPos33Count(height)
	}

	_, ok = c.tcMap[height]
	if !ok {
		c.tcMap[height] = c.tcMap[height-1]
		// } else {
		// 	last := c.tcMap[height-1]
		// 	for k, v := range last {
		// 		if mp[k] == 0 {
		// 			mp[k] = v
		// 		}
		// 	}
	}
	plog.Info("update ticket count", "height", b.Height, "all count", c.acMap[b.Height])
	delete(c.acMap, height-pt.Pos33SortBlocks*2-1)
	delete(c.tcMap, height-pt.Pos33SortBlocks*2-1)
}

func (c *Client) getMiner() {
	if c.myAddr != "" {
		return
	}

	resp, err := c.GetAPI().ExecWalletFunc("pos33", "WalletGetMiner", &types.ReqNil{})
	if err != nil {
		plog.Debug("WalletGetMinerAddr", "err", err)
		return
	}
	w := resp.(*types.ReplyString)
	c.priv, err = privFromBytes([]byte(w.Data))
	if err != nil {
		plog.Error("privFromBytes", "err", err)
		return
	}
	c.myAddr = address.PubKeyToAddr(ethID, c.priv.PubKey().Bytes())
	plog.Debug("getMiner", "addr", c.myAddr)
}

func (c *Client) queryEntrustCount(miner string, height int64) int64 {
	msg, err := c.GetAPI().Query(pt.Pos33TicketX, "Pos33ConsigneeEntrust", &types.ReqAddr{Addr: miner})
	if err != nil {
		plog.Error("query Pos33Consignee error", "error", err, "height", height, "miner", miner)
		return 0
	}
	consignee := msg.(*pt.Pos33Consignee)
	price := pt.GetPos33MineParam(c.GetAPI().GetConfig(), c.GetCurrentHeight()).GetTicketPrice()
	return consignee.Amount / price
}

func (c *Client) queryTicketCount(addr string, height int64) int64 {
	c.mlock.Lock()
	defer c.mlock.Unlock()

	if height < 0 {
		height = 0
	}
	if addr == "" {
		return 0
	}

	count := int64(0)
	mp, ok := c.tcMap[height]
	if ok {
		count, ok = mp[addr]
	}
	if !ok {
		count = c.queryMinerTicketCount(addr, height)
	}
	// plog.Debug("query ticket count", "height", height, "addr", addr, "count", count)
	return count
}

func (c *Client) queryMinerTicketCount(addr string, height int64) int64 {
	mp, ok := c.tcMap[height]
	if !ok || mp == nil {
		mp = make(map[string]int64)
	}
	if height < 0 {
		height = 0
	}

	count := int64(0)
	cfg := c.GetAPI().GetConfig()
	if cfg.IsDappFork(height, pt.Pos33TicketX, "UseEntrust") {
		count = c.queryEntrustCount(addr, height)
	} else {
		msg, err := c.GetAPI().Query(pt.Pos33TicketX, "Pos33TicketCount", &types.ReqAddr{Addr: addr})
		if err != nil {
			plog.Error("query count error", "error", err)
			count = 0
		} else {
			count = msg.(*types.Int64).Data
		}
	}
	// plog.Debug("query miner ticket count", "height", height, "miner", addr, "count", count)
	mp[addr] = count
	c.tcMap[height] = mp
	return count
}

func (c *Client) queryAllPos33Count(height int64) int {
	var msg types.Message
	var err error
	cfg := c.GetAPI().GetConfig()
	useAmount := cfg.IsDappFork(height, pt.Pos33TicketX, "UseEntrust")
	// plog.Debug("query all ticket count", "height", height, "useAmount", useAmount)
	if useAmount {
		msg, err = c.GetAPI().Query(pt.Pos33TicketX, "AllPos33TicketAmount", &types.ReqNil{})
	} else {
		msg, err = c.GetAPI().Query(pt.Pos33TicketX, "AllPos33TicketCount", &types.ReqNil{})
	}
	if err != nil {
		plog.Error("query all tickets count error", "error", err, "height", height, "useAmount", useAmount)
		return 0
	}
	count := msg.(*types.Int64).Data
	if useAmount {
		count = count / pt.GetPos33MineParam(cfg, height).GetTicketPrice()
	}
	c.acMap[height] = int(count)
	return int(count)
}

func (client *Client) myCount() int {
	client.getMiner()
	height := client.GetCurrentHeight()
	return int(client.queryTicketCount(client.myAddr, height))
}

// CreateBlock will start run
func (client *Client) CreateBlock() {
	b, err := client.RequestBlock(0)
	if err != nil {
		panic(err)
	}
	client.getMiner()
	gtm := client.GetGenesisBlockTime()
	plog.Debug("CreateBlock", "block 0 time", b.BlockTime, "genesis time", gtm)
	if b.BlockTime != gtm {
		panic("block 0 is NOT match config, remove old data or change the config file")
	}
	for {
		select {
		case <-client.done:
			plog.Debug("pos33 client done!!!")
			return
		default:
		}
		if client.IsClosed() {
			plog.Debug("create block stop")
			break
		}
		if !client.IsMining() || !(client.IsCaughtUp() || client.Cfg.ForceMining) {
			plog.Debug("createblock.ismining is disable or client is caughtup is false")
			time.Sleep(time.Second)
			continue
		}
		if client.myCount() == 0 {
			plog.Debug("createblock.myCount is 0")
			time.Sleep(time.Second * 3)
			continue
		}
		break
	}
	client.n.runLoop()
}

func createTicket(cfg *types.Chain33Config, minerAddr, returnAddr, blsAddr string, count int32, height int64) (ret []*types.Transaction) {
	//给hotkey 1000 个币，作为miner的手续费
	tx1 := types.Transaction{}
	tx1.Execer = []byte("coins")
	tx1.To = minerAddr
	g := &ct.CoinsAction_Genesis{}
	g.Genesis = &types.AssetsGenesis{Amount: cfg.GetCoinPrecision() * 1000}
	tx1.Payload = types.Encode(&ct.CoinsAction{Value: g, Ty: ct.CoinsActionGenesis})
	ret = append(ret, &tx1)

	// 发行并抵押
	tx2 := types.Transaction{}
	tx2.Execer = []byte("coins")
	tx2.To = driver.ExecAddress(pt.Pos33TicketX)
	g = &ct.CoinsAction_Genesis{}
	g.Genesis = &types.AssetsGenesis{Amount: int64(count) * pt.GetPos33MineParam(cfg, height).GetTicketPrice(), ReturnAddress: returnAddr}
	tx2.Payload = types.Encode(&ct.CoinsAction{Value: g, Ty: ct.CoinsActionGenesis})
	ret = append(ret, &tx2)

	if !cfg.IsDappFork(0, pt.Pos33TicketX, "UseEntrust") {
		// 冻结资金并开启挖矿
		tx3 := types.Transaction{}
		tx3.Execer = []byte(pt.Pos33TicketX)
		tx3.To = driver.ExecAddress(pt.Pos33TicketX)
		gticket := &pt.Pos33TicketAction_Genesis{}
		gticket.Genesis = &pt.Pos33TicketGenesis{MinerAddress: minerAddr, ReturnAddress: returnAddr, BlsAddress: blsAddr, Count: count}
		tx3.Payload = types.Encode(&pt.Pos33TicketAction{Value: gticket, Ty: pt.Pos33TicketActionGenesis})
		ret = append(ret, &tx3)
		plog.Debug("genesis miner", "execaddr", tx3.To, "genesistx", g.Genesis)
	} else {
		amount := int64(count) * pt.GetPos33MineParam(cfg, 0).GetTicketPrice()
		entrustAct := &pt.Pos33TicketAction_Entrust{
			Entrust: &pt.Pos33Entrust{
				Consignee: minerAddr,
				Consignor: returnAddr,
				Amount:    amount,
			},
		}
		tx := &types.Transaction{
			Execer:  []byte(pt.Pos33TicketX),
			To:      driver.ExecAddress(pt.Pos33TicketX),
			Payload: types.Encode(&pt.Pos33TicketAction{Value: entrustAct, Ty: pt.Pos33ActionEntrust}),
		}
		ret = append(ret, tx)

		blsBindAct := &pt.Pos33TicketAction_BlsBind{
			BlsBind: &pt.Pos33BlsBind{
				BlsAddr:   blsAddr,
				MinerAddr: minerAddr,
			},
		}
		tx = &types.Transaction{
			Execer:  []byte(pt.Pos33TicketX),
			To:      driver.ExecAddress(pt.Pos33TicketX),
			Payload: types.Encode(&pt.Pos33TicketAction{Value: blsBindAct, Ty: pt.Pos33ActionBlsBind}),
		}
		ret = append(ret, tx)
		plog.Debug("genesis use entrust", "execaddr", tx.To, "miner", minerAddr, "consignor", returnAddr, "amount", amount)
	}
	return ret
}

// CreateGenesisTx ticket create genesis tx
func (client *Client) CreateGenesisTx() (ret []*types.Transaction) {
	// 预先发行maxcoin 到 genesis 账户
	tx0 := types.Transaction{}
	tx0.Execer = []byte("coins")
	tx0.To = client.Cfg.Genesis
	g := &ct.CoinsAction_Genesis{}
	// 发行 100 亿
	cfg := client.GetAPI().GetConfig()
	coin := cfg.GetCoinPrecision()
	g.Genesis = &types.AssetsGenesis{Amount: types.MaxCoin * 10 * coin}
	tx0.Payload = types.Encode(&ct.CoinsAction{Value: g, Ty: ct.CoinsActionGenesis})
	ret = append(ret, &tx0)

	// 初始化挖矿
	for _, genesis := range client.conf.Genesis {
		tx1 := createTicket(cfg, genesis.MinerAddr, genesis.ReturnAddr, genesis.BlsAddr, genesis.Count, 0)
		ret = append(ret, tx1...)
	}
	return ret
}

// write block to chain
func (client *Client) setBlock(b *types.Block) error {
	cb := client.GetCurrentBlock()
	if cb.Height >= b.Height {
		return nil
	}
	lastBlock, err := client.RequestBlock(b.Height - 1)
	if err != nil {
		return err
	}
	err = client.WriteBlock(lastBlock.StateHash, b)
	if err != nil {
		plog.Error("writeBlock error", "err", err)
		return err
	}
	return nil
}

func getMiner(b *types.Block) (*pt.Pos33MinerMsg, error) {
	if b == nil {
		return nil, fmt.Errorf("b is nil")
	}
	if len(b.Txs) == 0 {
		plog.Error("No tx in the block", b.Height)
		return nil, errors.New("no tx in the block")
	}
	tx := b.Txs[0]
	var pact pt.Pos33TicketAction
	err := types.Decode(tx.Payload, &pact)
	if err != nil {
		return nil, err
	}
	miner := pact.GetMiner()
	if miner == nil {
		return nil, errors.New("tx0 is NOT miner tx")
	}
	return miner, nil
}

// Get used search block store db
func (client *Client) Get(key []byte) ([]byte, error) {
	query := &types.LocalDBGet{Keys: [][]byte{key}}
	msg := client.GetQueueClient().NewMessage("blockchain", types.EventLocalGet, query)
	client.GetQueueClient().Send(msg, true)
	resp, err := client.GetQueueClient().Wait(msg)

	if err != nil {
		plog.Error(err.Error()) //no happen for ever
		return nil, err
	}
	value := resp.GetData().(*types.LocalReplyValue).Values[0]
	if value == nil {
		return nil, types.ErrNotFound
	}
	return value, nil
}

// CmpBestBlock 比较newBlock是不是最优区块
func (client *Client) CmpBestBlock(newBlock *types.Block, cmpBlock *types.Block) bool {
	pb, err := client.RequestBlock(newBlock.Height - 1)
	if err != nil {
		plog.Error("block cmp error", "err", err)
		return false
	}
	err = client.BlockCheck(pb, newBlock)
	if err != nil {
		plog.Error("block cmp error", "err", err)
		return false
	}
	m1, err := getMiner(newBlock)
	if err != nil {
		return false
	}
	m2, err := getMiner(cmpBlock)
	if err != nil {
		return true
	}

	cb := client.GetCurrentBlock()
	if cb.Height > newBlock.Height {
		return false
	}

	plog.Debug("block cmp", "nv1", len(m1.BlsPkList), "nv2", len(m2.BlsPkList))
	return true

	// vw1 := voteWeight(m1.Votes)
	// vw2 := voteWeight(m2.Votes)
	// if int(vw1) == int(vw2) {
	// 	return string(m1.Sort.SortHash.Hash) < string(m2.Sort.SortHash.Hash)
	// }
	// return vw1 > vw2
}

// Query_FlushTicket ticket query flush ticket function
func (client *Client) Query_FlushPos33Ticket(req *types.ReqNil) (types.Message, error) {
	return &types.Reply{IsOk: true, Msg: []byte("OK")}, nil
}
