package registry

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistrationLeaseLifecycle(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := store.SaveService("", Service{Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveInstance(service.ID, "", Instance{Address: "127.0.0.1", Port: 8081, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	input := RegistrationInput{ServiceName: "orders", Address: "127.0.0.1", Port: 8082, Token: strings.Repeat("a", 64)}
	first, err := store.RegisterInstance(input)
	if err != nil {
		t.Fatal(err)
	}
	registeredVersion := store.Snapshot().Version
	var beforeChanges int64
	if err := store.db.QueryRow("SELECT total_changes()").Scan(&beforeChanges); err != nil {
		t.Fatal(err)
	}
	second, err := store.RegisterInstance(input)
	if err != nil || second.InstanceID != first.InstanceID {
		t.Fatalf("registration retry was not idempotent: %+v, %v", second, err)
	}
	if store.Snapshot().Version != registeredVersion {
		t.Fatal("lease renewal should not trigger a new agent snapshot")
	}
	var afterChanges int64
	if err := store.db.QueryRow("SELECT total_changes()").Scan(&afterChanges); err != nil {
		t.Fatal(err)
	}
	if afterChanges-beforeChanges > 1 {
		t.Fatalf("lease renewal changed %d SQL rows, expected at most one", afterChanges-beforeChanges)
	}
	if _, err := store.RenewRegistration(first.InstanceID, strings.Repeat("b", 64)); err == nil {
		t.Fatal("wrong token renewed a registration")
	}
	if _, err := store.RenewRegistration(first.InstanceID, input.Token); err != nil {
		t.Fatal(err)
	}
	if store.Snapshot().Version != registeredVersion {
		t.Fatal("explicit renewal should not trigger a new agent snapshot")
	}
	if len(store.Snapshot().Services[0].Instances) != 2 {
		t.Fatal("registration was not stored")
	}
	if err := store.ExpireLeases(time.Now().Add(LeaseTTL + time.Second)); err != nil {
		t.Fatal(err)
	}
	remaining := store.Snapshot().Services[0].Instances
	if len(remaining) != 1 || remaining[0].Port != 8081 {
		t.Fatalf("expired registration was not removed: %+v", remaining)
	}
	_, err = store.RenewRegistration(first.InstanceID, input.Token)
	var domain DomainError
	if !errors.As(err, &domain) || domain.Code != "not_found" {
		t.Fatalf("expected not_found after expiry, got %v", err)
	}
	registered, err := store.RegisterInstance(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeregisterInstance(registered.InstanceID, input.Token); err != nil {
		t.Fatal(err)
	}
	if len(store.Snapshot().Services[0].Instances) != 1 {
		t.Fatal("deregister did not remove the SDK instance")
	}
}

func TestExpiredRegistrationNeedsFreshHealthCheck(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveService("", Service{Name: "orders"}); err != nil {
		t.Fatal(err)
	}
	input := RegistrationInput{ServiceName: "orders", Address: "127.0.0.1", Port: 8082, Token: strings.Repeat("c", 64)}
	registered, err := store.RegisterInstance(input)
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	oldGeneration := store.data.Services[0].Instances[0].ProbeGeneration
	store.data.Services[0].Instances[0].Health = HealthHealthy
	store.data.Services[0].Instances[0].LeaseExpiresAt = time.Now().Add(-time.Second).Unix()
	version := store.data.Version
	store.mu.Unlock()
	result, err := store.RegisterInstance(input)
	if err != nil || result.InstanceID != registered.InstanceID {
		t.Fatalf("re-register expired lease: %+v, %v", result, err)
	}
	snapshot := store.Snapshot()
	if snapshot.Services[0].Instances[0].Health != HealthUnknown || snapshot.Version != version+1 {
		t.Fatalf("expired registration was not removed from discovery until rechecked: %+v", snapshot)
	}
	if err := store.ApplyProbeObservations([]ProbeObservation{{
		InstanceID: registered.InstanceID, Address: input.Address, Port: input.Port,
		HealthCheck: HealthCheck{Type: "tcp"}, Status: HealthHealthy, Generation: oldGeneration,
	}}); err != nil {
		t.Fatal(err)
	}
	if store.Snapshot().Services[0].Instances[0].Health != HealthUnknown {
		t.Fatal("a probe from the expired lease made its renewed registration available")
	}
}
