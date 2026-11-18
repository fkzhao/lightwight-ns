package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"
	"time"
)

const LeaseTTL = 30 * time.Second

type RegistrationInput struct {
	ServiceName string `json:"serviceName"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	Token       string `json:"token"`
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
	now := time.Now()
	expires := now.Add(LeaseTTL).Unix()
	result := RegistrationResult{LeaseTTLSeconds: int(LeaseTTL.Seconds()), ExpiresAt: expires}
	s.mu.Lock()
	defer s.mu.Unlock()
	serviceIndex := -1
	for i, service := range s.data.Services {
		if service.Name == input.ServiceName {
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
				if err := s.renewLease(existing, expires, existing.LeaseExpiresAt <= now.Unix()); err != nil {
					return RegistrationResult{}, err
				}
				result.InstanceID = existing.ID
				return result, nil
			}
			if existing.Address == input.Address && existing.Port == input.Port {
				return RegistrationResult{}, bad("port_in_use")
			}
		}
	}
	next := clone(s.data)
	instance := Instance{
		ID: newID(), Address: input.Address, Port: input.Port, Enabled: true,
		Health: HealthUnknown, Source: "sdk", LeaseTokenHash: hash, LeaseExpiresAt: expires,
		ProbeGeneration: 1,
	}
	next.Services[serviceIndex].Instances = append(next.Services[serviceIndex].Instances, instance)
	next.Version++
	result.InstanceID = instance.ID
	if err := s.commit(next); err != nil {
		return RegistrationResult{}, err
	}
	s.data = next
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
		instance.Health = HealthUnknown
		instance.ProbeGeneration++
		s.data.Version++
	}
	return nil
}

func (s *Store) DeregisterInstance(id, token string) error {
	hash, err := registrationHash(token)
	if err != nil {
		return err
	}
	return s.Update(func(d *Snapshot) error {
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
	})
}

// ExpireLeases removes registrations that have not been renewed.
func (s *Store) ExpireLeases(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data)
	changed := false
	for i := range next.Services {
		kept := next.Services[i].Instances[:0]
		for _, instance := range next.Services[i].Instances {
			if instance.Source == "sdk" && instance.LeaseExpiresAt <= now.Unix() {
				changed = true
				continue
			}
			kept = append(kept, instance)
		}
		next.Services[i].Instances = kept
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
