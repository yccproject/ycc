package pos33

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/33cn/chain33/common/crypto"
	"github.com/33cn/chain33/common/merkle"
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

	clock   sync.Mutex
	priv    crypto.PrivKey
	mycount int

	mlock sync.Mutex
	acMap map[int64]int

	done chan struct{}
}

// Tx is ...
type Tx = types.Transaction

type genesisTicket struct {
	MinerAddr  string `json:"minerAddr"`
	ReturnAddr string `json:"returnAddr"`
	Count      int32  `json:"count"`
}

type subConfig struct {
	Genesis          []*genesisTicket `json:"genesis"`
	GenesisBlockTime int64            `json:"genesisBlockTime"`
	ListenPort       int              `json:"listenPort,omitempty"`
	BootPeers        []string         `json:"bootPeers,omitempty"`
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
	plog.Debug("subcfg", "cfg", string(sub))

	n := newNode(&subcfg)
	client := &Client{BaseClient: c, n: n, conf: &subcfg, acMap: make(map[int64]int), done: make(chan struct{})}
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

func (client *Client) newBlock(lastBlock *types.Block, txs []*types.Transaction, height int64) (*types.Block, error) {
	if lastBlock.Height+1 != height {
		plog.Error("newBlock height error", "lastHeight", lastBlock.Height, "height", height)
		return nil, fmt.Errorf("the last block too low")
	}

	bt := time.Now().Unix()
	if bt < lastBlock.GetBlockTime() {
		bt = lastBlock.GetBlockTime()
	}

	cfg := client.GetAPI().GetConfig()
	nb := &types.Block{
		ParentHash: lastBlock.Hash(cfg),
		Height:     lastBlock.Height + 1,
		BlockTime:  bt,
	}

	maxTxs := int(cfg.GetP(height).MaxTxNumber)
	txs = append(txs, client.RequestTx(maxTxs, nil)...)
	txs = client.AddTxsToBlock(nb, txs)

	nb.Txs = txs
	nb.TxHash = merkle.CalcMerkleRoot(cfg, lastBlock.Height, txs)
	return nb, nil
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

func (client *Client) myCount() int {
	client.clock.Lock()
	defer client.clock.Unlock()
	return client.mycount
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
	cr, err := crypto.New(types.GetSignName("", types.SECP256K1))
	if err != nil {
		return nil, err
	}
	if privkey == nil {
		return nil, errors.New("null privKey")
	}
	return cr.PrivKeyFromBytes(privkey)
}

func (client *Client) getPriv() crypto.PrivKey {
	client.clock.Lock()
	defer client.clock.Unlock()
	if client.priv == nil {
		plog.Error("Wallet LOCKED or not Set mining account")
		return nil
	}
	return client.priv
}

func (c *Client) AddBlock(b *types.Block) error {
	c.updateTicketCount(b.Height)
	c.n.addBlock(b)
	return nil
}

func (c *Client) updateTicketCount(height int64) {
	c.mlock.Lock()
	defer c.mlock.Unlock()
	ac := c.queryAllPos33Count()
	c.acMap[height] = ac
	c.mycount = c.getMyCount()
	plog.Debug("getAllCount", "count", ac, "height", height)
	delete(c.acMap, height-pt.Pos33SortitionSize-1)
}

func (c *Client) getMyCount() int {
	c.clock.Lock()
	defer c.clock.Unlock()
	resp, err := c.GetAPI().ExecWalletFunc("pos33", "WalletGetPos33Count", &types.ReqNil{})
	if err != nil {
		plog.Error("WalletGetPos33Count", "err", err)
		return 0
	}
	w := resp.(*pt.ReplyWalletPos33Count)
	c.mycount = int(w.Count)
	c.priv, err = privFromBytes(w.Privkey)
	if err != nil {
		plog.Error("privFromBytes", "err", err)
		return 0
	}
	plog.Debug("getMyCount", "count", c.mycount)
	return c.mycount
}

func (c *Client) queryAllPos33Count() int {
	msg, err := c.GetAPI().Query(pt.Pos33TicketX, "AllPos33TicketCount", &types.ReqNil{})
	if err != nil {
		plog.Debug("query Pos33AllPos33TicketCount error", "error", err)
		return 0
	}
	count := int(msg.(*types.Int64).Data)
	return count
}

// CreateBlock will start run
func (client *Client) CreateBlock() {
	b, err := client.RequestBlock(0)
	if err != nil {
		panic(err)
	}
	gtm := client.GetGenesisBlockTime()
	plog.Info("CreateBlock", "block 0 time", b.BlockTime, "genesis time", gtm)
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
			time.Sleep(time.Second)
			continue
		}
		break
	}
	client.n.runLoop()
}

