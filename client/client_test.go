package client

import (
	"net"
	"net/rpc"
	"path/filepath"
	"testing"

	"github.com/fkzhao/lightwight-ns/internal/agent"
	"github.com/fkzhao/lightwight-ns/internal/registry"
)

func TestDiscoverOverUnixSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socket)
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
	client, err := Dial(socket)
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
}
