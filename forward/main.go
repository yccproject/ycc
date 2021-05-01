package main

import (
	"flag"
	"fmt"
	"log"
	"sync"

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
	data := make([]byte, len(frame))
	copy(data, frame)
	addr := c.RemoteAddr().String()
	s.ch <- rd{data, addr}
	log.Printf("recv from %s data \n", addr)
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
			log.Printf("forward data, from=%s==>to=%s \n", r.addr, c.RemoteAddr().String())
			c.AsyncWrite(r.data)
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
	log.Fatal(gnet.Serve(s, fmt.Sprintf("tcp://:%d", port), gnet.WithMulticore(multicore)))
}
