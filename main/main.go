package main

import (
	"context"
	"errors"
	"log"
	"rpcsimple/client"
	"rpcsimple/codec"
	"rpcsimple/registry"
	"rpcsimple/server"
	"strings"
	"sync"
	"time"
)

type PersonService string
type Person struct {
	Name string
	Age  int
}

func (p PersonService) FindOldest(args []Person, reply *string) error {
	if len(args) == 0 {
		return errors.New("persons is empty")
	}
	oldest := 0
	for i, p := range args {
		if p.Age > args[oldest].Age {
			oldest = i
		}
	}
	*reply = args[oldest].Name
	return nil
}

func (p PersonService) Names(args []Person, reply *string) error {
	if len(args) == 0 {
		return errors.New("persons is empty")
	}
	names := make([]string, 0)
	for _, p := range args {
		names = append(names, p.Name)
	}
	*reply = strings.Join(names, ",")
	return nil
}

func main() {
	log.SetFlags(0)
	network := "tcp"
	addr := "127.0.0.1:9999"
	ctx, canc := context.WithTimeout(context.Background(), time.Second*5)
	defer canc()

	var personService PersonService
	r := registry.NewRegistry()
	r.Register(&personService)

	go server.Start(network, addr, r)
	opt := &server.Option{
		MagicNumber:    server.MagicNumber,
		CodecType:      codec.HttpType,
		ConnectTimeout: time.Second,
	}
	client, _ := client.Connect(network, addr, opt)

	time.Sleep(time.Second)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		people := []Person{
			{"Tom", 20},
			{"Jerry", 18},
			{"Mickey", 25},
		}
		var reply string
		client.Call(ctx, "PersonService.FindOldest", people, &reply)
		log.Printf("Oldest person is %s.", reply)
	}()
	wg.Wait()
}
