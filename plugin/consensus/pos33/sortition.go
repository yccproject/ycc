package pos33

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/33cn/chain33/common/address"
	"github.com/33cn/chain33/common/crypto"
	vrf "github.com/33cn/chain33/common/vrf/secp256k1"
	"github.com/33cn/chain33/types"
	secp256k1 "github.com/btcsuite/btcd/btcec"
	"github.com/golang/protobuf/proto"
	pt "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

const diffValue = 1.0

var max = big.NewInt(0).Exp(big.NewInt(2), big.NewInt(256), nil)
var fmax = big.NewFloat(0).SetInt(max) // 2^^256

const (
	Maker = iota
	Voter
)

// func hash2BlsSk(hash []byte) bls33.PrivKeyBLS {
// 	var h [32]byte
// 	copy(h[:], hash)
// 	re := bls.HashSecretKey(h).ToRepr()
// 	sk := g1pubs.KeyFromFQRepr(re)
// 	return sk.Serialize()
// }

// 算法依据：
// 1. 通过签名，然后hash，得出的Hash值是在[0，max]的范围内均匀分布并且随机的, 那么Hash/max实在[1/max, 1]之间均匀分布的
// 2. 那么从N个选票中抽出M个选票，等价于计算N次Hash, 并且Hash/max < M/N

func calcuVrfHash2(input proto.Message, blsSk crypto.PrivKey) ([]byte, []byte) {
	in := types.Encode(input)
	sig := blsSk.Sign(in)
	return crypto.Sha256(sig.Bytes())[:], sig.Bytes()
}

func calcuVrfHash(input proto.Message, priv crypto.PrivKey) ([]byte, []byte) {
	privKey, _ := secp256k1.PrivKeyFromBytes(secp256k1.S256(), priv.Bytes())
	vrfPriv := &vrf.PrivateKey{PrivateKey: (*ecdsa.PrivateKey)(privKey)}
	in := types.Encode(input)
	vrfHash, vrfProof := vrfPriv.Evaluate(in)
	return vrfHash[:], vrfProof
}

func (n *node) voterSort(seed []byte, height int64, round, ty, num int) []*pt.Pos33SortMsg {
	count := n.myCount()
	priv := n.getPriv()
	if priv == nil {
		return nil
	}

	diff := n.getDiff(height, round, false)

	input := &pt.VrfInput{Seed: seed, Height: height, Round: int32(round), Ty: int32(ty)}
	vrfHash, vrfProof := calcuVrfHash(input, priv)
	proof := &pt.HashProof{
		Input: input,
		// Diff:     diff,
		VrfHash:  vrfHash,
		VrfProof: vrfProof,
		Pubkey:   priv.PubKey().Bytes(),
	}

	var msgs []*pt.Pos33SortMsg
	for i := 0; i < count; i++ {
		data := fmt.Sprintf("%x+%d+%d", vrfHash, i, num)
		hash := hash2([]byte(data))

		// 转为big.Float计算，比较难度diff
		y := new(big.Int).SetBytes(hash)
		z := new(big.Float).SetInt(y)
		if new(big.Float).Quo(z, fmax).Cmp(big.NewFloat(diff)) > 0 {
			continue
		}

		// 符合，表示抽中了
		m := &pt.Pos33SortMsg{
			SortHash: &pt.SortHash{Hash: hash, Index: int64(i), Num: int32(num)},
			Proof:    proof,
		}
		msgs = append(msgs, m)
	}

	if len(msgs) == 0 {
		return nil
	}
	if len(msgs) > pt.Pos33VoterSize {
		sort.Sort(pt.Sorts(msgs))
		msgs = msgs[:pt.Pos33VoterSize]
	}
	plog.Info("voter sort", "height", height, "round", round, "num", num, "mycount", count, "n", len(msgs), "diff", diff*1000000, "addr", address.PubKeyToAddr(proof.Pubkey)[:16])
	return msgs
}

func (n *node) makerSort(seed []byte, height int64, round int) *pt.Pos33SortMsg {
	count := n.myCount()
	priv := n.getPriv()
	if priv == nil {
		return nil
	}

	diff := n.getDiff(height, round, true)
	input := &pt.VrfInput{Seed: seed, Height: height, Round: int32(round), Ty: int32(0)}
	vrfHash, vrfProof := calcuVrfHash(input, priv)
	proof := &pt.HashProof{
		Input: input,
		// Diff:     diff,
		VrfHash:  vrfHash,
		VrfProof: vrfProof,
		Pubkey:   priv.PubKey().Bytes(),
	}

	var minSort *pt.Pos33SortMsg
	// for j := 0; j < 3; j++ {
	for i := 0; i < count; i++ {
		data := fmt.Sprintf("%x+%d+%d", vrfHash, i, 0)
		hash := hash2([]byte(data))

		// 转为big.Float计算，比较难度diff
		y := new(big.Int).SetBytes(hash)
		z := new(big.Float).SetInt(y)
		if new(big.Float).Quo(z, fmax).Cmp(big.NewFloat(diff)) > 0 {
			continue
		}

		// 符合，表示抽中了
		m := &pt.Pos33SortMsg{
			SortHash: &pt.SortHash{Hash: hash, Index: int64(i), Num: 0, Time: time.Now().UnixNano() / 1000000},
			Proof:    proof,
		}
		if minSort == nil {
			minSort = m
		}
		// minHash use string compare, define a rule for which one is min
		if string(minSort.SortHash.Hash) > string(hash) {
			minSort = m
		}
	}
	// }
	plog.Info("maker sort", "height", height, "round", round, "mycount", count, "diff", diff*1000000, "addr", address.PubKeyToAddr(proof.Pubkey)[:16], "sortHash", minSort != nil)
	return minSort
}

