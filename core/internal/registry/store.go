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

const (
	HealthUnknown   = "unknown"
	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy"
)

type HealthCheck struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type Instance struct {
	ID             string `json:"id"`
	Address        string `json:"address"`
	Port           int    `json:"port"`
	Enabled        bool   `json:"enabled"`
	Health         string `json:"health"`
	Source         string `json:"source,omitempty"`
	LeaseTokenHash string `json:"leaseTokenHash,omitempty"`
	LeaseExpiresAt int64  `json:"leaseExpiresAt,omitempty"`
}

type Service struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	HealthCheck HealthCheck `json:"healthCheck"`
	Instances   []Instance  `json:"instances"`
}

type Snapshot struct {
	Version  uint64    `json:"version"`
	Services []Service `json:"services"`
}

type DomainError struct{ Code string }

func (e DomainError) Error() string { return e.Code }
func bad(code string) error         { return DomainError{Code: code} }

type Store struct {
	mu   sync.RWMutex
	path string
	data Snapshot
}

func NewStore(path string) (*Store, error) {
	s := &Store{path: path, data: Snapshot{Services: []Service{}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var disk struct {
		Version  uint64    `json:"version"`
		Services []Service `json:"services"`
		Machines []struct {
			ID      string `json:"id"`
			Address string `json:"address"`
		} `json:"machines"`
		Instances []struct {
			ID        string `json:"id"`
			ServiceID string `json:"serviceId"`
			MachineID string `json:"machineId"`
			Port      int    `json:"port"`
			Enabled   bool   `json:"enabled"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(b, &disk); err != nil {
		return nil, fmt.Errorf("decode registry: %w", err)
	}
	s.data = Snapshot{Version: disk.Version, Services: disk.Services}
	if s.data.Services == nil {
		s.data.Services = []Service{}
	}
	machines := make(map[string]string, len(disk.Machines))
	for _, machine := range disk.Machines {
		machines[machine.ID] = machine.Address
	}
	for _, old := range disk.Instances {
		address, ok := machines[old.MachineID]
		if !ok {
			return nil, fmt.Errorf("legacy instance %s references a missing machine", old.ID)
		}
		found := false
		for i := range s.data.Services {
			if s.data.Services[i].ID == old.ServiceID {
				s.data.Services[i].Instances = append(s.data.Services[i].Instances, Instance{ID: old.ID, Address: address, Port: old.Port, Enabled: old.Enabled})
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("legacy instance %s references a missing service", old.ID)
		}
	}
	for i := range s.data.Services {
		service := &s.data.Services[i]
		if service.HealthCheck.Type == "" {
			service.HealthCheck.Type = "tcp"
		}
		if service.Instances == nil {
			service.Instances = []Instance{}
		}
		for j := range service.Instances {
			service.Instances[j].Health = HealthUnknown
		}
	}
	// A restart invalidates persisted health observations and advances the agent revision.
	s.data.Version++
	if err := persist(path, s.data); err != nil {
		return nil, err
	}
	return s, nil
}

func clone(src Snapshot) Snapshot {
	services := make([]Service, len(src.Services))
	for i, service := range src.Services {
		services[i] = service
		services[i].Instances = append([]Instance{}, service.Instances...)
	}
	src.Services = services
	return src
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data)
}

func persist(path string, value Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".registry-*")
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
	return os.Rename(tmp.Name(), path)
}

// Update persists a new snapshot before exposing it to concurrent readers.
func (s *Store) Update(change func(*Snapshot) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data)
	if err := change(&next); err != nil {
		return err
	}
	next.Version++
	if err := persist(s.path, next); err != nil {
		return err
	}
	s.data = next
	return nil
}

// ApplyHealth writes only current observations that change instance health.
func (s *Store) ApplyHealth(observedVersion uint64, results map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Version != observedVersion {
		return nil
	}
	next := clone(s.data)
	changed := false
	for i := range next.Services {
		for j := range next.Services[i].Instances {
			instance := &next.Services[i].Instances[j]
			status, ok := results[instance.ID]
			if ok && instance.Enabled && (status == HealthHealthy || status == HealthUnhealthy) && instance.Health != status {
				instance.Health = status
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	next.Version++
	if err := persist(s.path, next); err != nil {
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

func normalizeHealthCheck(check HealthCheck) (HealthCheck, error) {
	if check.Type == "" {
		check.Type = "tcp"
	}
	if check.Type != "tcp" && check.Type != "http" {
		return HealthCheck{}, bad("invalid_health_type")
	}
	if check.Type == "tcp" {
		check.Path = ""
		return check, nil
	}
	if check.Path == "" {
		check.Path = "/health"
	}
	if !strings.HasPrefix(check.Path, "/") || strings.HasPrefix(check.Path, "//") || len(check.Path) > 200 {
		return HealthCheck{}, bad("invalid_health_path")
	}
	return check, nil
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
	check, err := normalizeHealthCheck(item.HealthCheck)
	if err != nil {
		return Service{}, err
	}
	item.HealthCheck = check
	err = s.Update(func(d *Snapshot) error {
		for _, existing := range d.Services {
			if existing.ID != id && strings.EqualFold(existing.Name, item.Name) {
				return bad("duplicate_service_name")
			}
		}
		if id == "" {
			item.ID = newID()
			item.Instances = []Instance{}
			d.Services = append(d.Services, item)
			return nil
		}
		for i := range d.Services {
			if d.Services[i].ID == id {
				item.ID = id
				item.Instances = d.Services[i].Instances
				if item.HealthCheck != d.Services[i].HealthCheck {
					for j := range item.Instances {
						item.Instances[j].Health = HealthUnknown
					}
				}
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
		for i, item := range d.Services {
			if item.ID == id {
				d.Services = append(d.Services[:i], d.Services[i+1:]...)
				return nil
			}
		}
		return bad("not_found")
	})
}

func (s *Store) SaveInstance(serviceID, id string, item Instance) (Instance, error) {
	address := net.ParseIP(strings.TrimSpace(item.Address))
	if address == nil {
		return Instance{}, bad("invalid_address")
	}
	item.Address = address.String()
	if item.Port < 1 || item.Port > 65535 {
		return Instance{}, bad("invalid_port")
	}
	item.Health = HealthUnknown
	item.Source = ""
	item.LeaseTokenHash = ""
	item.LeaseExpiresAt = 0
	err := s.Update(func(d *Snapshot) error {
		serviceIndex := -1
		for i, service := range d.Services {
			if service.ID == serviceID {
				serviceIndex = i
			}
			for _, existing := range service.Instances {
				if existing.ID != id && existing.Address == item.Address && existing.Port == item.Port {
					return bad("port_in_use")
				}
			}
		}
		if serviceIndex == -1 {
			return bad("not_found")
		}
		if id == "" {
			item.ID = newID()
			d.Services[serviceIndex].Instances = append(d.Services[serviceIndex].Instances, item)
			return nil
		}
		for i := range d.Services[serviceIndex].Instances {
			if d.Services[serviceIndex].Instances[i].ID == id {
				if d.Services[serviceIndex].Instances[i].Source == "sdk" {
					return bad("managed_instance")
				}
				item.ID = id
				d.Services[serviceIndex].Instances[i] = item
				return nil
			}
		}
		return bad("not_found")
	})
	return item, err
}

func (s *Store) DeleteInstance(serviceID, id string) error {
	return s.Update(func(d *Snapshot) error {
		for i := range d.Services {
			if d.Services[i].ID != serviceID {
				continue
			}
			for j, instance := range d.Services[i].Instances {
				if instance.ID == id {
					d.Services[i].Instances = append(d.Services[i].Instances[:j], d.Services[i].Instances[j+1:]...)
					return nil
				}
			}
			break
		}
		return bad("not_found")
	})
}
