package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/panjf2000/gnet"
)

type rd struct {
	data []byte
	addr string
}
type server struct {
	*gnet.EventServer
	connectedSockets sync.Map
	ch               chan rd
}

func (s *server) OnInitComplete(srv gnet.Server) (action gnet.Action) {
	log.Printf("forward server is listening on %s (multi-cores: %t)", srv.Addr.String(), srv.Multicore)
	return
}

func (s *server) OnOpened(c gnet.Conn) (out []byte, action gnet.Action) {
	log.Printf("Socket with addr: %s has been opened...\n", c.RemoteAddr().String())
	s.connectedSockets.Store(c.RemoteAddr().String(), c)
	return
}

func (s *server) OnClosed(c gnet.Conn, err error) (action gnet.Action) {
	log.Printf("Socket with addr: %s is closing...\n", c.RemoteAddr().String())
	s.connectedSockets.Delete(c.RemoteAddr().String())
	return
}

// func (s *server) Tick() (delay time.Duration, action gnet.Action) {
// 	log.Println("It's time to push data to clients!!!")
// 	s.connectedSockets.Range(func(key, value interface{}) bool {
// 		addr := key.(string)
// 		c := value.(gnet.Conn)
// 		c.AsyncWrite([]byte(fmt.Sprintf("heart beating to %s\n", addr)))
// 		return true
// 	})
// 	delay = s.tick
// 	return
// }

func (s *server) React(frame []byte, c gnet.Conn) (out []byte, action gnet.Action) {
	// s.connectedSockets.Range(func(key, value interface{}) bool {
	// 	addr := key.(string)
	// 	raddr := c.RemoteAddr().String()
	// 	if addr == raddr {
	// 		return true
	// 	}
	// 	c := value.(gnet.Conn)
	// 	dt := append([]byte{}, frame...)
	// 	log.Printf("forward data, from=%s==>to=%s, len=%d \n", raddr, addr, len(dt))
	// 	c.AsyncWrite(dt)
	// 	return true
	// })
	data := append([]byte{}, frame...)
	s.ch <- rd{data, c.RemoteAddr().String()}
	return
}

func (s *server) forward() {
	for {
		r := <-s.ch
		s.connectedSockets.Range(func(key, value interface{}) bool {
			addr := key.(string)
			if addr == r.addr {
				return true
			}
			c := value.(gnet.Conn)
			data := append([]byte{}, r.data...)
			log.Printf("forward data, from=%s==>to=%s, len=%d \n", r.addr, addr, len(data))
			c.AsyncWrite(data)
			return true
		})
	}
}

func main() {
	var port int
	var multicore bool

	flag.IntVar(&port, "port", 10902, "server port")
	flag.BoolVar(&multicore, "multicore", true, "multicore")
	flag.Parse()
	s := &server{ch: make(chan rd, 16)}
	go s.forward()
	log.Fatal(gnet.Serve(s, fmt.Sprintf("tcp://:%d", port), gnet.WithMulticore(multicore), gnet.WithTCPKeepAlive(time.Second*30) /*, gnet.WithCodec(&codec{})*/))
}

type codec struct {
}

func (co *codec) Encode(conn gnet.Conn, buf []byte) ([]byte, error) {
	result := make([]byte, 0)
	buffer := bytes.NewBuffer(result)

	dataLen := uint16(len(buf))
	if err := binary.Write(buffer, binary.BigEndian, dataLen); err != nil {
		s := fmt.Sprintf("Pack datalength error , %v", err)
		return nil, errors.New(s)
	}
	if dataLen > 0 {
		if err := binary.Write(buffer, binary.BigEndian, buf); err != nil {
			s := fmt.Sprintf("Pack data error , %v", err)
			return nil, errors.New(s)
		}
	}

	return buffer.Bytes(), nil
}

func (co *codec) Decode(c gnet.Conn) ([]byte, error) {
	size, header := c.ReadN(2)
	if size == 2 {
		byteBuffer := bytes.NewBuffer(header)
		var dataLength uint16
		_ = binary.Read(byteBuffer, binary.BigEndian, &dataLength)
		dataLen := int(dataLength)
		protocolLen := 2 + dataLen
		if dataSize, data := c.ReadN(protocolLen); dataSize == protocolLen {
			c.ShiftN(protocolLen)
			// return the payload of the data
			return data[2:], nil
		}
		return nil, errors.New("not enough payload data")
	}
	return nil, errors.New("not enough header data")
}
