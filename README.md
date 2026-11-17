# lightwight-ns

A small Go service registry with a web administration interface and a machine-local discovery agent. Administrators register each microservice and add its machine IP addresses and ports beneath it. The registry center checks endpoint health; a local agent caches the registry and answers discovery requests by service name over a Unix socket.

## Model and availability

- **Service:** A unique name, optional description, and one health-check policy for its instances.
- **Instance:** One machine IP address and port beneath a service. The same IP and port cannot be assigned twice. An instance can be disabled manually.
- **Health:** TCP checks whether the port accepts a connection. HTTP sends `GET` to a configured path and requires a 2xx response. Checks run every 5 seconds by default, with a 2-second timeout. New or changed instances remain unavailable until a check succeeds. A failed check removes an instance from discovery; a later successful check restores it.
- **Discovery:** The center and the local agent return only enabled, healthy instances. A registered service with no healthy instances returns an empty list.
- **Persistence:** Registry data is stored in a JSON snapshot. Every change advances its version. On restart, saved health results reset to `unknown` until checked again. Existing JSON files with separate `machines` and `instances` collections are migrated to instances nested under their services.

## Run locally

Requires Go 1.22+ and Node.js 22.13+.

Start the center:

```bash
go run ./cmd/center
```

Start an agent on the same machine as the microservices:

```bash
go run ./cmd/agent
```

The center serves the management API at `http://127.0.0.1:8080` and RPC at `127.0.0.1:9090`. The agent listens on `/tmp/lightwight-ns/agent.sock` and syncs its cache with the center every 5 seconds. Configure `NS_CENTER_RPC_ADDR` on each machine to reach the center.

For frontend development, run:

```bash
cd web
npm ci
npm run dev
```

Vite proxies `/api` to the center. To serve the UI from Go instead, run `npm run build` inside `web`, then start the center from the repository root and open `http://127.0.0.1:8080`.

## Discover a service from Go

Use the reusable client connection in each microservice process:

```go
package main

import (
    "fmt"
    "log"

    "github.com/fkzhao/lightwight-ns/client"
)

func main() {
    discovery, err := client.Dial("") // Default Unix socket path.
    if err != nil {
        log.Fatal(err)
    }
    defer discovery.Close()

    endpoints, err := discovery.Discover("order-service")
    if err != nil {
        log.Fatal(err)
    }
    for _, endpoint := range endpoints {
        fmt.Printf("%s:%d\n", endpoint.Address, endpoint.Port)
    }
}
```

The local wire protocol is Go `net/rpc` over a Unix socket: `Agent.Discover` accepts a `ServiceName` string and returns an endpoint list. There is no local HTTP listener. The agent retains its last synchronized snapshot if it temporarily cannot reach the center.

For a quick manual lookup through the running agent, use `go run ./cmd/discover order-server`. It prints a JSON array of healthy `host:port` endpoints. Set `NS_AGENT_SOCKET` if the agent uses a custom socket path.

## Management API

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/snapshot` | Read services with nested instances and health states |
| POST | `/api/services` | Register a service |
| PUT, DELETE | `/api/services/{serviceID}` | Update or delete a service and its instances |
| POST | `/api/services/{serviceID}/instances` | Add a machine IP and port under a service |
| PUT, DELETE | `/api/services/{serviceID}/instances/{instanceID}` | Update or remove an instance |

Service input includes `name`, `description`, and `healthCheck`, such as `{"type":"http","path":"/health"}` or `{"type":"tcp"}`. Instance input includes `address`, `port`, and `enabled`.

The center's TCP `net/rpc` service is named `Registry`. `Registry.GetSnapshot` provides versioned snapshots to agents. `Registry.Discover` also resolves a service name to enabled, healthy endpoints.

## Configuration

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `NS_DATA_FILE` | `data/registry.json` | Center snapshot file |
| `NS_HTTP_ADDR` | `127.0.0.1:8080` | Web interface and management API address |
| `NS_RPC_ADDR` | `127.0.0.1:9090` | Center RPC address for agents |
| `NS_HEALTH_INTERVAL` | `5s` | Center health-check interval |
| `NS_WEB_DIST` | `web/dist` | Built frontend directory |
| `NS_CENTER_RPC_ADDR` | `127.0.0.1:9090` | Center RPC address used by an agent |
| `NS_AGENT_SOCKET` | `/tmp/lightwight-ns/agent.sock` | Agent Unix socket path |
| `NS_SYNC_INTERVAL` | `5s` | Agent snapshot sync interval |

This version has a single center backed by one JSON file. Center management HTTP and RPC do not provide authentication. Keep them on a trusted network when allowing agents on other machines to connect. During a center outage, agents keep their last synchronized snapshot, so health changes are reflected after connectivity returns.
