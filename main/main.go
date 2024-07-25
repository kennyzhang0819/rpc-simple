package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"rpcsimple/client"
	"rpcsimple/registry"
	"rpcsimple/server"
	"runtime"
	"time"
)

type Math struct{}

type Args struct {
	A, B int
}

type Args2 struct {
	A, B string
}

func (m *Math) Add(args Args, reply *int) error {
	*reply = args.A + args.B
	return nil
}

func (m *Math) AddString(args Args2, reply *string) error {
	*reply = args.A + args.B
	return nil
}

func call(addr string, ctx server.Context) (int, error) {
	log.Printf("Calling rpc %s with args %v", ctx.ServiceMethod, ctx.Args)
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(ctx); err != nil {
		return 0, fmt.Errorf("failed to encode request: %v", err)
	}

	resp, err := http.Post(fmt.Sprintf("http://%s/call", addr), "application/json", &buf)
	if err != nil {
		return 0, fmt.Errorf("failed to make POST request: %v", err)
	}
	defer resp.Body.Close()

	var result int
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %v", err)
	}

	return result, nil
}

func main() {
	log.SetFlags(0)
	addr := "127.0.0.1:9999"

	var mathService Math
	r := registry.NewRegistry()
	r.Register(&mathService)

	runtime.GOMAXPROCS(runtime.NumCPU())

	go func() {
		server.Start(addr, r)
	}()
	log.Printf("Server is running on %s", addr)
	time.Sleep(1 * time.Second)

	client := client.NewClient("http://127.0.0.1:9999/call")
	args := make(map[string]interface{})
	args["A"] = "hello"
	args["B"] = "world"
	response := client.Call("Math.AddString", args)
	log.Printf("Response: %v", response.Result)

	select {}

}
