package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"rpcsimple/codec"
	"rpcsimple/server"
	"sync"
	"time"
)

// Call represents an active RPC.
type Call struct {
	Seq           uint64
	ServiceMethod string      // format "<service>.<method>"
	Args          interface{} // arguments to the function
	Reply         interface{} // reply from the function
	Error         error       // if error occurs, it will be set
	Done          chan *Call  // Strobes when call is complete.
}

func (call *Call) done() {
	call.Done <- call
}

// There may be multiple outstanding Calls associated
// with a single Client, and a Client may be used by
// multiple goroutines simultaneously.
type Client struct {
	c        codec.Codec
	opt      *server.Option
	sending  sync.Mutex // protect following
	header   codec.Header
	lock     sync.Mutex // protect following
	seq      uint64
	pending  map[uint64]*Call
	closing  bool // user has called Close
	shutdown bool // server has told us to stop
}

var _ io.Closer = (*Client)(nil)

var ErrShutdown = errors.New("connection is shut down")

func (client *Client) Close() error {
	client.lock.Lock()
	defer client.lock.Unlock()
	if client.closing {
		return ErrShutdown
	}
	client.closing = true
	return client.c.Close()
}

func (client *Client) IsAvailable() bool {
	client.lock.Lock()
	defer client.lock.Unlock()
	return !client.shutdown && !client.closing
}

func (client *Client) registerCall(call *Call) (uint64, error) {
	client.lock.Lock()
	defer client.lock.Unlock()
	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}
	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

func (client *Client) removeCall(seq uint64) *Call {
	client.lock.Lock()
	defer client.lock.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.lock.Lock()
	defer client.lock.Unlock()
	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

func (client *Client) send(call *Call) {
	client.sending.Lock()
	defer client.sending.Unlock()
	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	if err := client.c.Write(&client.header, call.Args); err != nil {
		call := client.removeCall(seq)
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

func (client *Client) receive() {
	var err error
	for err == nil {
		var header codec.Header
		if err = client.c.ReadHeader(&header); err != nil {
			break
		}
		call := client.removeCall(header.Seq)
		switch {
		case call == nil:
			// it usually means that Write partially failed
			// and call was already removed.
			err = client.c.ReadBody(nil)
		case header.Error != "":
			call.Error = fmt.Errorf(header.Error)
			err = client.c.ReadBody(nil)
			call.done()
		default:
			err = client.c.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}
	client.terminateCalls(err)
}

// Go invokes the named function asynchronously.
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 1)
	} else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	client.send(call)
	return call
}

// Call invokes the named function, waits for it to complete,
// and returns its error status.
func (client *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	call := client.Go(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return errors.New("rpc client: call failed: " + ctx.Err().Error())
	case call := <-call.Done:
		return call.Error
	}
}

func parseOptions(options ...*server.Option) (*server.Option, error) {
	if len(options) == 0 || options[0] == nil {
		return server.DefaultOption, nil
	}
	if len(options) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	option := options[0]
	if option.MagicNumber == 0 {
		option.MagicNumber = server.DefaultOption.MagicNumber
	}
	if option.CodecType == "" {
		option.CodecType = server.DefaultOption.CodecType
	}
	return option, nil
}

func NewClient(conn net.Conn, opt *server.Option) (*Client, error) {
	c := codec.NewCodecFuncMap[opt.CodecType]
	if c == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("rpc client: codec error:", err)
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options error: ", err)
		_ = conn.Close()
		return nil, err
	}
	return newClientCodec(c(conn), opt), nil
}

func newClientCodec(c codec.Codec, opt *server.Option) *Client {
	client := &Client{
		seq:     1, // seq starts with 1, 0 means invalid call
		c:       c,
		opt:     opt,
		pending: make(map[uint64]*Call),
	}
	go client.receive()
	return client
}

type clientResult struct {
	client *Client
	err    error
}

type newClientFunc func(conn net.Conn, opt *server.Option) (client *Client, err error)

func dialTimeout(f newClientFunc, network, address string, opts ...*server.Option) (client *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	// close the connection if client is nil
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	ch := make(chan clientResult)
	go func() {
		client, err := f(conn, opt)
		ch <- clientResult{client: client, err: err}
	}()
	if opt.ConnectTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}
	select {
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("rpc client: connect timeout: expect within %s", opt.ConnectTimeout)
	case result := <-ch:
		return result.client, result.err
	}
}

// Connect connects to an RPC server at the specified network address
func Connect(network, address string, opts ...*server.Option) (*Client, error) {
	return dialTimeout(NewClient, network, address, opts...)
}
