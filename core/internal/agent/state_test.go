package agent

import (
	"context"
	"net"
	"net/rpc"
	"path/filepath"
	"testing"
	"time"

	"github.com/fkzhao/lightwight-ns/core/internal/registry"
)

func TestSyncAndLocalDiscoveryTracksHealth(t *testing.T) {
	store, err := registry.NewStore(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := store.SaveService("", registry.Service{Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := store.SaveInstance(service.ID, "", registry.Instance{Address: "127.0.0.1", Port: 8080, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := rpc.NewServer()
	if err := server.RegisterName("Registry", &registry.RPCServer{Store: store}); err != nil {
		t.Fatal(err)
	}
	go server.Accept(listener)
	state := &State{}
	if _, err := state.Resolve("orders"); err == nil {
		t.Fatal("discovery succeeded before initial sync")
	}
	if err := SyncOnce(context.Background(), state, listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	reply, err := state.Resolve("orders")
	if err != nil || len(reply.Endpoints) != 0 {
		t.Fatalf("unknown endpoint should be excluded: %+v, %v", reply, err)
	}
	if err := store.ApplyHealth(store.Snapshot().Version, map[string]string{instance.ID: registry.HealthHealthy}); err != nil {
		t.Fatal(err)
	}
	if err := SyncOnce(context.Background(), state, listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	reply, err = state.Resolve("orders")
	if err != nil || len(reply.Endpoints) != 1 || reply.Endpoints[0].Port != 8080 {
		t.Fatalf("healthy endpoint missing: %+v, %v", reply, err)
	}
	if err := store.ApplyHealth(store.Snapshot().Version, map[string]string{instance.ID: registry.HealthUnhealthy}); err != nil {
		t.Fatal(err)
	}
	if err := SyncOnce(context.Background(), state, listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	reply, err = state.Resolve("orders")
	if err != nil || len(reply.Endpoints) != 0 {
		t.Fatalf("unhealthy endpoint remained available: %+v, %v", reply, err)
	}
}

func TestAgentRejectsExpiredSnapshot(t *testing.T) {
	state := &State{MaxStale: time.Second}
	state.Apply(registry.Snapshot{Version: 1, Services: []registry.Service{{Name: "orders"}}})
	state.mu.Lock()
	state.lastSync = time.Now().Add(-2 * time.Second)
	state.mu.Unlock()
	if _, err := state.Resolve("orders"); err == nil {
		t.Fatal("expired snapshot was served")
	}
	if !state.Status().Stale {
		t.Fatal("status did not report staleness")
	}
	state.MarkSynced()
	if _, err := state.Resolve("orders"); err != nil {
		t.Fatal(err)
	}
}
