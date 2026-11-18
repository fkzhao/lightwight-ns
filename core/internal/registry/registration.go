package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net"
	"strings"
	"time"
)

const LeaseTTL = 30 * time.Second

type RegistrationInput struct {
	ServiceName string            `json:"serviceName"`
	Namespace   string            `json:"namespace,omitempty"`
	Group       string            `json:"group,omitempty"`
	Version     string            `json:"version,omitempty"`
	Weight      *int              `json:"weight,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Address     string            `json:"address"`
	Port        int               `json:"port"`
	Token       string            `json:"token"`
}

type RegistrationResult struct {
	InstanceID      string `json:"instanceId"`
	LeaseTTLSeconds int    `json:"leaseTtlSeconds"`
	ExpiresAt       int64  `json:"expiresAt"`
}

type RegistrationToken struct {
	Token string `json:"token"`
}

func registrationHash(token string) (string, error) {
	if len(token) != 64 {
		return "", bad("invalid_registration_token")
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", bad("invalid_registration_token")
	}
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:]), nil
}

func (s *Store) RegisterInstance(input RegistrationInput) (RegistrationResult, error) {
	input.ServiceName = strings.TrimSpace(input.ServiceName)
	address := net.ParseIP(strings.TrimSpace(input.Address))
	if input.ServiceName == "" || address == nil || input.Port < 1 || input.Port > 65535 {
		return RegistrationResult{}, bad("invalid_registration")
	}
	input.Address = address.String()
	hash, err := registrationHash(input.Token)
	if err != nil {
		return RegistrationResult{}, err
	}
	candidate := Instance{Version: input.Version, Weight: input.Weight, Metadata: input.Metadata}
	if err := normalizeInstance(&candidate); err != nil {
		return RegistrationResult{}, err
	}
	now := time.Now()
	expires := now.Add(LeaseTTL).Unix()
	result := RegistrationResult{LeaseTTLSeconds: int(LeaseTTL.Seconds()), ExpiresAt: expires}
	s.mu.Lock()
	defer s.mu.Unlock()
	serviceIndex := -1
	for i, service := range s.data.Services {
		if service.Name == input.ServiceName && scope(service.Namespace) == scope(input.Namespace) && scope(service.Group) == scope(input.Group) {
			serviceIndex = i
		}
	}
	if serviceIndex == -1 {
		return RegistrationResult{}, bad("not_found")
	}
	for i := range s.data.Services {
		for j := range s.data.Services[i].Instances {
			existing := &s.data.Services[i].Instances[j]
			if existing.Source == "sdk" && existing.LeaseTokenHash == hash {
				if i != serviceIndex || existing.Address != input.Address || existing.Port != input.Port {
					return RegistrationResult{}, bad("registration_conflict")
				}
				candidate.Lifecycle = existing.Lifecycle
				if instanceJSON(candidate) != instanceJSON(*existing) {
					next := clone(s.data)
					updated := &next.Services[i].Instances[j]
					updated.Version = candidate.Version
					updated.Weight = candidate.Weight
					updated.Metadata = candidate.Metadata
					next.Version++
					if err := s.commit(next); err != nil {
						return RegistrationResult{}, err
					}
					s.publish(next)
					existing = &s.data.Services[i].Instances[j]
				}
				if err := s.renewLease(existing, expires, existing.LeaseExpiresAt <= now.Unix()); err != nil {
					return RegistrationResult{}, err
				}
				s.beats[existing.ID] = now
				result.InstanceID = existing.ID
				return result, nil
			}
			if existing.Address == input.Address && existing.Port == input.Port {
				return RegistrationResult{}, bad("port_in_use")
			}
		}
	}
	var retired int
	if err := s.db.QueryRow("SELECT count(*) FROM retired_registrations WHERE token_hash=?", hash).Scan(&retired); err != nil {
		return RegistrationResult{}, err
	}
	if retired > 0 {
		return RegistrationResult{}, bad("registration_retired")
	}
	next := clone(s.data)
	instance := Instance{
		ID: newID(), Address: input.Address, Port: input.Port, Enabled: true,
		Health: HealthUnknown, Source: "sdk", LeaseTokenHash: hash, LeaseExpiresAt: expires,
		ProbeGeneration: 1, Version: candidate.Version, Weight: candidate.Weight, Metadata: candidate.Metadata, Lifecycle: "active",
	}
	next.Services[serviceIndex].Instances = append(next.Services[serviceIndex].Instances, instance)
	next.Version++
	result.InstanceID = instance.ID
	if err := s.commit(next); err != nil {
		return RegistrationResult{}, err
	}
	s.publish(next)
	s.beats[instance.ID] = now
	return result, nil
}

func (s *Store) RenewRegistration(id, token string) (RegistrationResult, error) {
	hash, err := registrationHash(token)
	if err != nil {
		return RegistrationResult{}, err
	}
	now := time.Now()
	expires := now.Add(LeaseTTL).Unix()
	result := RegistrationResult{InstanceID: id, LeaseTTLSeconds: int(LeaseTTL.Seconds()), ExpiresAt: expires}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Services {
		for j := range s.data.Services[i].Instances {
			instance := &s.data.Services[i].Instances[j]
			if instance.ID != id {
				continue
			}
			if instance.Source != "sdk" || instance.LeaseTokenHash != hash {
				return RegistrationResult{}, bad("registration_forbidden")
			}
			if instance.LeaseExpiresAt <= now.Unix() {
				return RegistrationResult{}, bad("registration_expired")
			}
			if err := s.renewLease(instance, expires, false); err != nil {
				return RegistrationResult{}, err
			}
			s.beats[instance.ID] = now
			return result, nil
		}
	}
	return RegistrationResult{}, bad("not_found")
}

// renewLease changes a single row without cloning or republishing the registry.
// The caller holds s.mu. Expired leases must pass a new health check.
func (s *Store) renewLease(instance *Instance, expires int64, expired bool) error {
	if !expired && instance.LeaseExpiresAt == expires {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE instances SET lease_expires_at = ? WHERE id = ?", expires, instance.ID); err != nil {
		return err
	}
	if expired {
		if _, err := tx.Exec("UPDATE metadata SET value = ? WHERE key = 'version'", s.data.Version+1); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	instance.LeaseExpiresAt = expires
	if expired {
		instance.LeaseExpired = false
		instance.Health = HealthUnknown
		instance.ConsecutiveFailures = 0
		instance.ConsecutiveSuccesses = 0
		instance.LastHealthyAt = 0
		instance.LastCheckAt = 0
		instance.LastError = ""
		instance.ProbeGeneration++
		s.data.Version++
		if s.changed != nil {
			close(s.changed)
			s.changed = make(chan struct{})
		}
	}
	return nil
}

func (s *Store) DeregisterInstance(id, token string) error {
	hash, err := registrationHash(token)
	if err != nil {
		return err
	}
	return s.update(func(d *Snapshot) error {
		for i := range d.Services {
			for j, instance := range d.Services[i].Instances {
				if instance.ID != id {
					continue
				}
				if instance.Source != "sdk" || instance.LeaseTokenHash != hash {
					return bad("registration_forbidden")
				}
				d.Services[i].Instances = append(d.Services[i].Instances[:j], d.Services[i].Instances[j+1:]...)
				return nil
			}
		}
		return bad("not_found")
	}, true)
}

// ExpireLeases removes registrations that have not been renewed.
func (s *Store) ExpireLeases(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data)
	changed := false
	for i := range next.Services {
		service := &next.Services[i]
		status := s.protectionStatus(*service, now)
		if status != service.ProtectionStatus {
			service.ProtectionStatus = status
			changed = true
		}

		kept := service.Instances[:0]
		removed := 0
		limit := cleanupLimit(*service)
		for _, instance := range service.Instances {
			if instance.Source == "sdk" && instance.LeaseExpiresAt <= now.Unix() {
				if status.Active && instance.Lifecycle != "draining" {
					if !instance.LeaseExpired {
						instance.LeaseExpired = true
						changed = true
					}
				} else if removed < limit {
					removed++
					changed = true
					continue
				}
				if !instance.LeaseExpired {
					instance.LeaseExpired = true
					changed = true
				}
			}
			kept = append(kept, instance)
		}
		service.Instances = kept
	}
	if !changed {
		return nil
	}
	next.Version++
	if err := s.commit(next); err != nil {
		return err
	}
	s.publish(next)
	for _, service := range next.Services {
		previous := s.protections[service.ID]
		if previous.Active != service.ProtectionStatus.Active {
			log.Printf("self protection service=%s namespace=%s group=%s active=%t reason=%s renewalRatio=%.2f", service.Name, service.Namespace, service.Group, service.ProtectionStatus.Active, service.ProtectionStatus.Reason, service.ProtectionStatus.RenewalRatio)
		}
		s.protections[service.ID] = service.ProtectionStatus
	}
	return nil
}
