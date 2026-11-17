package health

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/fkzhao/lightwight-ns/core/internal/registry"
)

func TestHTTPHealthRemovesAndRestoresDiscoveryEndpoint(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	address, portString, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatal(err)
	}
	store, err := registry.NewStore(filepath.Join(t.TempDir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := store.SaveService("", registry.Service{Name: "orders", HealthCheck: registry.HealthCheck{Type: "http", Path: "/ready"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveInstance(service.ID, "", registry.Instance{Address: address, Port: port, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	checker := Checker{Store: store}
	rpcServer := registry.RPCServer{Store: store}
	checkEndpoints := func(expected int) {
		t.Helper()
		var reply registry.DiscoverReply
		if err := rpcServer.Discover(registry.DiscoverRequest{ServiceName: "orders"}, &reply); err != nil {
			t.Fatal(err)
		}
		if len(reply.Endpoints) != expected {
			t.Fatalf("expected %d endpoints, got %+v", expected, reply.Endpoints)
		}
	}
	checkEndpoints(0)
	if err := checker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkEndpoints(1)
	healthy.Store(false)
	if err := checker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkEndpoints(0)
	healthy.Store(true)
	if err := checker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkEndpoints(1)
}
