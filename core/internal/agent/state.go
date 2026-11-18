package agent

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"strings"
	"sync"
	"time"

	"github.com/fkzhao/lightwight-ns/core/internal/registry"
)

type State struct {
	mu       sync.RWMutex
	snapshot registry.Snapshot
	ready    bool
	lastSync time.Time
	MaxStale time.Duration
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
	s.lastSync = time.Now().UTC()
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
	Stale           bool   `json:"stale"`
}

func (s *State) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := Status{Ready: s.ready, RegistryVersion: s.snapshot.Version, Stale: s.isStale()}
	if !s.lastSync.IsZero() {
		status.LastSyncAt = s.lastSync.Format(time.RFC3339Nano)
	}
	return status
}

// Resolve returns healthy machine addresses and ports for one service.
func (s *State) Resolve(serviceName string) (registry.DiscoverReply, error) {
	result, err := s.ResolveV2(registry.ResolveQuery{Service: serviceName})
	reply := registry.DiscoverReply{Version: result.RegistryVersion, Endpoints: []registry.Endpoint{}}
	for _, endpoint := range result.Endpoints {
		reply.Endpoints = append(reply.Endpoints, registry.Endpoint{Address: endpoint.Address, Port: endpoint.Port})
	}
	return reply, err
}

func (s *State) isStale() bool {
	limit := s.MaxStale
	if limit <= 0 {
		limit = 30 * time.Second
	}
	return s.lastSync.IsZero() || time.Since(s.lastSync) > limit
}

func (s *State) ResolveV2(query registry.ResolveQuery) (registry.ResolveResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return registry.ResolveResult{}, errors.New("registry_unavailable")
	}
	if s.isStale() {
		return registry.ResolveResult{}, registry.DomainError{Code: "stale_snapshot"}
	}
	return registry.Resolve(s.snapshot, query, time.Now())
}

// SyncOnce fetches a changed snapshot and retains the last good cache on failure.
func SyncOnce(ctx context.Context, state *State, centerAddress string) error {
	return syncSnapshot(ctx, state, centerAddress, false)
}

func WatchOnce(ctx context.Context, state *State, centerAddress string) error {
	return syncSnapshot(ctx, state, centerAddress, true)
}

func syncSnapshot(ctx context.Context, state *State, centerAddress string, watch bool) error {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", centerAddress)
	if err != nil {
		return err
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { connection.Close() })
	defer stop()
	if err := connection.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}
	client := rpc.NewClient(connection)
	defer client.Close()
	var reply registry.SnapshotReply
	method := "Registry.GetSnapshot"
	if watch {
		method = "Registry.WatchSnapshot"
	}
	if err := client.Call(method, registry.SnapshotRequest{KnownVersion: state.Version()}, &reply); err != nil {
		if !watch || !strings.Contains(err.Error(), "can't find method") {
			return err
		}
		if err := client.Call("Registry.GetSnapshot", registry.SnapshotRequest{KnownVersion: state.Version()}, &reply); err != nil {
			return err
		}
		defer func() {
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
		}()
	}
	if reply.Changed {
		state.Apply(reply.Snapshot)
	}
	state.MarkSynced()
	return nil
}
