# lightwight-ns

A small Go service registry with a web administration interface and a machine-local discovery agent. Administrators register each microservice and add its machine IP addresses and ports beneath it. The registry center checks endpoint health; a local agent caches the registry and answers discovery requests by service name over a Unix socket or TCP RPC.

## Modules

| Directory | Responsibility |
| --- | --- |
| `registry-center/` | Starts the registry center: management HTTP API, center RPC server, health checker, and built web UI. |
| `internal/registry/` | Defines services and nested instances, validates changes, persists versioned JSON snapshots, and implements center RPC methods. |
| `internal/health/` | Probes enabled instances using each service's TCP or HTTP health-check policy and updates their health state. |
| `local-agent/` | Starts a machine-local process that syncs snapshots from the center and listens on Unix and TCP sockets for discovery RPC. |
| `internal/agent/` | Holds the agent's cached snapshot and resolves service names to enabled, healthy endpoints. |
| `client/` | Provides the reusable Go library that microservices use to query their local agent. |
| `discover/` | Provides a command-line lookup tool for operators and manual tests. |
| `web/` | Contains the React management UI and its Vite build configuration. |
| `web/src/App.tsx` | Assembles the service page, instance list, status badges, and page-level actions. |
| `web/src/features/registry/` | Holds service and instance forms plus the registry snapshot provider and refresh logic. |
| `web/src/lib/api.ts` and `web/src/types/registry.ts` | Define the management API client and the frontend registry data types. |
| `web/src/i18n/` | Holds Chinese and English translations and tracks the selected language. |
| `web/src/components/` and `web/src/index.css` | Provide reusable UI components, light and dark themes, and semantic color tokens. |

An administrator changes the registry through the web UI and center HTTP API. The center stores those changes, probes the instances, and exposes versioned snapshots over RPC. Each local agent syncs a snapshot; microservices query that agent through the `client` package and receive the healthy machine and port list for a service name.

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
go run ./registry-center
```

Start an agent on the same machine as the microservices:

```bash
go run ./local-agent
```

The center serves the management API at `http://127.0.0.1:8080` and RPC at `127.0.0.1:9090`. The agent listens on both `/tmp/lightwight-ns/agent.sock` and `127.0.0.1:9091`, and syncs its cache with the center every 5 seconds. Restart an older agent process after upgrading to enable the TCP listener. Configure `NS_CENTER_RPC_ADDR` on each machine to reach the center.

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

For TCP RPC, replace `client.Dial("")` with `client.DialTCP("")`. Both use a reusable Go `net/rpc` connection and call `Agent.Discover` with a `ServiceName` string. The default TCP address is `127.0.0.1:9091`; pass a different address to `DialTCP` when needed. The agent does not expose a local HTTP listener. It retains its last synchronized snapshot if it temporarily cannot reach the center.

For a quick manual lookup through the running agent, use `go run ./discover order-server` for Unix or `go run ./discover -tcp order-server` for TCP. Both print a JSON array of healthy `host:port` endpoints. Set `NS_AGENT_SOCKET` or `NS_AGENT_TCP_ADDR` if the agent uses a custom address.

Run these commands from the repository root. Start `go run ./local-agent` in a separate terminal before using `discover`; a missing Unix socket means the agent is not running. The center must also be running for the agent to receive registry data.

## Manual health-check and discovery test

This test uses a service named `order-server` with TCP health checks and two enabled instances: `127.0.0.1:8081` and `127.0.0.1:8082`. Register those records in the web UI first, then keep the center and local agent running in separate terminals. Run all commands from the repository root.

Before starting mock listeners, check whether either port is already in use:

```bash
lsof -nP -iTCP:8081 -sTCP:LISTEN
lsof -nP -iTCP:8082 -sTCP:LISTEN
```

For the full stop-and-restart sequence, use free ports and mock listeners. If a port already has a real service, choose other registered test ports. Run each of these commands in its own terminal only when the table below asks you to start that listener:

```bash
python3 -c 'import socketserver; socketserver.TCPServer.allow_reuse_address = True; socketserver.TCPServer(("127.0.0.1", 8081), socketserver.BaseRequestHandler).serve_forever()'
```

```bash
python3 -c 'import socketserver; socketserver.TCPServer.allow_reuse_address = True; socketserver.TCPServer(("127.0.0.1", 8082), socketserver.BaseRequestHandler).serve_forever()'
```

After each change, allow about 15 seconds for the center health check, agent sync, and web UI refresh. Run `go run ./discover order-server` and `go run ./discover -tcp order-server` to inspect both agent transports. They should return the same available endpoint set; endpoint order is not part of the contract.

| Step | Action | Expected agent result | Expected web status |
| --- | --- | --- | --- |
| 1 | Leave both ports unbound | `[]` | Both show Unreachable after the first check |
| 2 | Start the 8081 mock listener | `["127.0.0.1:8081"]` | 8081 Available; 8082 Unreachable |
| 3 | Start the 8082 mock listener | `["127.0.0.1:8081", "127.0.0.1:8082"]` | Both Available |
| 4 | Press `Ctrl+C` in the 8081 listener terminal | `["127.0.0.1:8082"]` | 8081 Unreachable; 8082 Available |
| 5 | Restart the 8081 mock listener | `["127.0.0.1:8081", "127.0.0.1:8082"]` | Both Available again |
| 6 | Stop both mock listeners with `Ctrl+C` | `[]` | Both Unreachable |

These mock processes accept TCP connections without serving files. A newly registered instance may show `unknown` until its first check. The `127.0.0.1` addresses refer to the center's machine when it probes endpoints; if the center runs elsewhere, register an IP address reachable from that machine and start the listeners there.

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
| `NS_AGENT_TCP_ADDR` | `127.0.0.1:9091` | Agent TCP RPC listen address; also used by `discover -tcp` |
| `NS_SYNC_INTERVAL` | `5s` | Agent snapshot sync interval |

This version has a single center backed by one JSON file. Center management HTTP, center RPC, and agent TCP RPC do not provide authentication. The agent TCP listener binds to loopback by default; keep it on a trusted network if you change its listen address. During a center outage, agents keep their last synchronized snapshot, so health changes are reflected after connectivity returns.
