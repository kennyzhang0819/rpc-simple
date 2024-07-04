package client

import (
	"net"
	"sync"
)

type ConnectionPool struct {
	mu          sync.Mutex
	connections chan net.Conn
	address     string
}

func NewConnectionPool(address string, size int) *ConnectionPool {
	pool := &ConnectionPool{
		connections: make(chan net.Conn, size),
		address:     address,
	}
	for i := 0; i < size; i++ {
		conn, err := net.Dial("tcp", address)
		if err != nil {
			// Handle error
			continue
		}
		pool.connections <- conn
	}
	return pool
}

func (p *ConnectionPool) Get() (net.Conn, error) {
	select {
	case conn := <-p.connections:
		return conn, nil
	default:
		return net.Dial("tcp", p.address)
	}
}

func (p *ConnectionPool) Put(conn net.Conn) {
	select {
	case p.connections <- conn:
	default:
		conn.Close()
	}
}

func (p *ConnectionPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	close(p.connections)
	for conn := range p.connections {
		conn.Close()
	}
}

// Usage in client
func main() {
	pool := NewConnectionPool("127.0.0.1:9999", 10)
	defer pool.Close()

	// Example RPC call using the connection pool
	conn, err := pool.Get()
	if err != nil {
		// Handle error
	}
	// Perform RPC call with conn
	// ...

	// Return connection to pool
	pool.Put(conn)
}
