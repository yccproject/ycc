package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	// "math"
	"math/rand"
	"os"
	"time"

	"github.com/33cn/chain33/common"
	"github.com/33cn/chain33/common/address"
	"github.com/33cn/chain33/common/crypto"
	grpc "github.com/33cn/chain33/rpc/grpcclient"
	jrpc "github.com/33cn/chain33/rpc/jsonclient"
	rpctypes "github.com/33cn/chain33/rpc/types"
<<<<<<< HEAD
=======
	"github.com/33cn/chain33/system/crypto/secp256k1"
>>>>>>> master
	ctypes "github.com/33cn/chain33/system/dapp/coins/types"
	"github.com/33cn/chain33/types"
	_ "github.com/33cn/plugin/plugin"
)

//
// test auto generate tx and send to the node
//

var rootKey crypto.PrivKey

func init() {
	rand.Seed(time.Now().UnixNano())
	rootKey = HexToPrivkey("CC38546E9E659D15E6B4893F0AB32A06D103931A8230B0BDE71459D2B27D6944")
}

var rpcURL = flag.String("u", "http://localhost:7901", "rpc url")
var grpcURL = flag.String("g", "127.0.0.1:7902", "grpc url")
var pnodes = flag.Bool("n", false, "only print node private keys")
var ini = flag.Bool("i", false, "send init tx")
var maxacc = flag.Int("a", 10000, "max account")
var maxaccF = flag.Int("m", 1000000, "max account in a file")
var rn = flag.Int("r", 3000, "sleep in Microsecond")
var conf = flag.String("c", "ycc.toml", "chain33 config file")
var useGrpc = flag.Bool("G", false, "if use grpc")
var sign = flag.Bool("s", true, "signature tx")
var accFile = flag.String("f", "acc.dat", "acc file")

var gClient types.Chain33Client
var jClient *jrpc.JSONClient
var config *types.Chain33Config

func main() {
	flag.Parse()
	config = types.NewChain33Config(types.MergeCfg(types.ReadFile(*conf), ""))
	// config.EnableCheckFork(false)

	privCh := runLoadAccounts(*accFile, *maxacc)
	if *pnodes {
		return
	}
	if privCh == nil {
		log.Fatal("NO account !!!")
		return
	}

	gclient, err := grpc.NewMainChainClient(config, *grpcURL)
	if err != nil {
		log.Fatal(err)
	}
	gClient = gclient

	jclient, err := jrpc.NewJSONClient(*rpcURL)
	if err != nil {
		log.Fatal(err)
	}
	jClient = jclient

	if *ini {
		log.Println("@@@@@@@ send init txs", *ini)
		runSendInitTxs(privCh)
	} else {
		var privs []crypto.PrivKey
		for {
			priv, ok := <-privCh
			if !ok {
				break
			}
			privs = append(privs, priv)

		}
		run(privs)
	}
}

// Int is int64
type Int int64

// Marshal Int to []byte
func (i Int) Marshal() []byte {
	b := make([]byte, 16)
	n := binary.PutVarint(b, int64(i))
	return b[:n]
}

// Unmarshal []byte to Int
func (i *Int) Unmarshal(b []byte) (int, error) {
	a, n := binary.Varint(b)
	*i = Int(a)
	return n, nil
}

type pp struct {
	i int
	p crypto.PrivKey
}

// Tx is alise types.Transaction
type Tx = types.Transaction

