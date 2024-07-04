package codec

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
)

type HttpCodec struct {
	conn   io.ReadWriteCloser
	buffer *bufio.Writer
	dec    *json.Decoder
	enc    *json.Encoder
}

var _ Codec = (*HttpCodec)(nil)

func NewHttpCodec(conn io.ReadWriteCloser) Codec {
	buffer := bufio.NewWriter(conn)
	return &HttpCodec{
		conn:   conn,
		buffer: buffer,
		dec:    json.NewDecoder(conn),
		enc:    json.NewEncoder(buffer),
	}
}

func (c *HttpCodec) ReadHeader(header *Header) error {
	return c.dec.Decode(header)
}

func (c *HttpCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *HttpCodec) Write(header *Header, body interface{}) (err error) {
	defer func() {
		_ = c.buffer.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()
	if err = c.enc.Encode(header); err != nil {
		log.Println("rpc: http error encoding header:", err)
		return
	}
	if err = c.enc.Encode(body); err != nil {
		log.Println("rpc: http error encoding body:", err)
		return
	}
	return
}

func (c *HttpCodec) Close() error {
	return c.conn.Close()
}
