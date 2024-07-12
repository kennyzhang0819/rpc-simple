package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"testing"
)

type RequestBody struct {
	ConnectTimeout int         `json:"ConnectTimeout"`
	HandleTimeout  int         `json:"HandleTimeout"`
	ServiceMethod  string      `json:"ServiceMethod"`
	Args           ArgsPayload `json:"Args"`
}

type ResponseBody struct {
	Result int `json:"result"`
}

type ArgsPayload struct {
	A int `json:"A"`
	B int `json:"B"`
}

const (
	url            = "http://127.0.0.1:9999/call"
	numRequests    = 1000
	numGoroutines  = 10000
	connectTimeout = 10
	handleTimeout  = 10
)

func makeRequest(x, y int, wg *sync.WaitGroup, t *testing.T) {
	defer wg.Done()

	body := RequestBody{
		ConnectTimeout: connectTimeout,
		HandleTimeout:  handleTimeout,
		ServiceMethod:  "Math.Add",
		Args: ArgsPayload{
			A: x,
			B: y,
		},
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		t.Errorf("Error marshaling JSON: %v", err)
		return
	}

	addr := "127.0.0.1:9999"
	resp, err := http.Post(fmt.Sprintf("http://%s/call", addr), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Non-OK HTTP status: %v", resp.Status)
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
		return
	}

	var responseBody ResponseBody
	err = json.Unmarshal(bodyBytes, &responseBody)
	if err != nil {
		t.Errorf("Error unmarshaling response JSON: %v", err)
		return
	}

	expected := x + y
	if responseBody.Result != expected {
		t.Errorf("Unexpected result: got %v, want %v", responseBody.Result, expected)
	}
}

func TestStress(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(numRequests)

	sem := make(chan struct{}, numGoroutines)
	defer close(sem)
	for i := 0; i < numRequests; i++ {
		sem <- struct{}{}
		go func(i int) {
			defer func() { <-sem }()

			x := rand.Intn(1000)
			y := rand.Intn(1000)
			makeRequest(x, y, &wg, t)
		}(i)
	}

	wg.Wait()
}
