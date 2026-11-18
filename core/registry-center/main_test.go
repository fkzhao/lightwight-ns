package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"testing"

	"github.com/fkzhao/lightwight-ns/core/internal/registry"
)

func TestRegistrationHTTPLifecycle(t *testing.T) {
	store, err := registry.NewStore(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveService("", registry.Service{Name: "orders"}); err != nil {
		t.Fatal(err)
	}
	token := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		w := httptest.NewRecorder()
		handleRegistration(store, w, r)
		return w
	}
	registered := request(http.MethodPost, "/api/registrations", registry.RegistrationInput{ServiceName: "orders", Address: "127.0.0.1", Port: 8080, Token: token})
	if registered.Code != http.StatusOK {
		t.Fatalf("register status %d: %s", registered.Code, registered.Body.String())
	}
	var result registry.RegistrationResult
	if err := json.Unmarshal(registered.Body.Bytes(), &result); err != nil || result.InstanceID == "" {
		t.Fatalf("invalid registration response: %+v, %v", result, err)
	}
	renewed := request(http.MethodPost, "/api/registrations/"+result.InstanceID+"/renew", registry.RegistrationToken{Token: token})
	if renewed.Code != http.StatusOK {
		t.Fatalf("renew status %d: %s", renewed.Code, renewed.Body.String())
	}
	removed := request(http.MethodDelete, "/api/registrations/"+result.InstanceID, registry.RegistrationToken{Token: token})
	if removed.Code != http.StatusNoContent || len(store.Snapshot().Services[0].Instances) != 0 {
		t.Fatalf("deregister failed: %d, %s", removed.Code, removed.Body.String())
	}
}

func TestRPCDiscoveryExcludesUnhealthyInstances(t *testing.T) {
	store, err := registry.NewStore(filepath.Join(t.TempDir(), "registry.db"))
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

func TestDatabasePathConfiguration(t *testing.T) {
	t.Setenv("NS_DB_FILE", "")
	t.Setenv("NS_DATA_FILE", "")
	if got := databasePath(); got != "data/registry.db" {
		t.Fatalf("default database path = %s", got)
	}
	t.Setenv("NS_DATA_FILE", "/tmp/test/registry.json")
	if got := databasePath(); got != "/tmp/test/registry.db" {
		t.Fatalf("legacy database path = %s", got)
	}
	t.Setenv("NS_DB_FILE", "/tmp/custom.db")
	if got := databasePath(); got != "/tmp/custom.db" {
		t.Fatalf("NS_DB_FILE did not take precedence: %s", got)
	}
}
