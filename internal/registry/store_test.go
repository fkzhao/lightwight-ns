package registry

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestStorePersistenceAndPortReservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err := store.SaveService("", Service{Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	machine, err := store.SaveMachine("", Machine{Name: "node-1", Address: "10.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := store.SaveInstance("", Instance{ServiceID: service.ID, MachineID: machine.ID, Port: 8080, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveInstance("", Instance{ServiceID: service.ID, MachineID: machine.ID, Port: 8080})
	var domain DomainError
	if !errors.As(err, &domain) || domain.Code != "port_in_use" {
		t.Fatalf("expected port_in_use, got %v", err)
	}
	if err := store.DeleteMachine(machine.ID); !errors.As(err, &domain) || domain.Code != "machine_has_instances" {
		t.Fatalf("expected machine_has_instances, got %v", err)
	}
	copy := store.Snapshot()
	copy.Services[0].Name = "changed"
	if store.Snapshot().Services[0].Name != "orders" {
		t.Fatal("snapshot exposed mutable state")
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Snapshot()
	if got.Version != 3 || len(got.Instances) != 1 || got.Instances[0].ID != instance.ID {
		t.Fatalf("unexpected persisted snapshot: %+v", got)
	}
}
