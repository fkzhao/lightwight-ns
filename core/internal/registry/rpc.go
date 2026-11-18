package registry

import (
	"context"
	"time"
)

type SnapshotRequest struct{ KnownVersion uint64 }

type SnapshotReply struct {
	Changed  bool
	Snapshot Snapshot
}

type DiscoverRequest struct{ ServiceName string }

type Endpoint struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

type DiscoverReply struct {
	Version   uint64
	Endpoints []Endpoint
}

type RPCServer struct{ Store *Store }

func (r *RPCServer) GetSnapshot(req SnapshotRequest, reply *SnapshotReply) error {
	reply.Snapshot, reply.Changed = r.Store.SnapshotSince(req.KnownVersion)
	// Legacy Agents do not understand scopes, so never send them non-default services.
	if reply.Changed {
		services := reply.Snapshot.Services[:0]
		for _, service := range reply.Snapshot.Services {
			if scope(service.Namespace) == DefaultScope && scope(service.Group) == DefaultScope {
				services = append(services, service)
			}
		}
		reply.Snapshot.Services = services
	}
	return nil
}

func (r *RPCServer) Discover(req DiscoverRequest, reply *DiscoverReply) error {
	result, err := Resolve(r.Store.Snapshot(), ResolveQuery{Service: req.ServiceName}, time.Now())
	reply.Version = result.RegistryVersion
	reply.Endpoints = []Endpoint{}
	for _, endpoint := range result.Endpoints {
		reply.Endpoints = append(reply.Endpoints, Endpoint{Address: endpoint.Address, Port: endpoint.Port})
	}
	return err
}

// WatchSnapshot returns a fresh complete snapshot or a heartbeat after 25 seconds.
func (r *RPCServer) WatchSnapshot(req SnapshotRequest, reply *SnapshotReply) error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	reply.Snapshot, reply.Changed = r.Store.Watch(ctx, req.KnownVersion)
	return nil
}
