// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package types

import (
	"encoding/hex"
	"testing"

	"github.com/33cn/chain33/common/crypto"
	rpctypes "github.com/33cn/chain33/rpc/types"
	"github.com/33cn/chain33/types"
	"github.com/stretchr/testify/assert"
)

func init() {
	InitExecutor(nil)
}

func TestDecodeLogNewPos33Ticket(t *testing.T) {
	var logTmp = &ReceiptPos33Deposit{}

	dec := types.Encode(logTmp)

	strdec := hex.EncodeToString(dec)
	rlog := &rpctypes.ReceiptLog{
		Ty:  TyLogNewPos33Ticket,
		Log: "0x" + strdec,
	}

	logs := []*rpctypes.ReceiptLog{}
	logs = append(logs, rlog)

	var data = &rpctypes.ReceiptData{
		Ty:   5,
		Logs: logs,
	}
	result, err := rpctypes.DecodeLog([]byte("pos33"), data)
	assert.Nil(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "LogNewPos33Ticket", result.Logs[0].TyName)
}

func TestDecodeLogClosePos33Ticket(t *testing.T) {
	var logTmp = &ReceiptPos33Miner{}

	dec := types.Encode(logTmp)

	strdec := hex.EncodeToString(dec)
	rlog := &rpctypes.ReceiptLog{
		Ty:  TyLogClosePos33Ticket,
		Log: "0x" + strdec,
	}

	logs := []*rpctypes.ReceiptLog{}
	logs = append(logs, rlog)

	var data = &rpctypes.ReceiptData{
		Ty:   5,
		Logs: logs,
	}
	result, err := rpctypes.DecodeLog([]byte("pos33"), data)
	assert.Nil(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "LogClosePos33Ticket", result.Logs[0].TyName)
}

func TestHash2BlsSk(t *testing.T) {
	h1 := crypto.Sha256([]byte("1"))
	sk1 := Hash2BlsSk(h1)
	sk2 := Hash2BlsSk(h1)
	if !sk1.Equals(sk2) {
		t.Error("same hash must same sk")
	}
	h2 := crypto.Sha256([]byte("2"))
	sk3 := Hash2BlsSk(h2)
	if sk1.Equals(sk3) {
		t.Error("diffrent hash must diffrent sk")
	}
}
