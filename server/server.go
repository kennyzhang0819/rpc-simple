package server

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"reflect"
	"rpcsimple/registry"
	"time"
)

type Context struct {
	ConnectTimeout time.Duration // 0 means no limit
	HandleTimeout  time.Duration
	ServiceMethod  string
	Args           []interface{}
}

type Server struct {
	funcMap *registry.Registry
}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

// 启动服务器
func (server *Server) Start(address string, funcMap *registry.Registry) {
	server.funcMap = funcMap
	http.HandleFunc("/rpc", server.handleRequest)
	log.Printf("Starting HTTP server on %s\n", address)
	if err := http.ListenAndServe(address, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func Start(address string, funcMap *registry.Registry) {
	DefaultServer.Start(address, funcMap)
}

func (server *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST requests are supported", http.StatusMethodNotAllowed)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}

	var ctx Context
	if err := json.Unmarshal(body, &ctx); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse request body: %v", err), http.StatusBadRequest)
		return
	}

	server.handle(w, ctx)
}

func (server *Server) handle(w http.ResponseWriter, ctx Context) {
	service, mEntry, err := server.funcMap.FindService(ctx.ServiceMethod)
	if err != nil {
		http.Error(w, fmt.Sprintf("Service method %s not found: %v", ctx.ServiceMethod, err), http.StatusBadRequest)
		return
	}

	argv := mEntry.NewArgv()
	replyv := mEntry.NewReplyv()

	if len(ctx.Args) != 1 {
		http.Error(w, "Invalid number of arguments", http.StatusBadRequest)
		return
	}

	argBytes, err := json.Marshal(ctx.Args[0])
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal arguments: %v", err), http.StatusBadRequest)
		return
	}

	if argv.Kind() == reflect.Ptr {
		argv = argv.Elem()
	}

	if err := json.Unmarshal(argBytes, argv.Addr().Interface()); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unmarshal arguments: %v", err), http.StatusBadRequest)
		return
	}

	callDone := make(chan struct{})
	go func() {
		err = service.Call(mEntry, argv, replyv)
		callDone <- struct{}{}
	}()

	select {
	case <-callDone:
		if err != nil {
			http.Error(w, fmt.Sprintf("Service call failed: %v", err), http.StatusInternalServerError)
			return
		}
		respBytes, err := json.Marshal(replyv.Interface())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to marshal response: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(respBytes)
	case <-time.After(ctx.HandleTimeout):
		http.Error(w, "Service call timeout", http.StatusRequestTimeout)
	}
}
