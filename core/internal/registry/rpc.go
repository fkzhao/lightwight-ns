package registry

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
	return nil
}

func (r *RPCServer) Discover(req DiscoverRequest, reply *DiscoverReply) error {
	snapshot := r.Store.Snapshot()
	reply.Version = snapshot.Version
	reply.Endpoints = []Endpoint{}
	for _, service := range snapshot.Services {
		if service.Name == req.ServiceName {
			for _, instance := range service.Instances {
				if instance.Enabled && instance.Health == HealthHealthy {
					reply.Endpoints = append(reply.Endpoints, Endpoint{Address: instance.Address, Port: instance.Port})
				}
			}
			return nil
		}
	}
	return DomainError{Code: "not_found"}
}
