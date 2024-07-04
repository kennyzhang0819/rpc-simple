package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type GobCodec struct {
	conn   io.ReadWriteCloser
	buffer *bufio.Writer
	dec    *gob.Decoder
	enc    *gob.Encoder
}

var _ Codec = (*GobCodec)(nil)

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buffer := bufio.NewWriter(conn)
	return &GobCodec{
		conn:   conn,
		buffer: buffer,
		dec:    gob.NewDecoder(conn),
		enc:    gob.NewEncoder(buffer),
	}
}

func (c *GobCodec) ReadHeader(header *Header) error {
	return c.dec.Decode(header)
}

func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *GobCodec) Write(header *Header, body interface{}) (err error) {
	defer func() {
		_ = c.buffer.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()
	if err = c.enc.Encode(header); err != nil {
		log.Println("rpc: gob error encoding header:", err)
		return
	}
	if err = c.enc.Encode(body); err != nil {
		log.Println("rpc: gob error encoding body:", err)
		return
	}
	return
}

func (c *GobCodec) Close() error {
	return c.conn.Close()
}