func run(privs []crypto.PrivKey) {
<<<<<<< HEAD
	hch := make(chan int64, 100)
	ch := generateTxs(privs, hch)
	tch := time.NewTicker(time.Second * 10).C
	h, err := gClient.GetLastHeader(context.Background(), &types.ReqNil{})
	if err != nil {
		panic(err)
	}
	height := h.Height
	hch <- height
	i := 0
	max := 256
	txs := make([]*types.Transaction, max)
	for tx := range ch {
=======
	hch := make(chan int64, 10)
	hch <- 0
	ch := generateTxs(privs, hch)
	tch := time.NewTicker(time.Second * 10).C
	i := 0
	height := int64(0)
	txs := make([]*types.Transaction, 0, 200)
	for {
>>>>>>> master
		select {
		case <-tch:
			if *useGrpc {
				data := &types.ReqNil{}
				h, err := gClient.GetLastHeader(context.Background(), data)
				if err != nil {
					panic(err)
				}
				height = h.Height
			} else {
				var res rpctypes.Header
				err := jClient.Call("Chain33.GetLastHeader", nil, &res)
				if err != nil {
					panic(err)
				}
				height = res.Height
			}
<<<<<<< HEAD
		default:
		}
		hch <- height

		j := i % max
		txs[j] = tx
		i++
		if j == max-1 {
			sendTxs(txs)
		}
		if i%1000 == 0 {
			log.Println(i, "... txs sent")
=======
		case tx := <-ch:
			txs = append(txs, tx)
			if len(txs) == 200 {
				sendTxs(txs)
				txs = nil
				hch <- height
				//time.Sleep(time.Microsecond * time.Duration(*rn))
				i++
				if i%1000 == 0 {
					log.Println(i, "... txs sent")
				}
			}
>>>>>>> master
		}
	}
}

func generateTxs(privs []crypto.PrivKey, hch <-chan int64) chan *Tx {
	N := 4
	l := len(privs) - 1
	ch := make(chan *Tx, N)
	f := func() {
		for {
			i := rand.Intn(len(privs))
			signer := privs[l-i]
<<<<<<< HEAD
			ch <- newTxWithTxHeight(signer, 1, address.PubKeyToAddress(privs[i].PubKey().Bytes()).String(), <-hch)
			// ch <- newTx(signer, 1, address.PubKeyToAddress(privs[i].PubKey().Bytes()).String())
=======
			ch <- newTxWithTxHeight(signer, 1, address.PubKeyToAddr(address.DefaultID, privs[i].PubKey().Bytes()).String(), <-hch)
>>>>>>> master
		}
	}
	for i := 0; i < N; i++ {
		go f()
	}
	return ch
}

func sendTxs(txs []*Tx) error {
	ts := &types.Transactions{Txs: txs}
	var err error
	if *useGrpc {
<<<<<<< HEAD
		_, err := gClient.SendTransactions(context.Background(), ts)
		if err != nil {
			panic(err)
		}
	} else {
		err = errors.New("not support grpc in batch send txs")
		panic(err)
=======
		_, err = gClient.SendTransactions(context.Background(), ts)
	} else {
		return errors.New("not support grpc in batch send txs")
>>>>>>> master
		// err = jClient.Call("Chain33.SendTransaction", &rpctypes.RawParm{Data: common.ToHex(types.Encode(tx))}, &txHash)
	}
	if err != nil {
		log.Println("@@@ rpc error: ", err)
		return err
	}
	return nil
}

// _, ok := err.(*json.InvalidUnmarshalError)
func sendTx(tx *Tx) error {
	var err error
	if *useGrpc {
		_, err = gClient.SendTransaction(context.Background(), tx)
	} else {
		var txHash string
		err = jClient.Call("Chain33.SendTransaction", &rpctypes.RawParm{Data: common.ToHex(types.Encode(tx))}, &txHash)
	}
	if err != nil {
		// _, ok := err.(*json.InvalidUnmarshalError)
		// if ok {
		// 	break
		// }
		// if err == types.ErrFeeTooLow {
		// 	log.Println("@@@ rpc error: ", err, tx.From(), tx.Fee)
		// 	tx.Fee *= 2
		// 	continue
		// }
		log.Println("@@@ rpc error: ", err, common.HashHex(tx.Hash()))
	}
	return nil
}

func runSendInitTxs(privCh chan crypto.PrivKey) {
	ch := make(chan *Tx, 16)
	go runGenerateInitTxs(privCh, ch)
<<<<<<< HEAD
	max := 256
	i := 0
	txs := make([]*types.Transaction, max)
=======
	i := 0
	txs := make([]*types.Transaction, 0)
>>>>>>> master
	for {
		tx, ok := <-ch
		if !ok {
			log.Println("init txs finished:", i)
			break
		}
<<<<<<< HEAD
		j := i % max
		txs[j] = tx
=======
>>>>>>> master
		i++
		if i%100 == 0 {
			log.Println("send init txs:", i, len(txs))
		}
<<<<<<< HEAD
		if j == max-1 {
			sendTxs(txs)
=======
		txs = append(txs, tx)
		if len(txs) == 200 {
			sendTxs(txs)
			txs = make([]*types.Transaction, 0)
>>>>>>> master
		}
	}
}

func newTxWithTxHeight(priv crypto.PrivKey, amount int64, to string, height int64) *Tx {
	act := &ctypes.CoinsAction{Value: &ctypes.CoinsAction_Transfer{Transfer: &types.AssetsTransfer{Cointoken: "YCC", Amount: amount}}, Ty: ctypes.CoinsActionTransfer}
	payload := types.Encode(act)
	tx, err := types.CreateFormatTx(config, "coins", payload)
	if err != nil {
		panic(err)
	}
<<<<<<< HEAD
	// tx.Fee, err = tx.GetRealFee(config.GetMinTxFeeRate())
	// if err != nil {
	// 	panic(err)
	// }
	tx.To = to
	tx.Expire = height + 20 + types.TxHeightFlag
	tx.ChainID = 999
	tx.Fee *= 100
=======
	tx.Fee = config.GetMaxTxFee()
	tx.To = to
	tx.Expire = height + 20 + types.TxHeightFlag
>>>>>>> master
	if *sign {
		tx.Sign(types.SECP256K1, priv)
	}
	return tx
}

func newTx(priv crypto.PrivKey, amount int64, to string) *Tx {
	act := &ctypes.CoinsAction{Value: &ctypes.CoinsAction_Transfer{Transfer: &types.AssetsTransfer{Cointoken: "YCC", Amount: amount}}, Ty: ctypes.CoinsActionTransfer}
	payload := types.Encode(act)
	tx, err := types.CreateFormatTx(config, "coins", payload)
	if err != nil {
		panic(err)
	}
<<<<<<< HEAD
	// tx.Fee, err = tx.GetRealFee(config.GetMinTxFeeRate())
	// if err != nil {
	// 	panic(err)
	// }
	tx.Fee *= 100
	tx.To = to
	tx.ChainID = 999
=======
	tx.Fee = config.GetMaxTxFee()
	tx.To = to
>>>>>>> master
	if *sign {
		tx.Sign(types.SECP256K1, priv)
	}
	return tx
}

//HexToPrivkey ： convert hex string to private key
func HexToPrivkey(key string) crypto.PrivKey {
<<<<<<< HEAD
	cr, err := crypto.Load(types.GetSignName("", types.SECP256K1), -1)
=======
	cr, err := crypto.New(secp256k1.Name)
>>>>>>> master
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

func runGenerateInitTxs(privCh chan crypto.PrivKey, ch chan *Tx) {
	for {
		priv, ok := <-privCh
		if !ok {
			log.Println("privCh is closed")
			close(ch)
			return
		}
<<<<<<< HEAD
		m := 1000 * types.DefaultCoinPrecision
		ch <- newTx(rootKey, m, address.PubKeyToAddress(priv.PubKey().Bytes()).String())
	}
}

// func generateInitTxs(n int, privs []crypto.PrivKey, ch chan *Tx, done chan struct{}) {
// 	for _, priv := range privs {
// 		select {
// 		case <-done:
// 			return
// 		default:
// 		}

// 		m := 100 * types.DefaultCoinPrecision
// 		ch <- newTx(rootKey, m, address.PubKeyToAddress(priv.PubKey().Bytes()).String())
// 	}
// 	log.Println(n, len(privs))
// }
=======
		m := 100 * types.DefaultCoinPrecision
		ch <- newTx(rootKey, m, address.PubKeyToAddr(address.DefaultID, priv.PubKey().Bytes()).String())
	}
}
func generateInitTxs(n int, privs []crypto.PrivKey, ch chan *Tx, done chan struct{}) {
	for _, priv := range privs {
		select {
		case <-done:
			return
		default:
		}

		m := 100 * types.DefaultCoinPrecision
		ch <- newTx(rootKey, m, address.PubKeyToAddr(address.DefaultID, priv.PubKey().Bytes()).String())
	}
	log.Println(n, len(privs))
}
>>>>>>> master

//
func runGenerateAccounts(max int, pksCh chan []crypto.PrivKey) {
	log.Println("generate accounts begin:")

	t := time.Now()
	goN := 16
	ch := make(chan pp, goN)
	done := make(chan struct{}, 1)

<<<<<<< HEAD
	c, _ := crypto.Load(types.GetSignName("", types.SECP256K1), -1)
=======
	c, _ := crypto.New(secp256k1.Name)
>>>>>>> master
	for i := 0; i < goN; i++ {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					priv, _ := c.GenKey()
					ch <- pp{i: 0, p: priv}
				}
			}
		}()
	}
	all := 0
	var pks []crypto.PrivKey
	for {
		p := <-ch
		pks = append(pks, p.p)
		l := len(pks)
		if l%1000 == 0 && l > 0 {
			all += 1000
			log.Println("generate acc:", all, *maxaccF, l)
			if l%(*maxaccF) == 0 {
				pksCh <- pks
				log.Println(time.Since(t))
				pks = nil
			}
		}
		if all == max {
			close(done)
			break
		}
	}
	log.Println("generate accounts end", time.Since(t))
	close(pksCh)
}

