package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"rpcsimple/codec"
	"rpcsimple/registry"
	"sync"
	"time"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber    int           // MagicNumber marks this's a geerpc request
	CodecType      codec.Type    // client may choose different Codec to encode body
	ConnectTimeout time.Duration // 0 means no limit
	HandleTimeout  time.Duration
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GobType,
	ConnectTimeout: time.Second * 10,
}

type Server struct {
	funcMap *registry.Registry
}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

// 启动服务器
func (server *Server) Start(network, address string, funcMap *registry.Registry) {
	if network == "tcp" {
		server.funcMap = funcMap
		server.ListenAndServeTCP(address)
	} else {
		log.Fatalf("rpc server: unsupported network type %s", network)
	}
}

func Start(network, address string, funcMap *registry.Registry) {
	DefaultServer.Start(network, address, funcMap)
}

func (server *Server) ListenAndServeTCP(address string) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("network error: %v", err)
	}
	server.Accept(listener)
}

func (server *Server) Accept(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		go server.ServeConn(conn)
	}
}

// 用json解前4个字节，校验MagicNumber和CodecType，根据CodecType得到对应的解码器，然后调用ServeCodec
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()

	var option Option
	if err := json.NewDecoder(conn).Decode(&option); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}
	if option.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", option.MagicNumber)
		return
	}
	c := codec.NewCodecFuncMap[option.CodecType]
	if c == nil {
		log.Printf("rpc server: invalid codec type %s", option.CodecType)
		return
	}
	log.Println("rpc server: using codec:", option.CodecType)
	server.ServeCodec(c(conn), &option)
}

var invalidRequest = struct{}{}

// 读取请求，调用服务，发送响应
func (server *Server) ServeCodec(c codec.Codec, option *Option) {
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		request, err := server.readRequest(c)
		if err != nil {
			if request == nil {
				break
			}
			request.header.Error = err.Error()
			server.writeResponse(c, request.header, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go server.handle(c, request, sending, wg, option.HandleTimeout)
	}
	wg.Wait()
	_ = c.Close()
}

type request struct {
	header       *codec.Header // header of request
	argv, replyv reflect.Value // argv and replyv of request
	mtype        *registry.MethodEntry
	service      *registry.Service
}

func (server *Server) readRequestHeader(c codec.Codec) (*codec.Header, error) {
	var header codec.Header
	if err := c.ReadHeader(&header); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &header, nil
}

func (server *Server) readRequest(c codec.Codec) (*request, error) {
	header, err := server.readRequestHeader(c)
	if err != nil {
		return nil, err
	}
	request := &request{header: header}
	request.service, request.mtype, err = server.funcMap.FindService(header.ServiceMethod)
	if err != nil {
		return request, err
	}
	request.argv = request.mtype.NewArgv()
	request.replyv = request.mtype.NewReplyv()
	argvi := request.argv.Interface()
	if request.argv.Type().Kind() != reflect.Ptr {
		argvi = request.argv.Addr().Interface()
	}
	if err = c.ReadBody(argvi); err != nil {
		log.Println("rpc server: read body err:", err)
		return request, err
	}
	return request, nil
}

func (server *Server) handle(c codec.Codec, request *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	called := make(chan struct{})
	sent := make(chan struct{})
	go func() {

		err := request.service.Call(request.mtype, request.argv, request.replyv)
		called <- struct{}{}
		if err != nil {
			request.header.Error = err.Error()
			server.writeResponse(c, request.header, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		server.writeResponse(c, request.header, request.replyv.Interface(), sending)
		sent <- struct{}{}
	}()
	if timeout == 0 {
		<-called
		<-sent
		return
	}
	select {
	case <-time.After(timeout):
		request.header.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		server.writeResponse(c, request.header, invalidRequest, sending)
	case <-called:
		<-sent
	}
}

func (server *Server) writeResponse(c codec.Codec, header *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := c.Write(header, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}
