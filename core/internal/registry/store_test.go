package registry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceEndpointsAndHealthPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
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
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Snapshot()
	if got.Version != version+1 || len(got.Services[0].Instances) != 1 || got.Services[0].Instances[0].Health != HealthUnknown {
		t.Fatalf("unexpected reloaded snapshot: %+v", got)
	}
	t.Cleanup(func() { reloaded.Close() })
}

func TestLegacySnapshotMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
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
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "registry.json"), b, 0600); err != nil {
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
	t.Cleanup(func() { store.Close() })
}

func TestSQLiteWALAndNestedJSONMigration(t *testing.T) {
	directory := t.TempDir()
	dbPath := filepath.Join(directory, "registry.db")
	jsonPath := filepath.Join(directory, "registry.json")
	legacy := Snapshot{Version: 4, Services: []Service{{
		ID: "service-1", Name: "orders", HealthCheck: HealthCheck{Type: "tcp"},
		Instances: []Instance{{ID: "instance-1", Address: "10.0.0.1", Port: 8080, Enabled: true, Health: HealthHealthy}},
	}}}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var journal string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
		t.Fatalf("expected WAL mode, got %q, %v", journal, err)
	}
	got := store.Snapshot()
	if got.Version != 5 || len(got.Services) != 1 || got.Services[0].Instances[0].Health != HealthUnknown {
		t.Fatalf("nested JSON import failed: %+v", got)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, []byte("invalid after completed migration"), 0600); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("existing database should not import JSON again: %v", err)
	}
	t.Cleanup(func() { reloaded.Close() })
	got = reloaded.Snapshot()
	if got.Version != 6 || len(got.Services) != 1 || len(got.Services[0].Instances) != 1 {
		t.Fatalf("database did not preserve migrated data: %+v", got)
	}
}

func TestProbeObservationRejectsChangedEndpoint(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	service, err := store.SaveService("", Service{Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := store.SaveInstance(service.ID, "", Instance{Address: "10.0.0.1", Port: 8080, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	old := ProbeObservation{InstanceID: instance.ID, Address: instance.Address, Port: instance.Port, HealthCheck: service.HealthCheck, Status: HealthHealthy, Generation: instance.ProbeGeneration}
	if _, err := store.SaveInstance(service.ID, instance.ID, Instance{Address: "10.0.0.2", Port: 8080, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	version := store.Snapshot().Version
	if err := store.ApplyProbeObservations([]ProbeObservation{old}); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot()
	if got.Version != version || got.Services[0].Instances[0].Health != HealthUnknown {
		t.Fatalf("stale probe changed current endpoint: %+v", got)
	}
}

func TestFailedMigrationCanBeRetried(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "registry.db")
	legacyPath := filepath.Join(directory, "registry.json")
	if err := os.WriteFile(legacyPath, []byte("broken JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if store, err := NewStore(path); err == nil {
		store.Close()
		t.Fatal("invalid JSON was silently ignored")
	}
	if err := os.WriteFile(legacyPath, []byte(`{"version":9,"services":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("migration retry failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if got := store.Snapshot().Version; got != 10 {
		t.Fatalf("migration marker was committed before data: version = %d", got)
	}
}

func TestFailedWriteDoesNotPublishCache(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	version := store.Snapshot().Version
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveService("", Service{Name: "orders"}); err == nil {
		t.Fatal("closed database accepted a write")
	}
	got := store.Snapshot()
	if len(got.Services) != 0 || got.Version != version {
		t.Fatalf("uncommitted change became visible: %+v", got)
	}
}