func createTicket(cfg *types.Chain33Config, minerAddr, returnAddr string, count int32, height int64) (ret []*types.Transaction) {
	//给hotkey 10000 个币，作为miner的手续费
	tx1 := types.Transaction{}
	tx1.Execer = []byte("coins")
	tx1.To = minerAddr
	g := &ct.CoinsAction_Genesis{}
	g.Genesis = &types.AssetsGenesis{Amount: pt.GetPos33TicketMinerParam(cfg, height).Pos33TicketPrice}
	tx1.Payload = types.Encode(&ct.CoinsAction{Value: g, Ty: ct.CoinsActionGenesis})
	ret = append(ret, &tx1)

	// 发行并抵押
	tx2 := types.Transaction{}
	tx2.Execer = []byte("coins")
	tx2.To = driver.ExecAddress(pt.Pos33TicketX)
	g = &ct.CoinsAction_Genesis{}
	g.Genesis = &types.AssetsGenesis{Amount: int64(count) * pt.GetPos33TicketMinerParam(cfg, height).Pos33TicketPrice, ReturnAddress: returnAddr}
	tx2.Payload = types.Encode(&ct.CoinsAction{Value: g, Ty: ct.CoinsActionGenesis})
	ret = append(ret, &tx2)

	// 冻结资金并开启挖矿
	tx3 := types.Transaction{}
	tx3.Execer = []byte(pt.Pos33TicketX)
	tx3.To = driver.ExecAddress(pt.Pos33TicketX)
	gticket := &pt.Pos33TicketAction_Genesis{}
	gticket.Genesis = &pt.Pos33TicketGenesis{MinerAddress: minerAddr, ReturnAddress: returnAddr, Count: count}
	tx3.Payload = types.Encode(&pt.Pos33TicketAction{Value: gticket, Ty: pt.Pos33TicketActionGenesis})
	ret = append(ret, &tx3)
	plog.Debug("genesis miner", "execaddr", tx3.To)
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
	g.Genesis = &types.AssetsGenesis{Amount: types.MaxCoin * 10}
	tx0.Payload = types.Encode(&ct.CoinsAction{Value: g, Ty: ct.CoinsActionGenesis})
	ret = append(ret, &tx0)

	// 初始化挖矿
	cfg := client.GetAPI().GetConfig()
	for _, genesis := range client.conf.Genesis {
		tx1 := createTicket(cfg, genesis.MinerAddr, genesis.ReturnAddr, genesis.Count, 0)
		ret = append(ret, tx1...)
	}
	return ret
}

// write block to chain
func (client *Client) setBlock(b *types.Block) error {
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

func getMiner(b *types.Block) (*pt.Pos33TicketMiner, error) {
	if b == nil {
		return nil, fmt.Errorf("b is nil")
	}
	if len(b.Txs) == 0 {
		panic("No tx in the block")
	}
	tx := b.Txs[0]
	var pact pt.Pos33TicketAction
	err := types.Decode(tx.Payload, &pact)
	if err != nil {
		return nil, err
	}
	return pact.GetMiner(), nil
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

	plog.Info("block cmp", "nv1", len(m1.Votes), "nv2", len(m2.Votes))

	vw1 := voteWeight(m1.Votes)
	vw2 := voteWeight(m2.Votes)
	if int(vw1) == int(vw2) {
		return string(m1.Sort.SortHash.Hash) < string(m2.Sort.SortHash.Hash)
	}
	return vw1 > vw2
}

// Query_GetTicketCount ticket query ticket count function
func (client *Client) Query_GetPos33TicketCount(req *types.ReqNil) (types.Message, error) {
	var ret types.Int64
	ret.Data = int64(client.myCount())
	return &ret, nil
}

// Query_FlushTicket ticket query flush ticket function
func (client *Client) Query_FlushPos33Ticket(req *types.ReqNil) (types.Message, error) {
	client.getMyCount()
	return &types.Reply{IsOk: true, Msg: []byte("OK")}, nil
}

func (client *Client) Query_GetPos33Reward(req *pt.Pos33TicketReward) (types.Message, error) {
	var b *types.Block
	var err error
	if req.Height <= 0 {
		b, err = client.n.RequestLastBlock()
		if err != nil {
			return nil, err
		}
	} else {
		b, err = client.RequestBlock(req.Height)
		if err != nil {
			return nil, err
		}
	}
	m, err := getMiner(b)
	if err != nil {
		return nil, err
	}
	br := int64(0)
	vr := int64(0)
	addr := req.Addr
	if addr == "" {
		addr = saddr(b.Signature)
	}
	if saddr(b.Signature) == addr {
		br = pt.Pos33BpReward * int64(len(m.Votes))
	}
	for _, v := range m.Votes {
		if saddr(v.Sig) == addr {
			vr += pt.Pos33VoteReward
		}
	}
	return &pt.ReplyPos33TicketReward{VoterReward: vr, MinerReward: br}, nil
}
