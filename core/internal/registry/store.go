package registry

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"maps"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	HealthUnknown   = "unknown"
	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy"
)

type HealthCheck struct {
	Type             string `json:"type"`
	Path             string `json:"path"`
	FailureThreshold int    `json:"failureThreshold"`
	SuccessThreshold int    `json:"successThreshold"`
	IntervalMillis   int    `json:"intervalMillis,omitempty"`
	TimeoutMillis    int    `json:"timeoutMillis,omitempty"`
	GRPCService      string `json:"grpcService,omitempty"`
	TLS              bool   `json:"tls,omitempty"`
	ServerName       string `json:"serverName,omitempty"`
	CAPEM            string `json:"caPem,omitempty"`
}

type Instance struct {
	ID                   string            `json:"id"`
	Address              string            `json:"address"`
	Port                 int               `json:"port"`
	Enabled              bool              `json:"enabled"`
	Health               string            `json:"health"`
	Source               string            `json:"source,omitempty"`
	LeaseTokenHash       string            `json:"leaseTokenHash,omitempty"`
	LeaseExpiresAt       int64             `json:"leaseExpiresAt,omitempty"`
	ProbeGeneration      uint64            `json:"-"`
	Version              string            `json:"version,omitempty"`
	Weight               *int              `json:"weight,omitempty"`
	Metadata             map[string]string `json:"metadata,omitempty"`
	Lifecycle            string            `json:"lifecycle"`
	LeaseExpired         bool              `json:"leaseExpired,omitempty"`
	LastCheckAt          int64             `json:"lastCheckAt,omitempty"`
	LastHealthyAt        int64             `json:"lastHealthyAt,omitempty"`
	LastError            string            `json:"lastError,omitempty"`
	DurationMillis       int64             `json:"durationMillis,omitempty"`
	ConsecutiveFailures  int               `json:"consecutiveFailures"`
	ConsecutiveSuccesses int               `json:"consecutiveSuccesses"`
}

type Service struct {
	ID               string           `json:"id"`
	Namespace        string           `json:"namespace"`
	Group            string           `json:"group"`
	Protection       ProtectionPolicy `json:"protection"`
	ProtectionStatus ProtectionStatus `json:"protectionStatus"`
	Name             string           `json:"name"`
	Description      string           `json:"description"`
	HealthCheck      HealthCheck      `json:"healthCheck"`
	Instances        []Instance       `json:"instances"`
}

type Snapshot struct {
	Version  uint64    `json:"version"`
	Services []Service `json:"services"`
}

type DomainError struct{ Code string }

func (e DomainError) Error() string { return e.Code }
func bad(code string) error         { return DomainError{Code: code} }

type Store struct {
	mu          sync.RWMutex
	db          *sql.DB
	data        Snapshot
	changed     chan struct{}
	startedAt   time.Time
	beats       map[string]time.Time
	protections map[string]ProtectionStatus
}

