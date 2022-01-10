package pos33

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

type frc struct {
	fs string
	c  net.Conn
	ch chan []byte
}

func encode(buf []byte, c net.Conn) error {
	result := make([]byte, 0)
	buffer := bytes.NewBuffer(result)

	dataLen := uint16(len(buf))
	if err := binary.Write(buffer, binary.BigEndian, dataLen); err != nil {
		s := fmt.Sprintf("Pack datalength error , %v", err)
		return errors.New(s)
	}
	if dataLen > 0 {
		if err := binary.Write(buffer, binary.BigEndian, buf); err != nil {
			s := fmt.Sprintf("Pack data error , %v", err)
			return errors.New(s)
		}
	}

	n := 0
	buf = buffer.Bytes()
	for n < len(buf) {
		w, err := c.Write(buf)
		if err != nil {
			return err
		}
		n += w
		buf = buf[w:]
	}
	return nil
}

func decode(c net.Conn) ([]byte, error) {
	buf := make([]byte, 2)
	n, err := io.ReadFull(c, buf)
	if n != 2 {
		return nil, err
	}
	byteBuffer := bytes.NewBuffer(buf)
	var dl uint16
	_ = binary.Read(byteBuffer, binary.BigEndian, &dl)
	buf = make([]byte, int(dl))
	n, err = io.ReadFull(c, buf)
	if n != int(dl) {
		return nil, err
	}
	return buf, nil
}

func (f *frc) connect(fs string) error {
	conn, err := net.Dial("tcp", fs)
	if err != nil {
		return err
	}
	plog.Info("connect forward server", "addr", conn.RemoteAddr().String())
	f.c = conn
	f.fs = fs
	return nil
}

func (f *frc) reConnect() error {
	f.c.Close()
	for {
		time.Sleep(time.Second * 10)
		err := f.connect(f.fs)
		if err != nil {
			plog.Error("connect forward server error:", "err", err)
			continue
		}
	}
}

func (g *gossip2) forwad(data []byte) {
	select {
	// case g.fsCh <- data:
	default:
	}
}

func (g *gossip2) fsLoop(fs []string) error {
	send := func(fc *frc) {
		for data := range fc.ch {
			err := encode(data, fc.c)
			if err != nil {
				plog.Error("encode error", "err", err)
				fc.reConnect()
			}
		}
	}
	recv := func(fc *frc) {
		for {
			var data []byte
			data, err := decode(fc.c)
			if err != nil {
				plog.Error("decode error", "err", err)
				fc.reConnect()
				continue
			}
			g.C <- data
		}
	}
	mp := make(map[string]*frc)
	// go func() {
	// 	for data := range g.fsCh {
	// 		for _, fc := range mp {
	// 			fc.ch <- data
	// 		}
	// 	}
	// }()
	for _, s := range fs {
		fc, ok := mp[s]
		if !ok {
			fc = &frc{ch: make(chan []byte, 16)}
			mp[s] = fc
		}
		err := fc.connect(s)
		if err != nil {
			plog.Error("fs connect error", "err", err)
			continue
		}
		go send(fc)
		go recv(fc)
	}
	return nil
}
