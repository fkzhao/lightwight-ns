package registry

import (
	"math"
	"time"
)

type ProtectionPolicy struct {
	Enabled               bool    `json:"enabled"`
	MinInstances          int     `json:"minInstances"`
	RenewalThreshold      float64 `json:"renewalThreshold"`
	ProbeFailureThreshold float64 `json:"probeFailureThreshold"`
	WindowSeconds         int     `json:"windowSeconds"`
	WarmupSeconds         int     `json:"warmupSeconds"`
	MaxHoldSeconds        int     `json:"maxHoldSeconds"`
	RecoverySeconds       int     `json:"recoverySeconds"`
	AllowStale            bool    `json:"allowStale"`
	MaxStaleSeconds       int     `json:"maxStaleSeconds"`
}

type ProtectionStatus struct {
	Active        bool    `json:"active"`
	Reason        string  `json:"reason,omitempty"`
	Since         int64   `json:"since,omitempty"`
	RenewalRatio  float64 `json:"renewalRatio"`
	RecoverySince int64   `json:"recoverySince,omitempty"`
	CooldownUntil int64   `json:"cooldownUntil,omitempty"`
}

func normalizeProtection(policy ProtectionPolicy) (ProtectionPolicy, error) {
	if policy.MinInstances == 0 {
		policy.MinInstances = 10
	}
	if policy.RenewalThreshold == 0 {
		policy.RenewalThreshold = .85
	}
	if policy.ProbeFailureThreshold == 0 {
		policy.ProbeFailureThreshold = .5
	}
	if policy.WindowSeconds == 0 {
		policy.WindowSeconds = 20
	}
	if policy.WarmupSeconds == 0 {
		policy.WarmupSeconds = 60
	}
	if policy.MaxHoldSeconds == 0 {
		policy.MaxHoldSeconds = 120
	}
	if policy.RecoverySeconds == 0 {
		policy.RecoverySeconds = 20
	}
	if policy.MaxStaleSeconds == 0 {
		policy.MaxStaleSeconds = 60
	}
	if policy.MinInstances < 2 || policy.MinInstances > 100000 || policy.RenewalThreshold <= 0 || policy.RenewalThreshold > 1 || policy.ProbeFailureThreshold <= 0 || policy.ProbeFailureThreshold > 1 || policy.WindowSeconds < 10 || policy.WindowSeconds >= int(LeaseTTL.Seconds()) || (policy.WarmupSeconds < 0 || policy.WarmupSeconds > 3600) || policy.MaxHoldSeconds < 10 || policy.MaxHoldSeconds > 600 || (policy.RecoverySeconds < 10 || policy.RecoverySeconds > 600) || policy.MaxStaleSeconds < 1 || policy.MaxStaleSeconds > 300 {
		return policy, bad("invalid_protection_policy")
	}
	return policy, nil
}

func (s *Store) protectionStatus(service Service, now time.Time) ProtectionStatus {
	policy := service.Protection
	previous := service.ProtectionStatus
	if !policy.Enabled {
		return ProtectionStatus{}
	}
	total, recent, observed, failed := 0, 0, 0, 0
	for _, instance := range service.Instances {
		if !instance.Enabled || instance.Lifecycle == "draining" {
			continue
		}
		if instance.Source == "sdk" {
			total++
			if beat, ok := s.beats[instance.ID]; ok && now.Sub(beat) <= time.Duration(policy.WindowSeconds)*time.Second {
				recent++
			}
		}
		if instance.LastHealthyAt > 0 && now.UnixMilli()-instance.LastCheckAt < int64(policy.WindowSeconds)*1000 {
			observed++
			if instance.ConsecutiveFailures > 0 {
				failed++
			}
		}
	}
	ratio := 1.0
	if total > 0 {
		ratio = float64(recent) / float64(total)
	}
	previous.RenewalRatio = ratio
	anomaly := total >= policy.MinInstances && ratio < policy.RenewalThreshold || observed >= policy.MinInstances && float64(failed)/float64(observed) >= policy.ProbeFailureThreshold
	if previous.Active {
		if !anomaly {
			if previous.RecoverySince == 0 {
				previous.RecoverySince = now.Unix()
			}
		} else {
			previous.RecoverySince = 0
		}
		recovered := previous.RecoverySince > 0 && now.Unix()-previous.RecoverySince >= int64(policy.RecoverySeconds)
		if recovered || now.Unix()-previous.Since >= int64(policy.MaxHoldSeconds) {
			previous.Active = false
			previous.CooldownUntil = now.Add(time.Minute).Unix()
			previous.Reason = "recovery_cleanup"
		}
	} else if anomaly && now.Sub(s.startedAt) >= time.Duration(policy.WarmupSeconds)*time.Second && now.Unix() >= previous.CooldownUntil {
		previous.Active = true
		previous.Since = now.Unix()
		previous.RecoverySince = 0
		previous.Reason = "renewal_drop"
		if observed >= policy.MinInstances && float64(failed)/float64(observed) >= policy.ProbeFailureThreshold {
			previous.Reason = "probe_failure_surge"
		}
	}
	return previous
}

func cleanupLimit(service Service) int {
	if service.Protection.Enabled {
		return max(1, int(math.Ceil(float64(len(service.Instances))*.1)))
	}
	return len(service.Instances)
}