func vrfVerify(pub []byte, input []byte, proof []byte, hash []byte) error {
	pubKey, err := secp256k1.ParsePubKey(pub, secp256k1.S256())
	if err != nil {
		plog.Error("vrfVerify", "err", err)
		return pt.ErrVrfVerify
	}
	vrfPub := &vrf.PublicKey{PublicKey: (*ecdsa.PublicKey)(pubKey)}
	vrfHash, err := vrfPub.ProofToHash(input, proof)
	if err != nil {
		plog.Error("vrfVerify", "err", err)
		return pt.ErrVrfVerify
	}
	// plog.Debug("vrf verify", "ProofToHash", fmt.Sprintf("(%x, %x): %x", input, proof, vrfHash), "hash", hex.EncodeToString(hash))
	if !bytes.Equal(vrfHash[:], hash) {
		plog.Error("vrfVerify", "err", fmt.Errorf("invalid VRF hash"))
		return pt.ErrVrfVerify
	}
	return nil
}

var errDiff = errors.New("diff error")

func (n *node) queryDeposit(addr string) (*pt.Pos33DepositMsg, error) {
	resp, err := n.GetAPI().Query(pt.Pos33TicketX, "Pos33Deposit", &types.ReqAddr{Addr: addr})
	if err != nil {
		return nil, err
	}
	reply := resp.(*pt.Pos33DepositMsg)
	return reply, nil
}

func (n *node) verifySort(height int64, ty int, seed []byte, m *pt.Pos33SortMsg) error {
	if height <= pt.Pos33SortBlocks {
		return nil
	}
	if m == nil || m.Proof == nil || m.SortHash == nil || m.Proof.Input == nil {
		return fmt.Errorf("verifySort error: sort msg is nil")
	}

	addr := address.PubKeyToAddr(m.Proof.Pubkey)
	d, err := n.queryDeposit(addr)
	if err != nil {
		return err
	}
	count := d.Count
	if d.CloseHeight >= height-pt.Pos33SortBlocks {
		count = d.PreCount
	}
	if count <= m.SortHash.Index {
		return fmt.Errorf("sort index %d > %d your count, height %d, close-height %d, precount %d", m.SortHash.Index, count, height, d.CloseHeight, d.PreCount)
	}

	if m.Proof.Input.Height != height {
		return fmt.Errorf("verifySort error, height NOT match: %d!=%d", m.Proof.Input.Height, height)
	}
	if string(m.Proof.Input.Seed) != string(seed) {
		return fmt.Errorf("verifySort error, seed NOT match")
	}
	if m.Proof.Input.Ty != int32(ty) {
		return fmt.Errorf("verifySort error, step NOT match")
	}

	round := m.Proof.Input.Round
	input := &pt.VrfInput{Seed: seed, Height: height, Round: round, Ty: int32(ty)}
	in := types.Encode(input)
	err = vrfVerify(m.Proof.Pubkey, in, m.Proof.VrfProof, m.Proof.VrfHash)
	if err != nil {
		plog.Info("vrfVerify error", "err", err, "height", height, "round", round, "ty", ty, "who", addr[:16])
		return err
	}
	data := fmt.Sprintf("%x+%d+%d", m.Proof.VrfHash, m.SortHash.Index, m.SortHash.Num)
	hash := hash2([]byte(data))
	if string(hash) != string(m.SortHash.Hash) {
		return fmt.Errorf("sort hash error")
	}

	diff := n.getDiff(height, int(round), ty == 0)

	y := new(big.Int).SetBytes(hash)
	z := new(big.Float).SetInt(y)
	if new(big.Float).Quo(z, fmax).Cmp(big.NewFloat(diff)) > 0 {
		plog.Error("verifySort diff error", "height", height, "ty", ty, "round", round, "diff", diff*1000000, "addr", address.PubKeyToAddr(m.Proof.Pubkey))
		return errDiff
	}

	return nil
}

func hash2(data []byte) []byte {
	return crypto.Sha256(crypto.Sha256(data))
}
