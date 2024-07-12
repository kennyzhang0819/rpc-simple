package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"rpcsimple/registry"
	"time"

	"github.com/panjf2000/ants/v2"
)

type Context struct {
	ConnectTimeout int // 0 means no limit
	HandleTimeout  int
	ServiceMethod  string
	Args           map[string]interface{}
}

type Server struct {
	funcMap *registry.Registry
	pool    *ants.Pool
}

func NewServer(poolSize int) (*Server, error) {
	pool, err := ants.NewPool(poolSize)
	if err != nil {
		return nil, err
	}
	return &Server{pool: pool}, nil
}

var DefaultServer, _ = NewServer(5000)

// 启动服务器
func (server *Server) Start(address string, funcMap *registry.Registry) {
	server.funcMap = funcMap
	http.HandleFunc("/call", server.handleRequestWithPool)
	log.Printf("Starting HTTP server on %s\n", address)
	if err := http.ListenAndServe(address, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func Start(address string, funcMap *registry.Registry) {
	DefaultServer.Start(address, funcMap)
}

type RequestData struct {
	ctx Context
	err error
}

type ResponseData struct {
	StatusCode int
	Body       []byte
}

func (server *Server) handleRequestWithPool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST requests are supported", http.StatusMethodNotAllowed)
		return
	}

	requestChan := make(chan RequestData)
	err := server.pool.Submit(func() {
		server.readRequest(r, requestChan)
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to submit request to pool: %v", err), http.StatusInternalServerError)
		return
	}

	result := <-requestChan
	if result.err != nil {
		http.Error(w, fmt.Sprintf("Failed to read or parse request body: %v", result.err), http.StatusBadRequest)
		return
	}

	responseChan := make(chan ResponseData)
	err = server.pool.Submit(func() {
		server.handle(result.ctx, responseChan)
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to submit request to pool: %v", err), http.StatusInternalServerError)
		return
	}

	response := <-responseChan
	w.WriteHeader(response.StatusCode)
	w.Header().Set("Content-Type", "application/json")
	w.Write(response.Body)
}

func (server *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST requests are supported", http.StatusMethodNotAllowed)
		return
	}

	requestChan := make(chan RequestData)
	go server.readRequest(r, requestChan)

	result := <-requestChan
	if result.err != nil {
		http.Error(w, fmt.Sprintf("Failed to read or parse request body: %v", result.err), http.StatusBadRequest)
		return
	}

	responseChan := make(chan ResponseData)
	go server.handle(result.ctx, responseChan)

	response := <-responseChan
	w.WriteHeader(response.StatusCode)
	w.Header().Set("Content-Type", "application/json")
	w.Write(response.Body)
}

func (server *Server) readRequest(r *http.Request, requestChan chan<- RequestData) {
	var ctx Context
	body, err := io.ReadAll(r.Body)
	if err != nil {
		requestChan <- RequestData{ctx: ctx, err: err}
		return
	}

	if err := json.Unmarshal(body, &ctx); err != nil {
		requestChan <- RequestData{ctx: ctx, err: err}
		return
	}
	requestChan <- RequestData{ctx: ctx, err: nil}
}

func (server *Server) handle(ctx Context, responseChan chan<- ResponseData) {
	service, mEntry, err := server.funcMap.FindService(ctx.ServiceMethod)
	if err != nil {
		responseChan <- ResponseData{
			StatusCode: http.StatusBadRequest,
			Body:       []byte(fmt.Sprintf("Service method %s not found: %v", ctx.ServiceMethod, err)),
		}
		return
	}

	argv := mEntry.NewArgv()
	replyv := mEntry.NewReplyv()

	argBytes, err := json.Marshal(ctx.Args)
	if err != nil {
		responseChan <- ResponseData{
			StatusCode: http.StatusBadRequest,
			Body:       []byte(fmt.Sprintf("Failed to marshal arguments: %v", err)),
		}
		return
	}

	if argv.Kind() == reflect.Ptr {
		argv = argv.Elem()
	}

	if err := json.Unmarshal(argBytes, argv.Addr().Interface()); err != nil {
		responseChan <- ResponseData{
			StatusCode: http.StatusBadRequest,
			Body:       []byte(fmt.Sprintf("Failed to unmarshal arguments: %v", err)),
		}
		return
	}

	callDone := make(chan struct{})
	var callErr error

	go func() {
		callErr = service.Call(mEntry, argv, replyv)
		close(callDone)
	}()

	select {
	case <-callDone:
		if callErr != nil {
			responseChan <- ResponseData{
				StatusCode: http.StatusInternalServerError,
				Body:       []byte(fmt.Sprintf("Service call failed: %v", callErr)),
			}
			return
		}
		respMap := map[string]interface{}{
			"result": replyv.Interface(),
		}
		respBytes, err := json.Marshal(respMap)
		if err != nil {
			responseChan <- ResponseData{
				StatusCode: http.StatusInternalServerError,
				Body:       []byte(fmt.Sprintf("Failed to marshal response: %v", err)),
			}
			return
		}
		responseChan <- ResponseData{
			StatusCode: http.StatusOK,
			Body:       respBytes,
		}
	case <-time.After(time.Duration(ctx.ConnectTimeout) * time.Second):
		responseChan <- ResponseData{
			StatusCode: http.StatusRequestTimeout,
			Body:       []byte("Service call timeout"),
		}
	}
}
