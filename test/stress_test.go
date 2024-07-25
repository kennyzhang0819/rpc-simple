package test

import (
	"log"
	"math/rand"
	"rpcsimple/client"
	"sync"
	"testing"
	"time"
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
	numRequests    = 100
	numGoroutines  = numRequests
	connectTimeout = 10
	handleTimeout  = 10
)

func TestStress(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(numRequests)

	sem := make(chan struct{}, numGoroutines)
	defer close(sem)
	client := client.NewClient("http://127.0.0.1:9999/call")

	for i := 0; i < numRequests; i++ {
		sem <- struct{}{}
		go func(i int) {
			defer func() { <-sem }()
			args := make(map[string]interface{})
			x := rand.Intn(1000)
			y := rand.Intn(1000)

			args["A"] = x
			args["B"] = y
			response := client.Call("Math.Add", args)

			expected := x + y
			// Type assertion to ensure correct type comparison
			if result, ok := response.Result.(float64); ok {
				if int(result) != expected {
					t.Errorf("Unexpected result: got %v, want %v", result, expected)
				}
			} else {
				t.Errorf("Unexpected result type: got %T, want float64", response.Result)
			}

			wg.Done()
		}(i)
	}

	wg.Wait()
}

func TestStressMultipleTimes(t *testing.T) {
	numTests := 10
	totalElapsedTime := time.Duration(0)

	for i := 0; i < numTests; i++ {
		startTime := time.Now()

		t.Run("TestStress", TestStress)

		duration := time.Since(startTime)
		totalElapsedTime += duration
		log.Printf("TestStress run %d completed in %v\n", i+1, duration)
	}

	avgElapsedTime := totalElapsedTime / 10
	log.Printf("Average test duration: %v\n", avgElapsedTime)
}
