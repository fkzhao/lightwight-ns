package main

import (
	"net"
	"net/rpc"
	"path/filepath"
	"testing"

	"github.com/fkzhao/lightwight-ns/internal/registry"
)

func TestRPCSnapshotAndDiscovery(t *testing.T) {
	store, err := registry.NewStore(filepath.Join(t.TempDir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := store.SaveService("", registry.Service{Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	machine, err := store.SaveMachine("", registry.Machine{Name: "node-1", Address: "10.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveInstance("", registry.Instance{ServiceID: service.ID, MachineID: machine.ID, Port: 8080, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveInstance("", registry.Instance{ServiceID: service.ID, MachineID: machine.ID, Port: 8081, Enabled: false})
	if err != nil {
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
	if !reply.Changed || reply.Snapshot.Version != 4 {
		t.Fatalf("unexpected snapshot: %+v", reply)
	}
	reply = registry.SnapshotReply{}
	if err := client.Call("Registry.GetSnapshot", registry.SnapshotRequest{KnownVersion: 4}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Changed {
		t.Fatal("unchanged version should not send a new snapshot")
	}
	var discovered registry.DiscoverReply
	if err := client.Call("Registry.Discover", registry.DiscoverRequest{ServiceName: "orders"}, &discovered); err != nil {
		t.Fatal(err)
	}
	if len(discovered.Endpoints) != 1 || discovered.Endpoints[0].Port != 8080 {
		t.Fatalf("unexpected endpoints: %+v", discovered)
	}
}
