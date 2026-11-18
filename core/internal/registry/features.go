package registry

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"time"
)

const DefaultScope = "default"

func scope(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultScope
	}
	return strings.ToLower(value)
}

func validLabel(value string) bool {
	if len(value) > 80 {
		return false
	}
	for _, r := range value {
		if r < 33 || r == '/' || r == '\\' {
			return false
		}
	}
	return value != ""
}

func normalizeInstance(instance *Instance) error {
	instance.Metadata = maps.Clone(instance.Metadata)
	if instance.Weight != nil {
		weight := *instance.Weight
		instance.Weight = &weight
	}
	if instance.Weight == nil {
		weight := 100
		instance.Weight = &weight
	}
	if *instance.Weight < 0 || *instance.Weight > 10000 || len(instance.Version) > 80 {
		return bad("invalid_instance_metadata")
	}
	if instance.Lifecycle == "" {
		instance.Lifecycle = "active"
	}
	if instance.Lifecycle != "active" && instance.Lifecycle != "draining" {
		return bad("invalid_lifecycle")
	}
	encoded, err := json.Marshal(instance.Metadata)
	if err != nil || len(encoded) > 8192 || len(instance.Metadata) > 32 {
		return bad("invalid_instance_metadata")
	}
	for key, value := range instance.Metadata {
		if !validLabel(key) || len(value) > 512 {
			return bad("invalid_instance_metadata")
		}
	}
	return nil
}

// publish runs with s.mu held and wakes long polls only for discovery changes.
func (s *Store) publish(next Snapshot) {
	previous := s.data.Version
	s.data = next
	presentInstances := make(map[string]bool)
	presentServices := make(map[string]bool)
	for _, service := range next.Services {
		presentServices[service.ID] = true
		for _, instance := range service.Instances {
			presentInstances[instance.ID] = true
		}
	}
	for id := range s.beats {
		if !presentInstances[id] {
			delete(s.beats, id)
		}
	}
	for id := range s.protections {
		if !presentServices[id] {
			delete(s.protections, id)
		}
	}
	if previous != next.Version && s.changed != nil {
		close(s.changed)
		s.changed = make(chan struct{})
	}
}

func (s *Store) Watch(ctx context.Context, version uint64) (Snapshot, bool) {
	for {
		s.mu.RLock()
		if version == 0 || s.data.Version != version {
			snapshot := clone(s.data)
			s.mu.RUnlock()
			return snapshot, true
		}
		changed := s.changed
		s.mu.RUnlock()
		select {
		case <-ctx.Done():
			return Snapshot{}, false
		case <-changed:
		}
	}
}

// DrainRegistration stops new discoveries while preserving the renewable lease.
func (s *Store) DrainRegistration(id, token string) error {
	hash, err := registrationHash(token)
	if err != nil {
		return err
	}
	return s.Update(func(snapshot *Snapshot) error {
		for i := range snapshot.Services {
			for j := range snapshot.Services[i].Instances {
				instance := &snapshot.Services[i].Instances[j]
				if instance.ID != id {
					continue
				}
				if instance.Source != "sdk" || instance.LeaseTokenHash != hash {
					return bad("registration_forbidden")
				}
				instance.Lifecycle = "draining"
				return nil
			}
		}
		return bad("not_found")
	})
}

type ResolveQuery struct {
	Service    string            `json:"service"`
	Namespace  string            `json:"namespace,omitempty"`
	Group      string            `json:"group,omitempty"`
	Version    string            `json:"version,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	AllowStale bool              `json:"allowStale,omitempty"`
}

type ResolvedEndpoint struct {
	InstanceID string            `json:"instanceId"`
	Address    string            `json:"address"`
	Port       int               `json:"port"`
	Version    string            `json:"version,omitempty"`
	Weight     int               `json:"weight"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Stale      bool              `json:"stale"`
}

type ResolveResult struct {
	RegistryVersion uint64             `json:"registryVersion"`
	Endpoints       []ResolvedEndpoint `json:"endpoints"`
	Stale           bool               `json:"stale"`
}

func Resolve(snapshot Snapshot, query ResolveQuery, now time.Time) (ResolveResult, error) {
	result := ResolveResult{RegistryVersion: snapshot.Version, Endpoints: []ResolvedEndpoint{}}
	for _, service := range snapshot.Services {
		if service.Name != query.Service || scope(service.Namespace) != scope(query.Namespace) || scope(service.Group) != scope(query.Group) {
			continue
		}
		var historical []ResolvedEndpoint
		for _, instance := range service.Instances {
			weight := 100
			if instance.Weight != nil {
				weight = *instance.Weight
			}
			if !instance.Enabled || instance.Lifecycle == "draining" || weight == 0 || query.Version != "" && query.Version != instance.Version {
				continue
			}
			matches := true
			for key, value := range query.Metadata {
				if actual, ok := instance.Metadata[key]; !ok || actual != value {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			endpoint := ResolvedEndpoint{InstanceID: instance.ID, Address: instance.Address, Port: instance.Port, Version: instance.Version, Weight: weight, Metadata: maps.Clone(instance.Metadata)}
			if instance.Health == HealthHealthy && !instance.LeaseExpired {
				result.Endpoints = append(result.Endpoints, endpoint)
				continue
			}
			if query.AllowStale && service.Protection.AllowStale && service.ProtectionStatus.Active && instance.LastHealthyAt > 0 && now.UnixMilli()-instance.LastHealthyAt <= int64(service.Protection.MaxStaleSeconds)*1000 {
				endpoint.Stale = true
				historical = append(historical, endpoint)
			}
		}
		if len(result.Endpoints) == 0 && len(historical) > 0 {
			result.Endpoints = historical
			result.Stale = true
		}
		return result, nil
	}
	return result, bad("not_found")
}

func (s *Store) SetInstanceState(serviceID, id string, enabled *bool, draining bool) error {
	return s.Update(func(snapshot *Snapshot) error {
		for i := range snapshot.Services {
			if snapshot.Services[i].ID == serviceID {
				for j := range snapshot.Services[i].Instances {
					instance := &snapshot.Services[i].Instances[j]
					if instance.ID != id {
						continue
					}
					if enabled != nil {
						instance.Enabled = *enabled
						instance.ProbeGeneration++
						instance.Health = HealthUnknown
						instance.LastHealthyAt = 0
						instance.LastCheckAt = 0
						instance.LastError = ""
						instance.ConsecutiveFailures = 0
						instance.ConsecutiveSuccesses = 0
					}
					if draining {
						instance.Lifecycle = "draining"
					}
					return nil
				}
			}
		}
		return bad("not_found")
	})
}
