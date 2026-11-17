package registry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Service struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Machine struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Address     string `json:"address"`
	Description string `json:"description"`
}

type Instance struct {
	ID        string `json:"id"`
	ServiceID string `json:"serviceId"`
	MachineID string `json:"machineId"`
	Port      int    `json:"port"`
	Enabled   bool   `json:"enabled"`
}

type Snapshot struct {
	Version   uint64     `json:"version"`
	Services  []Service  `json:"services"`
	Machines  []Machine  `json:"machines"`
	Instances []Instance `json:"instances"`
}

type DomainError struct {
	Code string
}

func (e DomainError) Error() string { return e.Code }

func bad(code string) error { return DomainError{Code: code} }

type Store struct {
	mu   sync.RWMutex
	path string
	data Snapshot
}

func NewStore(path string) (*Store, error) {
	s := &Store{path: path, data: Snapshot{Services: []Service{}, Machines: []Machine{}, Instances: []Instance{}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("decode registry: %w", err)
	}
	s.data = clone(s.data)
	return s, nil
}

func clone(src Snapshot) Snapshot {
	src.Services = append([]Service{}, src.Services...)
	src.Machines = append([]Machine{}, src.Machines...)
	src.Instances = append([]Instance{}, src.Instances...)
	return src
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data)
}

// Update persists a new snapshot through a temporary file before swapping it into memory.
func (s *Store) Update(change func(*Snapshot) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data)
	if err := change(&next); err != nil {
		return err
	}
	next.Version++
	if err := os.MkdirAll(filepath.Dir(s.path), 0750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".registry-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	s.data = next
	return nil
}

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (s *Store) SaveService(id string, item Service) (Service, error) {
	item.Name = strings.TrimSpace(item.Name)
	item.Description = strings.TrimSpace(item.Description)
	if item.Name == "" || len(item.Name) > 80 {
		return Service{}, bad("invalid_service_name")
	}
	if len(item.Description) > 300 {
		return Service{}, bad("invalid_description")
	}
	err := s.Update(func(d *Snapshot) error {
		for _, existing := range d.Services {
			if existing.ID != id && strings.EqualFold(existing.Name, item.Name) {
				return bad("duplicate_service_name")
			}
		}
		if id == "" {
			item.ID = newID()
			d.Services = append(d.Services, item)
			return nil
		}
		for i := range d.Services {
			if d.Services[i].ID == id {
				item.ID = id
				d.Services[i] = item
				return nil
			}
		}
		return bad("not_found")
	})
	return item, err
}

func (s *Store) DeleteService(id string) error {
	return s.Update(func(d *Snapshot) error {
		for _, item := range d.Instances {
			if item.ServiceID == id {
				return bad("service_has_instances")
			}
		}
		for i, item := range d.Services {
			if item.ID == id {
				d.Services = append(d.Services[:i], d.Services[i+1:]...)
				return nil
			}
		}
		return bad("not_found")
	})
}

func (s *Store) SaveMachine(id string, item Machine) (Machine, error) {
	item.Name = strings.TrimSpace(item.Name)
	item.Address = strings.TrimSpace(item.Address)
	item.Description = strings.TrimSpace(item.Description)
	if item.Name == "" || len(item.Name) > 80 {
		return Machine{}, bad("invalid_machine_name")
	}
	if net.ParseIP(item.Address) == nil {
		return Machine{}, bad("invalid_address")
	}
	if len(item.Description) > 300 {
		return Machine{}, bad("invalid_description")
	}
	err := s.Update(func(d *Snapshot) error {
		for _, existing := range d.Machines {
			if existing.ID != id && (strings.EqualFold(existing.Name, item.Name) || existing.Address == item.Address) {
				return bad("duplicate_machine")
			}
		}
		if id == "" {
			item.ID = newID()
			d.Machines = append(d.Machines, item)
			return nil
		}
		for i := range d.Machines {
			if d.Machines[i].ID == id {
				item.ID = id
				d.Machines[i] = item
				return nil
			}
		}
		return bad("not_found")
	})
	return item, err
}

func (s *Store) DeleteMachine(id string) error {
	return s.Update(func(d *Snapshot) error {
		for _, item := range d.Instances {
			if item.MachineID == id {
				return bad("machine_has_instances")
			}
		}
		for i, item := range d.Machines {
			if item.ID == id {
				d.Machines = append(d.Machines[:i], d.Machines[i+1:]...)
				return nil
			}
		}
		return bad("not_found")
	})
}

func (s *Store) SaveInstance(id string, item Instance) (Instance, error) {
	if item.Port < 1 || item.Port > 65535 {
		return Instance{}, bad("invalid_port")
	}
	err := s.Update(func(d *Snapshot) error {
		serviceFound, machineFound := false, false
		for _, service := range d.Services {
			serviceFound = serviceFound || service.ID == item.ServiceID
		}
		for _, machine := range d.Machines {
			machineFound = machineFound || machine.ID == item.MachineID
		}
		if !serviceFound || !machineFound {
			return bad("invalid_reference")
		}
		for _, existing := range d.Instances {
			if existing.ID != id && existing.MachineID == item.MachineID && existing.Port == item.Port {
				return bad("port_in_use")
			}
		}
		if id == "" {
			item.ID = newID()
			d.Instances = append(d.Instances, item)
			return nil
		}
		for i := range d.Instances {
			if d.Instances[i].ID == id {
				item.ID = id
				d.Instances[i] = item
				return nil
			}
		}
		return bad("not_found")
	})
	return item, err
}

func (s *Store) DeleteInstance(id string) error {
	return s.Update(func(d *Snapshot) error {
		for i, item := range d.Instances {
			if item.ID == id {
				d.Instances = append(d.Instances[:i], d.Instances[i+1:]...)
				return nil
			}
		}
		return bad("not_found")
	})
}