func loadAccounts(filename string, max int) []crypto.PrivKey {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Println(err)
		i := 0
		ss := strings.Split(filename, ".")
		pksCh := make(chan []crypto.PrivKey, 10)

		go runGenerateAccounts(max, pksCh)
		for {
			privs := <-pksCh
			if privs == nil {
				log.Println("xxxxxx")
				return nil
			}
			i++
			fname := ss[0] + fmt.Sprintf("%d", i) + "." + ss[1]
			f, err := os.OpenFile(fname, os.O_WRONLY|os.O_CREATE, os.ModePerm)
			if err != nil {
				log.Fatal(err)
			}
			defer f.Close()

			var data []byte
			data = append(data, Int(len(privs)).Marshal()...)
			f.Write(data)
			for _, p := range privs {
				f.Write(p.Bytes())
			}
			log.Println("write acc file", fname)
		}
	}
	var l Int
	n, err := l.Unmarshal(b)
	if err != nil {
		log.Fatal(err)
	}
	ln := int(l)
	b = b[n:]

	if max < ln {
		ln = max
	}

	privs := make([]crypto.PrivKey, ln)

	n = 32
<<<<<<< HEAD
	c, _ := crypto.Load(types.GetSignName("", types.SECP256K1), -1)
=======
	c, _ := crypto.New(secp256k1.Name)
