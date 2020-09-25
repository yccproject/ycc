package main

import (
	"flag"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/33cn/chain33/common"
	jrpc "github.com/33cn/chain33/rpc/jsonclient"
	rpctypes "github.com/33cn/chain33/rpc/types"
	ty "github.com/33cn/plugin/plugin/dapp/token/types"

	"github.com/33cn/chain33/common/crypto"
	"github.com/33cn/chain33/system/crypto/secp256k1"
	"github.com/33cn/chain33/types"
)

var rpcURL = flag.String("u", "http://localhost:8801", "rpc url")
var config = flag.String("f", "chain33.toml", "chain33 config file")
var count = flag.Int("c", 1000000, "token count")

var jClient *jrpc.JSONClient

const ownerAddr = "1EDnnePAZN48aC2hiTDzhkczfF39g1pZZX"
const ownerPriv = "2116459C0EC8ED01AA0EEAE35CAC5C96F94473F7816F114873291217303F6989"

func main() {
	flag.Parse()

	config := types.NewChain33Config(types.MergeCfg(types.ReadFile(*config), ""))
	config.EnableCheckFork(false)

	jclient, err := jrpc.NewJSONClient(*rpcURL)
	if err != nil {
		log.Fatal(err)
	}
	jClient = jclient

	priv := HexToPrivkey(ownerPriv)

	c := make(chan string, 1024)

	go func() {
		for i := 0; i < *count; i++ {
			symbol := newSymbol()
			tx, err := newPreTx(symbol, ownerAddr)
			if err != nil {
				log.Fatal(err)
			}
			log.Println("newPreCreatedTx:", tx)
			s, err := signAndSendTx(symbol, tx, priv)
			if err != nil {
				log.Fatal(err)
			}
			log.Println("sendPreCreateTx:", tx)
			c <- s
			if i > 0 && i%10 == 0 {
				time.Sleep(time.Second * 1)
			}
		}
		close(c)
	}()
	time.Sleep(time.Second * 3)

	i := 0
	for s := range c {
		ss := strings.Split(s, ":")
		hash := ss[0]
		err := queueTx(hash)
		if err != nil {
			log.Println(err)
			go func() {
				time.Sleep(time.Second)
				c <- s
			}()
			continue
		}
		log.Println("queueTx:", hash)
		symbol := ss[1]
		tx, err := newFinishTx(symbol, ownerAddr)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("newFinishTx:", tx)
		tx, err = signAndSendTx(symbol, tx, priv)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("sendFinishTx:", tx)
		i++
		if i > 0 && i%10 == 0 {
			time.Sleep(time.Second * 1)
		}
	}
}

//HexToPrivkey ： convert hex string to private key
func HexToPrivkey(key string) crypto.PrivKey {
	cr, err := crypto.New(secp256k1.Name)
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

type Tx = types.Transaction

func newSymbol() string {
	const str = "ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"
	var symbol []byte
	for i := 0; i < 4; i++ {
		n := rand.Intn(26)
		symbol = append(symbol, str[n])
	}
	return string(symbol)
}

func newPreTx(symbol, owner string) (string, error) {
	param := ty.TokenPreCreate{
		Name:     symbol,
		Symbol:   symbol,
		Price:    100,
		Category: 1,
		Total:    10000 * types.Coin,
		Owner:    owner,
	}
	var txhex string
	err := jClient.Call("token.CreateRawTokenPreCreateTx", param, &txhex)
	if err != nil {
		return "", err
	}

	return txhex, nil
}

func signAndSendTx(symbol, txhex string, priv crypto.PrivKey) (string, error) {
	txbytes, err := common.FromHex(txhex)
	if err != nil {
		return "", err
	}
	tx := &types.Transaction{}
	err = types.Decode(txbytes, tx)
	if err != nil {
		return "", err
	}
	tx.Fee = 1e6
	tx.Sign(types.SECP256K1, priv)

	var txHash string
	err = jClient.Call("Chain33.SendTransaction", &rpctypes.RawParm{Data: common.ToHex(types.Encode(tx))}, &txHash)
	return txHash + ":" + symbol, err
}

func newFinishTx(symbol, owner string) (string, error) {
	param := ty.TokenPreCreate{
		Symbol: symbol,
		Owner:  owner,
	}
	var txhex string
	err := jClient.Call("token.CreateRawTokenFinishTx", param, &txhex)
	if err != nil {
		return "", err
	}

	return txhex, nil
}

func queueTx(txhash string) error {
	param := &rpctypes.QueryParm{
		Hash: txhash,
	}

	var txd rpctypes.TransactionDetail
	err := jClient.Call("Chain33.QueryTransaction", param, &txd)
	if err != nil {
		return err
	}
	return nil
}
