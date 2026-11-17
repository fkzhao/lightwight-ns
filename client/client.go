// Package client provides service discovery through the machine-local agent.
package client

import (
	"net"
	"net/rpc"
)

const DefaultSocketPath = "/tmp/lightwight-ns/agent.sock"

type Endpoint struct {
	Address string
	Port    int
}

type Client struct{ rpc *rpc.Client }

// Dial opens a reusable connection to the local agent's Unix socket.
func Dial(socketPath string) (*Client, error) {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	connection, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	return &Client{rpc: rpc.NewClient(connection)}, nil
}

// Discover returns healthy machine addresses and ports for a service name.
func (c *Client) Discover(serviceName string) ([]Endpoint, error) {
	var reply struct {
		Version   uint64
		Endpoints []Endpoint
	}
	err := c.rpc.Call("Agent.Discover", struct{ ServiceName string }{ServiceName: serviceName}, &reply)
	return reply.Endpoints, err
}

func (c *Client) Close() error { return c.rpc.Close() }
