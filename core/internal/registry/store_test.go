package registry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceEndpointsAndHealthPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err := store.SaveService("", Service{Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := store.SaveInstance(service.ID, "", Instance{Address: "10.0.0.1", Port: 8080, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if instance.Health != HealthUnknown {
		t.Fatalf("new instance health = %s", instance.Health)
	}
	if err := store.ApplyHealth(store.Snapshot().Version, map[string]string{instance.ID: HealthHealthy}); err != nil {
		t.Fatal(err)
	}
	if store.Snapshot().Services[0].Instances[0].Health != HealthHealthy {
		t.Fatal("health did not update")
	}
	version := store.Snapshot().Version
	if err := store.ApplyHealth(version-1, map[string]string{instance.ID: HealthUnhealthy}); err != nil {
		t.Fatal(err)
	}
	if store.Snapshot().Services[0].Instances[0].Health != HealthHealthy {
		t.Fatal("stale health observation overwrote current state")
	}
	if err := store.ApplyHealth(store.Snapshot().Version, map[string]string{instance.ID: HealthHealthy}); err != nil {
		t.Fatal(err)
	}
	if store.Snapshot().Version != version {
		t.Fatal("unchanged health bumped the version")
	}
	_, err = store.SaveInstance(service.ID, "", Instance{Address: "10.0.0.1", Port: 8080, Enabled: true})
	var domain DomainError
	if !errors.As(err, &domain) || domain.Code != "port_in_use" {
		t.Fatalf("expected port_in_use, got %v", err)
	}
	copy := store.Snapshot()
	copy.Services[0].Instances[0].Address = "changed"
	if store.Snapshot().Services[0].Instances[0].Address != "10.0.0.1" {
		t.Fatal("snapshot exposed mutable state")
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Snapshot()
	if got.Version != version+1 || len(got.Services[0].Instances) != 1 || got.Services[0].Instances[0].Health != HealthUnknown {
		t.Fatalf("unexpected reloaded snapshot: %+v", got)
	}
}

func TestLegacySnapshotMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	legacy := map[string]any{
		"version":   7,
		"services":  []map[string]any{{"id": "service-1", "name": "orders", "description": ""}},
		"machines":  []map[string]any{{"id": "machine-1", "name": "node-1", "address": "10.0.0.1"}},
		"instances": []map[string]any{{"id": "instance-1", "serviceId": "service-1", "machineId": "machine-1", "port": 8080, "enabled": true}},
	}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot()
	if got.Version != 8 || got.Services[0].Instances[0].Address != "10.0.0.1" || got.Services[0].HealthCheck.Type != "tcp" {
		t.Fatalf("legacy data was not preserved: %+v", got)
	}
}
