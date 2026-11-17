package agent

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"sync"
	"time"

	"github.com/fkzhao/lightwight-ns/internal/registry"
)

type State struct {
	mu       sync.RWMutex
	snapshot registry.Snapshot
	ready    bool
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

type LocalRPC struct{ State *State }

// Discover resolves one service name to its currently healthy machine and port list.
func (r *LocalRPC) Discover(req registry.DiscoverRequest, reply *registry.DiscoverReply) error {
	r.State.mu.RLock()
	defer r.State.mu.RUnlock()
	if !r.State.ready {
		return errors.New("registry_unavailable")
	}
	reply.Version = r.State.snapshot.Version
	reply.Endpoints = []registry.Endpoint{}
	for _, service := range r.State.snapshot.Services {
		if service.Name != req.ServiceName {
			continue
		}
		for _, instance := range service.Instances {
			if instance.Enabled && instance.Health == registry.HealthHealthy {
				reply.Endpoints = append(reply.Endpoints, registry.Endpoint{Address: instance.Address, Port: instance.Port})
			}
		}
		return nil
	}
	return registry.DomainError{Code: "not_found"}
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
	return nil
}
