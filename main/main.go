package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"rpcsimple/registry"
	"rpcsimple/server"
	"sync"
	"time"
)

type Math struct{}

type Args struct {
	A, B int
}

func (m *Math) Add(args Args, reply *int) error {
	*reply = args.A + args.B
	return nil
}

func call(addr string, ctx server.Context) (int, error) {
	log.Printf("Calling rpc %s with args %v", ctx.ServiceMethod, ctx.Args)
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(ctx); err != nil {
		return 0, fmt.Errorf("failed to encode request: %v", err)
	}

	resp, err := http.Post(fmt.Sprintf("http://%s/rpc", addr), "application/json", &buf)
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

	go server.Start(addr, r)
	time.Sleep(100 * time.Millisecond)

	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			ctx := server.Context{
				ConnectTimeout: 5 * time.Second,
				HandleTimeout:  5 * time.Second,
				ServiceMethod:  "Math.Add",
				Args:           []interface{}{map[string]int{"A": i, "B": i + 1}},
			}

			if result, err := call(addr, ctx); err != nil {
				log.Fatalf("Failed to call remote procedure: %v", err)
			} else {
				log.Printf("Result: %d", result)
			}
		}(i)
	}
	wg.Wait()
}
