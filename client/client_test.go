package client

import (
	"fmt"
	"log"
	"net/http"
	"testing"
)

func _assert(condition bool, msg string, v ...interface{}) {
	if !condition {
		panic(fmt.Sprintf("assertion failed: "+msg, v...))
	}
}

func TestClient_dialTimeout(t *testing.T) {

	client := &http.Client{}

	resp, err := client.Post("127.0.0.1:9999", "application/json", nil)
	if err != nil {
		log.Printf("Error: %v", err)
	}
	// client := NewClient("127.0.0.1:9999")
	// args := make(map[string]interface{})
	// args["A"] = 1
	// args["B"] = 2
	// response := client.Call("Math.Add", args)
	log.Printf("Response: %v", resp)
}
