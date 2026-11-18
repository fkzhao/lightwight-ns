package registry

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net"
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
	ID              string `json:"id"`
	Address         string `json:"address"`
	Port            int    `json:"port"`
	Enabled         bool   `json:"enabled"`
	Health          string `json:"health"`
	Source          string `json:"source,omitempty"`
	LeaseTokenHash  string `json:"leaseTokenHash,omitempty"`
	LeaseExpiresAt  int64  `json:"leaseExpiresAt,omitempty"`
	ProbeGeneration uint64 `json:"-"`
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
	db   *sql.DB
	data Snapshot
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

// SnapshotSince avoids cloning the registry when an agent already has its version.
func (s *Store) SnapshotSince(version uint64) (Snapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if version != 0 && s.data.Version == version {
		return Snapshot{}, false
	}
	return clone(s.data), true
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
	if err := s.commit(next); err != nil {
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
	if err := s.commit(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

type ProbeObservation struct {
	InstanceID  string
	Address     string
	Port        int
	HealthCheck HealthCheck
	Status      string
	Generation  uint64
}

// ApplyProbeObservations accepts results only if the probed endpoint and policy still match.
func (s *Store) ApplyProbeObservations(observations []ProbeObservation) error {
	if len(observations) == 0 {
		return nil
	}
	latest := make(map[string]ProbeObservation, len(observations))
	for _, observation := range observations {
		latest[observation.InstanceID] = observation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data)
	changed := false
	for i := range next.Services {
		service := &next.Services[i]
		for j := range service.Instances {
			instance := &service.Instances[j]
			observation, ok := latest[instance.ID]
			if !ok || !instance.Enabled || instance.ProbeGeneration != observation.Generation || instance.Address != observation.Address || instance.Port != observation.Port || service.HealthCheck != observation.HealthCheck {
				continue
			}
			if (observation.Status == HealthHealthy || observation.Status == HealthUnhealthy) && instance.Health != observation.Status {
				instance.Health = observation.Status
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	next.Version++
	if err := s.commit(next); err != nil {
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
						item.Instances[j].ProbeGeneration++
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
			item.ProbeGeneration = 1
			d.Services[serviceIndex].Instances = append(d.Services[serviceIndex].Instances, item)
			return nil
		}
		for i := range d.Services[serviceIndex].Instances {
			if d.Services[serviceIndex].Instances[i].ID == id {
				if d.Services[serviceIndex].Instances[i].Source == "sdk" {
					return bad("managed_instance")
				}
				item.ID = id
				item.ProbeGeneration = d.Services[serviceIndex].Instances[i].ProbeGeneration + 1
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
