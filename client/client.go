package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
)

type Request struct {
	Seq          uint64
	requestBody  RequestBody
	responseBody ResponseBody
	Error        error
	Done         chan *Request
}

func (request *Request) done() {
	request.Done <- request
}

type RequestBody struct {
	ConnectTimeout int                    `json:"ConnectTimeout"`
	HandleTimeout  int                    `json:"HandleTimeout"`
	ServiceMethod  string                 `json:"ServiceMethod"`
	Args           map[string]interface{} `json:"Args"`
}

type ResponseBody struct {
	Result interface{} `json:"result"`
}

type Client struct {
	httpClient *http.Client
	url        string
	sending    sync.Mutex
	lock       sync.Mutex
	seq        uint64
	pending    map[uint64]*Request
	closing    bool
	shutdown   bool
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
	return nil
}

func (client *Client) IsAvailable() bool {
	client.lock.Lock()
	defer client.lock.Unlock()
	return !client.shutdown && !client.closing
}

func (client *Client) registerRequest(request *Request) (uint64, error) {
	client.lock.Lock()
	defer client.lock.Unlock()
	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}
	request.Seq = client.seq
	client.pending[request.Seq] = request
	client.seq++
	return request.Seq, nil
}

func (client *Client) removeRequest(seq uint64) *Request {
	client.lock.Lock()
	defer client.lock.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

func (client *Client) terminateRequests(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.lock.Lock()
	defer client.lock.Unlock()
	client.shutdown = true
	for _, request := range client.pending {
		request.Error = err
		request.done()
	}
}

func (client *Client) send(request *Request) {
	client.sending.Lock()
	defer client.sending.Unlock()
	seq, err := client.registerRequest(request)
	if err != nil {
		request.Error = err
		request.done()
		return
	}
	jsonData, err := json.Marshal(request.requestBody)
	if err != nil {
		request.Error = err
		request.done()
		return
	}
	resp, err := client.httpClient.Post(client.url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		request := client.removeRequest(seq)
		if request != nil {
			request.Error = err
			request.done()
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		request.Error = errors.New("Non-OK HTTP status: " + resp.Status)
		request.done()
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		request.Error = err
		request.done()
		return
	}

	var responseBody ResponseBody
	err = json.Unmarshal(bodyBytes, &responseBody)
	if err != nil {
		request.Error = err
		request.done()
		return
	}
	request.responseBody = responseBody
	request.done()
}

func (client *Client) Go(serviceMethod string, args interface{}) *Request {

	done := make(chan *Request, 1)

	requestBody := RequestBody{
		ConnectTimeout: 10,
		HandleTimeout:  10,
		ServiceMethod:  serviceMethod,
		Args:           args.(map[string]interface{}),
	}
	request := &Request{
		requestBody: requestBody,
		Done:        done,
	}
	client.send(request)
	return request
}

func (client *Client) Call(serviceMethod string, args interface{}) *ResponseBody {
	request := client.Go(serviceMethod, args)
	return &request.responseBody
}

func NewClient(url string) *Client {
	return &Client{
		httpClient: &http.Client{},
		url:        url,
		pending:    make(map[uint64]*Request),
	}
}