func clone(src Snapshot) Snapshot {
	services := make([]Service, len(src.Services))
	for i, service := range src.Services {
		services[i] = service
		services[i].Instances = append([]Instance{}, service.Instances...)
		for j := range services[i].Instances {
			instance := &services[i].Instances[j]
			instance.Metadata = maps.Clone(instance.Metadata)
			if instance.Weight != nil {
				weight := *instance.Weight
				instance.Weight = &weight
			}
		}
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
func (s *Store) Update(change func(*Snapshot) error) error { return s.update(change, false) }

func (s *Store) update(change func(*Snapshot) error, retireRemoved bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data)
	if err := change(&next); err != nil {
		return err
	}
	next.Version++
	if err := s.commitWithRetired(next, retireRemoved); err != nil {
		return err
	}
	s.publish(next)
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
	s.publish(next)
	return nil
}

type ProbeObservation struct {
	InstanceID     string
	Address        string
	Port           int
	HealthCheck    HealthCheck
	Status         string
	Generation     uint64
	Error          string
	DurationMillis int64
	CheckedAt      int64
}

// ApplyProbeObservations accepts results only if the probed endpoint and policy still match.
func (s *Store) ApplyProbeObservations(observations []ProbeObservation) error {
	if len(observations) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data)
	changed := false
	for _, observation := range observations {
		for i := range next.Services {
			service := &next.Services[i]
			for j := range service.Instances {
				instance := &service.Instances[j]
				if instance.ID != observation.InstanceID || !instance.Enabled || instance.ProbeGeneration != observation.Generation || instance.Address != observation.Address || instance.Port != observation.Port || service.HealthCheck != observation.HealthCheck {
					continue
				}
				if observation.Status != HealthHealthy && observation.Status != HealthUnhealthy {
					continue
				}
				instance.LastCheckAt = observation.CheckedAt
				if instance.LastCheckAt == 0 {
					instance.LastCheckAt = time.Now().UnixMilli()
				}
				instance.LastError = observation.Error
				instance.DurationMillis = observation.DurationMillis
				previous := instance.Health
				if observation.Status == HealthHealthy {
					instance.ConsecutiveFailures = 0
					instance.ConsecutiveSuccesses++
					instance.LastHealthyAt = instance.LastCheckAt
					if instance.ConsecutiveSuccesses >= max(1, service.HealthCheck.SuccessThreshold) {
						instance.Health = HealthHealthy
					}
				} else {
					instance.ConsecutiveSuccesses = 0
					instance.ConsecutiveFailures++
					if instance.ConsecutiveFailures >= max(1, service.HealthCheck.FailureThreshold) {
						instance.Health = HealthUnhealthy
					}
				}
				changed = changed || previous != instance.Health
			}
		}
	}
	if changed {
		next.Version++
		if err := s.commit(next); err != nil {
			return err
		}
		s.publish(next)
	} else {
		s.data = next
	}
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
	if check.FailureThreshold == 0 {
		check.FailureThreshold = 3
	}
	if check.SuccessThreshold == 0 {
		check.SuccessThreshold = 2
	}
	if check.FailureThreshold < 1 || check.FailureThreshold > 20 || check.SuccessThreshold < 1 || check.SuccessThreshold > 20 || check.IntervalMillis < 0 || check.IntervalMillis > 3600000 || check.TimeoutMillis < 0 || check.TimeoutMillis > 60000 || (len(check.GRPCService) > 200 || len(check.ServerName) > 200) || len(check.CAPEM) > 65536 {
		return HealthCheck{}, bad("invalid_health_policy")
	}
	if check.Type != "tcp" && check.Type != "http" && check.Type != "grpc" {
		return HealthCheck{}, bad("invalid_health_type")
	}
	if check.Type != "http" {
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
	item.ProtectionStatus = ProtectionStatus{}
	item.HealthCheck = check
	item.Namespace = scope(item.Namespace)
	item.Group = scope(item.Group)
	if !validLabel(item.Namespace) || !validLabel(item.Group) {
		return Service{}, bad("invalid_scope")
	}
	item.Protection, err = normalizeProtection(item.Protection)
	if err != nil {
		return Service{}, err
	}
	err = s.Update(func(d *Snapshot) error {
		for _, existing := range d.Services {
			if existing.ID != id && strings.EqualFold(existing.Name, item.Name) && strings.EqualFold(scope(existing.Namespace), item.Namespace) && strings.EqualFold(scope(existing.Group), item.Group) {
				return bad("duplicate_service_name")
			}
		}
		if id == "" {
			item.ID = newID()
			item.Instances = []Instance{}
			d.Services = append(d.Services, item)
			item = clone(Snapshot{Services: []Service{item}}).Services[0]
			return nil
		}
		for i := range d.Services {
			if d.Services[i].ID == id {
				item.ID = id
				item.Instances = d.Services[i].Instances
				if item.Protection.Enabled {
					item.ProtectionStatus = d.Services[i].ProtectionStatus
				}
				if item.HealthCheck != d.Services[i].HealthCheck {
					for j := range item.Instances {
						item.Instances[j].Health = HealthUnknown
						item.Instances[j].ConsecutiveFailures = 0
						item.Instances[j].ConsecutiveSuccesses = 0
						item.Instances[j].LastHealthyAt = 0
						item.Instances[j].LastCheckAt = 0
						item.Instances[j].LastError = ""
						item.Instances[j].ProbeGeneration++
					}
				}
				d.Services[i] = item
				item = clone(Snapshot{Services: []Service{item}}).Services[0]
				return nil
			}
		}
		return bad("not_found")
	})
	return item, err
}

func (s *Store) DeleteService(id string) error {
	return s.update(func(d *Snapshot) error {
		for i, item := range d.Services {
			if item.ID == id {
				d.Services = append(d.Services[:i], d.Services[i+1:]...)
				return nil
			}
		}
		return bad("not_found")
	}, true)
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
	if err := normalizeInstance(&item); err != nil {
		return Instance{}, err
	}
	item.LastCheckAt = 0
	item.LastHealthyAt = 0
	item.LastError = ""
	item.ConsecutiveFailures = 0
	item.ConsecutiveSuccesses = 0
	item.LeaseExpired = false
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
			item = clone(Snapshot{Services: []Service{{Instances: []Instance{item}}}}).Services[0].Instances[0]
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
				item = clone(Snapshot{Services: []Service{{Instances: []Instance{item}}}}).Services[0].Instances[0]
				return nil
			}
		}
		return bad("not_found")
	})
	return item, err
}

func (s *Store) DeleteInstance(serviceID, id string) error {
	return s.update(func(d *Snapshot) error {
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
	}, true)
}
