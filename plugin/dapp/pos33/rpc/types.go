// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"github.com/33cn/chain33/rpc/types"
	ty "github.com/yccproject/ycc/plugin/dapp/pos33/types"
)

// Jrpc json rpc type
type Jrpc struct {
	cli *channelClient
}

// Grpc grpc type
type Grpc struct {
	*channelClient
}

type channelClient struct {
	types.ChannelClient
}

// Init initial
func Init(name string, s types.RPCServer) {
	cli := &channelClient{}
	grpc := &Grpc{channelClient: cli}
	cli.Init(name, s, &Jrpc{cli: cli}, grpc)
	ty.RegisterPos33Server(s.GRPC(), grpc)
}
