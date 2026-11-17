package main

import (
	"net"
	"net/rpc"
	"path/filepath"
	"testing"

	"github.com/fkzhao/lightwight-ns/internal/registry"
)

func TestRPCDiscoveryExcludesUnhealthyInstances(t *testing.T) {
	store, err := registry.NewStore(filepath.Join(t.TempDir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := store.SaveService("", registry.Service{Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	healthy, err := store.SaveInstance(service.ID, "", registry.Instance{Address: "10.0.0.1", Port: 8080, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveInstance(service.ID, "", registry.Instance{Address: "10.0.0.2", Port: 8080, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyHealth(store.Snapshot().Version, map[string]string{healthy.ID: registry.HealthHealthy}); err != nil {
		t.Fatal(err)
	}
	server := rpc.NewServer()
	if err := server.RegisterName("Registry", &registry.RPCServer{Store: store}); err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := net.Pipe()
	go server.ServeConn(serverConn)
	client := rpc.NewClient(clientConn)
	defer client.Close()
	var reply registry.SnapshotReply
	if err := client.Call("Registry.GetSnapshot", registry.SnapshotRequest{}, &reply); err != nil {
		t.Fatal(err)
	}
	if !reply.Changed || len(reply.Snapshot.Services[0].Instances) != 2 {
		t.Fatalf("unexpected snapshot: %+v", reply)
	}
	var discovered registry.DiscoverReply
	if err := client.Call("Registry.Discover", registry.DiscoverRequest{ServiceName: "orders"}, &discovered); err != nil {
		t.Fatal(err)
	}
	if len(discovered.Endpoints) != 1 || discovered.Endpoints[0].Address != "10.0.0.1" {
		t.Fatalf("unexpected endpoints: %+v", discovered)
	}
}
