// Package discovery resolves services through the machine-local agent.
package discovery

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	DefaultTCPAddress = "127.0.0.1:9091"
	DefaultSocketPath = "/tmp/lightwight-ns/agent.sock"
)

type Endpoint struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

type Status struct {
	Ready           bool   `json:"ready"`
	RegistryVersion uint64 `json:"registryVersion"`
	LastSyncAt      string `json:"lastSyncAt"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("agent RPC %d: %s", e.Code, e.Message) }

type Client struct {
	mu     sync.Mutex
	conn   net.Conn
	reader *bufio.Reader
	nextID int64
}

func DialTCP(address string) (*Client, error) {
	if address == "" {
		address = DefaultTCPAddress
	}
	return dial("tcp", address)
}

func DialUnix(path string) (*Client, error) {
	if path == "" {
		path = DefaultSocketPath
	}
	return dial("unix", path)
}

func dial(network, address string) (*Client, error) {
	connection, err := net.DialTimeout(network, address, 3*time.Second)
	if err != nil {
		return nil, err
	}
	return &Client{conn: connection, reader: bufio.NewReader(connection)}, nil
}

func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return err
	}
	defer c.conn.SetDeadline(time.Time{})
	c.nextID++
	request := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int64  `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{JSONRPC: "2.0", ID: c.nextID, Method: method, Params: params}
	if err := json.NewEncoder(c.conn).Encode(request); err != nil {
		return err
	}
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return err
	}
	if len(line) > 1<<20 {
		return errors.New("agent response exceeds 1 MiB")
	}
	var reply struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int64           `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(line, &reply); err != nil {
		return err
	}
	if reply.JSONRPC != "2.0" || reply.ID != c.nextID {
		return errors.New("invalid agent RPC response")
	}
	if reply.Error != nil {
		return reply.Error
	}
	return json.Unmarshal(reply.Result, result)
}

func (c *Client) Resolve(ctx context.Context, serviceName string) ([]Endpoint, error) {
	var result struct {
		RegistryVersion uint64     `json:"registryVersion"`
		Endpoints       []Endpoint `json:"endpoints"`
	}
	err := c.call(ctx, "discovery.resolve", map[string]string{"service": serviceName}, &result)
	return result.Endpoints, err
}

func (c *Client) AgentStatus(ctx context.Context) (Status, error) {
	var status Status
	err := c.call(ctx, "agent.status", map[string]string{}, &status)
	return status, err
}

func (c *Client) Close() error { return c.conn.Close() }