>>>>>>> master
	for i := 0; i < ln; i++ {
		p := b[:n]
		priv, err := c.PrivKeyFromBytes(p)
		if err != nil {
			log.Fatal(err)
		}
		privs[i] = priv
		b = b[n:]
		if i%10000 == 0 {
			log.Println("load acc:", i)
		}
<<<<<<< HEAD
		log.Println("account: ", address.PubKeyToAddr(priv.PubKey().Bytes()))
=======
		log.Println("account: ", address.PubKeyToAddr(address.DefaultID, priv.PubKey().Bytes()))
>>>>>>> master
	}
	log.Println("loadAccount: ", len(privs))
	return privs
}

func runLoadAccounts(filename string, max int) chan crypto.PrivKey {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Println(err)
		i := 0
		ss := strings.Split(filename, ".")
		pksCh := make(chan []crypto.PrivKey, 1)

		go runGenerateAccounts(max, pksCh)
		for {
			privs := <-pksCh
			if privs == nil {
				log.Println("xxxxxx")
				return nil
			}
			i++
			fname := ss[0] + fmt.Sprintf("%d", i) + "." + ss[1]
			f, err := os.OpenFile(fname, os.O_WRONLY|os.O_CREATE, os.ModePerm)
			if err != nil {
				log.Fatal(err)
			}
			defer f.Close()

			var data []byte
			data = append(data, Int(len(privs)).Marshal()...)
			f.Write(data)
			for _, p := range privs {
				f.Write(p.Bytes())
			}
			log.Println("write acc file", fname)
		}
	}
	var l Int
	n, err := l.Unmarshal(b)
	if err != nil {
		log.Fatal(err)
	}
	ln := int(l)
	b = b[n:]

	if max < ln {
		ln = max
	}

	privCh := make(chan crypto.PrivKey, 1024)

	go func() {
		n = 32
<<<<<<< HEAD
		c, _ := crypto.Load(types.GetSignName("", types.SECP256K1), -1)
=======
		c, _ := crypto.New(secp256k1.Name)
>>>>>>> master
		for i := 0; i < ln; i++ {
			p := b[:n]
			priv, err := c.PrivKeyFromBytes(p)
			if err != nil {
				log.Fatal(err)
			}
			privCh <- priv
			b = b[n:]
			if i%10000 == 0 {
				log.Println("load acc:", i)
			}
		}
		close(privCh)
	}()
	return privCh
}
