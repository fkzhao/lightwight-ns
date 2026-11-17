package agent

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"sync"
	"time"

	"github.com/fkzhao/lightwight-ns/core/internal/registry"
)

type State struct {
	mu       sync.RWMutex
	snapshot registry.Snapshot
	ready    bool
	lastSync time.Time
}

func (s *State) Version() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot.Version
}

func (s *State) Apply(snapshot registry.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = snapshot
	s.ready = true
}

func (s *State) MarkSynced() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSync = time.Now().UTC()
}

type Status struct {
	Ready           bool   `json:"ready"`
	RegistryVersion uint64 `json:"registryVersion"`
	LastSyncAt      string `json:"lastSyncAt"`
}

func (s *State) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := Status{Ready: s.ready, RegistryVersion: s.snapshot.Version}
	if !s.lastSync.IsZero() {
		status.LastSyncAt = s.lastSync.Format(time.RFC3339Nano)
	}
	return status
}

// Resolve returns healthy machine addresses and ports for one service.
func (s *State) Resolve(serviceName string) (registry.DiscoverReply, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return registry.DiscoverReply{}, errors.New("registry_unavailable")
	}
	reply := registry.DiscoverReply{Version: s.snapshot.Version, Endpoints: []registry.Endpoint{}}
	for _, service := range s.snapshot.Services {
		if service.Name != serviceName {
			continue
		}
		for _, instance := range service.Instances {
			if instance.Enabled && instance.Health == registry.HealthHealthy {
				reply.Endpoints = append(reply.Endpoints, registry.Endpoint{Address: instance.Address, Port: instance.Port})
			}
		}
		return reply, nil
	}
	return registry.DiscoverReply{}, registry.DomainError{Code: "not_found"}
}

// SyncOnce fetches a changed snapshot and retains the last good cache on failure.
func SyncOnce(ctx context.Context, state *State, centerAddress string) error {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", centerAddress)
	if err != nil {
		return err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	client := rpc.NewClient(connection)
	defer client.Close()
	var reply registry.SnapshotReply
	if err := client.Call("Registry.GetSnapshot", registry.SnapshotRequest{KnownVersion: state.Version()}, &reply); err != nil {
		return err
	}
	if reply.Changed {
		state.Apply(reply.Snapshot)
	}
	state.MarkSynced()
	return nil
}
