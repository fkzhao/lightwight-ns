package health

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

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
	store, err := registry.NewStore(filepath.Join(t.TempDir(), "registry.db"))
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

func TestRunPublishesFastProbeBeforeSlowProbeCompletes(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	slowStarted := make(chan struct{}, 1)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case slowStarted <- struct{}{}:
		default:
		}
		select {
		case <-release:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer fast.Close()
	store, err := registry.NewStore(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	for _, item := range []struct{ name, endpoint string }{{"slow", slow.URL}, {"fast", fast.URL}} {
		parsed, err := url.Parse(item.endpoint)
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
		service, err := store.SaveService("", registry.Service{Name: item.name, HealthCheck: registry.HealthCheck{Type: "http", Path: "/ready"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.SaveInstance(service.ID, "", registry.Instance{Address: address, Port: port, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	checker := Checker{Store: store, Interval: time.Second, Timeout: 2 * time.Second, Concurrency: 2}
	done := make(chan struct{})
	go func() { checker.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("slow probe never started")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, service := range store.Snapshot().Services {
			if service.Name == "fast" && service.Instances[0].Health == registry.HealthHealthy {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("fast endpoint stayed unavailable while another probe was blocked")
}

func TestRunChecksEveryInstanceBeyondWorkerCount(t *testing.T) {
	store, err := registry.NewStore(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	if err := store.Update(func(snapshot *registry.Snapshot) error {
		for i := 0; i < 100; i++ {
			service := registry.Service{ID: fmt.Sprintf("service-%d", i), Name: fmt.Sprintf("service-%d", i), HealthCheck: registry.HealthCheck{Type: "tcp"}}
			for j := 0; j < 10; j++ {
				n := i*10 + j
				service.Instances = append(service.Instances, registry.Instance{
					ID: fmt.Sprintf("instance-%d", n), Address: fmt.Sprintf("127.10.%d.%d", n/250, n%250+1), Port: port,
					Enabled: true, Health: registry.HealthUnknown,
				})
			}
			snapshot.Services = append(snapshot.Services, service)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	checker := Checker{Store: store, Interval: time.Second, Timeout: 10 * time.Millisecond, Concurrency: 16}
	done := make(chan struct{})
	go func() { checker.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		checked := 0
		for _, service := range store.Snapshot().Services {
			for _, instance := range service.Instances {
				if instance.Health != registry.HealthUnknown {
					checked++
				}
			}
		}
		if checked == 1000 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("some endpoints were starved by the bounded worker pool")
}

func TestLeaseExpiryContinuesWhileProbeIsBlocked(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
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
	store, err := registry.NewStore(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.SaveService("", registry.Service{Name: "orders", HealthCheck: registry.HealthCheck{Type: "http", Path: "/ready"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterInstance(registry.RegistrationInput{
		ServiceName: "orders", Address: address, Port: port, Token: fmt.Sprintf("%064x", 1),
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	checker := Checker{Store: store, Interval: 50 * time.Millisecond, Timeout: 3 * time.Second, Concurrency: 1}
	go func() { checker.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe never started")
	}
	if err := store.Update(func(snapshot *registry.Snapshot) error {
		snapshot.Services[0].Instances[0].LeaseExpiresAt = time.Now().Add(-time.Second).Unix()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(store.Snapshot().Services[0].Instances) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("lease expiry waited for an unrelated blocked health probe")
}
