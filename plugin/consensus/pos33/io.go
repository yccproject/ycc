package pos33

import (
	"bufio"
	"encoding/binary"
	"io"

	"github.com/golang/protobuf/proto"
)

type Writer interface {
	WriteMsg(proto.Message) error
}

type WriteCloser interface {
	Writer
	io.Closer
}

type Reader interface {
	ReadMsg(msg proto.Message) error
}

type ReadCloser interface {
	Reader
	io.Closer
}

func NewDelimitedWriter(w io.Writer) WriteCloser {
	return &varintWriter{w, make([]byte, binary.MaxVarintLen64), nil}
}

type varintWriter struct {
	w      io.Writer
	lenBuf []byte
	buffer []byte
}

func (v *varintWriter) WriteMsg(msg proto.Message) (err error) {
	var data []byte
	// fallback
	data, err = proto.Marshal(msg)
	if err != nil {
		return err
	}
	length := uint64(len(data))
	n := binary.PutUvarint(v.lenBuf, length)
	_, err = v.w.Write(v.lenBuf[:n])
	if err != nil {
		return err
	}
	_, err = v.w.Write(data)
	return err
}

func (v *varintWriter) Close() error {
	if closer, ok := v.w.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func NewDelimitedReader(r io.Reader, maxSize int) ReadCloser {
	var closer io.Closer
	if c, ok := r.(io.Closer); ok {
		closer = c
	}
	return &varintReader{bufio.NewReader(r), nil, maxSize, closer}
}

type varintReader struct {
	r       *bufio.Reader
	buf     []byte
	maxSize int
	closer  io.Closer
}

func (v *varintReader) ReadMsg(msg proto.Message) error {
	length64, err := binary.ReadUvarint(v.r)
	if err != nil {
		return err
	}
	length := int(length64)
	if length < 0 || length > v.maxSize {
		return io.ErrShortBuffer
	}
	if len(v.buf) < length {
		v.buf = make([]byte, length)
	}
	buf := v.buf[:length]
	if _, err := io.ReadFull(v.r, buf); err != nil {
		return err
	}
	return proto.Unmarshal(buf, msg)
}

func (v *varintReader) Close() error {
	if v.closer != nil {
		return v.closer.Close()
	}
	return nil
}
