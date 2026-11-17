package client

import (
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"testing"

	"github.com/fkzhao/lightwight-ns/internal/agent"
	"github.com/fkzhao/lightwight-ns/internal/registry"
)

func TestDiscoverOverLocalTransports(t *testing.T) {
	for _, network := range []string{"unix", "tcp"} {
		t.Run(network, func(t *testing.T) {
			address := "127.0.0.1:0"
			if network == "unix" {
				dir, err := os.MkdirTemp("/tmp", "ns-test-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(dir) })
				address = filepath.Join(dir, "agent.sock")
			}
			listener, err := net.Listen(network, address)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			state := &agent.State{}
			state.Apply(registry.Snapshot{Version: 2, Services: []registry.Service{{
				Name: "orders", Instances: []registry.Instance{{Address: "10.0.0.1", Port: 8080, Enabled: true, Health: registry.HealthHealthy}},
			}}})
			server := rpc.NewServer()
			if err := server.RegisterName("Agent", &agent.LocalRPC{State: state}); err != nil {
				t.Fatal(err)
			}
			go server.Accept(listener)
			var client *Client
			if network == "tcp" {
				client, err = DialTCP(listener.Addr().String())
			} else {
				client, err = Dial(listener.Addr().String())
			}
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			endpoints, err := client.Discover("orders")
			if err != nil {
				t.Fatal(err)
			}
			if len(endpoints) != 1 || endpoints[0].Address != "10.0.0.1" || endpoints[0].Port != 8080 {
				t.Fatalf("unexpected endpoints: %+v", endpoints)
			}
		})
	}
}
