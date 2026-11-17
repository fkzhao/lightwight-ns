# lightwight-ns

A small service registry for microservices. The registry center owns service definitions, machine addresses, ports, and health checks. A lightweight Go agent runs on each machine, caches the center's snapshot, and returns healthy endpoints by service name. Go, Python, and Java SDKs can discover through the local agent or register an instance directly with the center.

## Architecture

```text
Service process ── registration SDK ── HTTP/JSON ── Registry center
                                                    │
                                        health checks and JSON snapshot
                                                    │
Service process ── discovery SDK ── local Agent ── Go RPC snapshot sync
                         TCP or Unix socket
```

The center remains the source of truth. The agent's TCP and Unix listeners use newline-delimited JSON-RPC 2.0, so the discovery SDKs do not depend on Go's gob encoding. The center-to-agent sync still uses Go `net/rpc`. See [discovery protocol](protocol/discovery-v1.md) and [registration protocol](protocol/registration-v1.md).

## Modules

| Path | Responsibility |
| --- | --- |
| `core/registry-center/` | Center process: management and registration HTTP API, center RPC listener, health checker, and built Web UI. |
| `core/agent/` | Local agent process: center sync, cache, Unix socket listener, and TCP listener. |
| `core/internal/registry/` | Service and instance model, validation, JSON persistence, leases, and center RPC. |
| `core/internal/health/` | TCP or HTTP health probes and lease expiry. |
| `core/internal/agent/` | Cached snapshot, healthy endpoint filtering, and JSON-RPC protocol. |
| `sdk/go/discovery/` and `sdk/go/registration/` | Go SDKs for agent lookup and center registration. |
| `sdk/python/lightwight_ns/` | Python SDK with discovery and registration clients; Python standard library only. |
| `sdk/java/` | Java 17 SDK with discovery and registration clients; uses Gson for JSON. |
| `tools/discover/` | Go command-line lookup tool for both agent transports. |
| `protocol/` | Versioned wire protocol specifications. |
| `web/` | React management UI with Chinese/English and light/dark modes. |
| `examples/` | Short Go, Python, and Java usage examples. |

Services are created in the Web UI or management API with a name and a TCP or HTTP health policy. Instances belong to one service. They can be added manually through the UI, or registered by a service process using a registration SDK. A manual instance remains until an administrator removes it. An SDK instance has a 30-second lease and is removed if renewal stops; it is also deregistered on graceful shutdown. Only enabled instances that pass the center's health check are returned by discovery. A known service with no healthy instances returns an empty list.

## Run locally

Requires Go 1.22 or newer. The Web development build also requires Node.js 22.13 or newer. Python SDK requires Python 3.9 or newer; Java SDK requires Java 17 and Maven.

From the repository root, start the center and agent in separate terminals:

```bash
go run ./core/registry-center
go run ./core/agent
```

The center listens on `127.0.0.1:8080` for HTTP and `127.0.0.1:9090` for agent snapshot sync. The agent listens on `/tmp/lightwight-ns/agent.sock` and `127.0.0.1:9091`, and syncs every 5 seconds. Set `NS_CENTER_RPC_ADDR` on remote machines to the center's reachable RPC address.

When upgrading an existing installation, restart both center and agent. The former `registry-center/`, `local-agent/`, `client/`, and `discover/` paths moved to `core/registry-center/`, `core/agent/`, `sdk/go/discovery/`, and `tools/discover/`. Go callers should replace the old `Dial`/`Discover` API with `DialUnix` or `DialTCP` and `Resolve(ctx, serviceName)`.

For Web development:

```bash
cd web
npm ci
npm run dev
```

Vite proxies `/api` to the center. To serve the Web UI directly from the center, run `npm run build` in `web/`, then start the center from the repository root.

## SDK usage

### Go

Discovery queries the local agent:

```go
agent, err := discovery.DialTCP("") // Or discovery.DialUnix("").
if err != nil { log.Fatal(err) }
defer agent.Close()
endpoints, err := agent.Resolve(context.Background(), "order-service")
if err != nil { log.Fatal(err) }
for _, endpoint := range endpoints {
    fmt.Printf("%s:%d\n", endpoint.Address, endpoint.Port)
}
```

Import `github.com/fkzhao/lightwight-ns/sdk/go/discovery`. Registration talks to the center:

```go
center := registration.NewClient("")
lease, err := center.Register(context.Background(), "order-service", "127.0.0.1", 8081)
if err != nil { log.Fatal(err) }
defer lease.Close(context.Background())
```

Import `github.com/fkzhao/lightwight-ns/sdk/go/registration`. Keep the lease alive for the service process lifetime. `LastRenewalError()` reports the latest background renewal failure.

### Python

Set `PYTHONPATH=sdk/python` when running from this repository, or install with `python3 -m pip install ./sdk/python`.

```python
from lightwight_ns import DiscoveryClient, RegistrationClient

with RegistrationClient().register("order-service", "127.0.0.1", 8081) as lease:
    with DiscoveryClient() as agent:  # TCP; pass unix_socket="/tmp/lightwight-ns/agent.sock" for Unix.
        print(agent.resolve("order-service"))
```

Keep the registration context open for the process lifetime. `lease.instance_id` and `lease.last_renewal_error` are available for monitoring. The example at `examples/python/example.py` accepts command-line arguments.

Discovery reflects the center's latest health check and the agent's latest sync, so a newly registered instance may take about 10 seconds to appear with the default intervals.

### Java

Build the SDK with `mvn -f sdk/java/pom.xml package`. It uses Java 17 and Gson. Add `io.github.fkzhao:lightwight-ns-sdk:0.1.0` to a Maven project after installing locally with `mvn -f sdk/java/pom.xml install`.

```java
try (var agent = new DiscoveryClient()) {
    var endpoints = agent.resolve("order-service");
}
var center = new RegistrationClient();
try (var lease = center.register("order-service", "127.0.0.1", 8081)) {
    // Run the service while the lease renews automatically.
}
```

Import classes from `io.github.fkzhao.lightwightns`. Java discovery uses the agent's TCP listener. See `examples/java/Example.java`. A registration attempt requires an existing service definition; it does not create or change the service health policy.

To run the standalone Java example from this checkout, first build the SDK and compile with its runtime dependencies:

```bash
mvn -q -f sdk/java/pom.xml package dependency:build-classpath -Dmdep.outputFile=target/classpath.txt
javac -cp "sdk/java/target/classes:$(cat sdk/java/target/classpath.txt)" -d sdk/java/target examples/java/Example.java
java -cp "sdk/java/target:sdk/java/target/classes:$(cat sdk/java/target/classpath.txt)" Example discover order-service
```

## Test the full discovery path

This test uses a service called `order-service` with TCP health checks and a mock TCP listener on `127.0.0.1:8081`. Start center and agent as shown above. In the Web UI at `http://127.0.0.1:8080`, create the service with TCP health checks, then manually add `127.0.0.1:8081` beneath it. Check that 8081 is free first.

1. Before starting the listener, wait one health-check cycle (5 seconds). The Web UI should show **Unreachable** and both `go run ./tools/discover order-service` and `go run ./tools/discover -tcp order-service` should print `[]`.
2. In another terminal, start a mock listener: `python3 -m http.server 8081 --bind 127.0.0.1`. After a center check and agent sync (up to about 10 seconds), the UI should show **Available** and both lookup commands should print `["127.0.0.1:8081"]`.
3. Stop the mock listener with Ctrl+C. After another check and sync, both lookup commands should return `[]` again.
4. Restart the mock listener. It should reappear in discovery. This confirms health recovery as well as removal.

The center probes from its own machine. If center and service run on different hosts, register an IP address the center can reach. For an SDK-managed instance, start the mock listener first, then register that instance using an SDK. It should appear in the UI with an **SDK** badge, become available after probing, and disappear after the lease closes or expires.

The automated checks are:

```bash
go test ./core/... ./sdk/go/... ./tools/...
PYTHONPATH=sdk/python python3 -m unittest discover -s sdk/python/tests -v
mvn -q -f sdk/java/pom.xml test
npm run build --prefix web
python3 tests/e2e.py
```

## Center APIs

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/health` | Center liveness |
| GET | `/api/snapshot` | Services, nested instances, and health |
| POST | `/api/services` | Create a service definition |
| PUT, DELETE | `/api/services/{serviceID}` | Update or delete a service |
| POST | `/api/services/{serviceID}/instances` | Add a manual instance |
| PUT, DELETE | `/api/services/{serviceID}/instances/{instanceID}` | Change or remove a manual instance |
| POST | `/api/registrations` | Register or renew an SDK instance idempotently |
| POST | `/api/registrations/{instanceID}/renew` | Renew a current SDK lease explicitly |
| DELETE | `/api/registrations/{instanceID}` | Deregister an SDK instance |

The service input includes `name`, `description`, and `healthCheck`, such as `{"type":"tcp"}` or `{"type":"http","path":"/health"}`. A manual instance input includes `address`, `port`, and `enabled`. The registration API is specified in `protocol/registration-v1.md`.

## Configuration

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `NS_DATA_FILE` | `data/registry.json` | Center JSON snapshot file |
| `NS_HTTP_ADDR` | `127.0.0.1:8080` | Center HTTP address |
| `NS_RPC_ADDR` | `127.0.0.1:9090` | Center-to-agent sync address |
| `NS_HEALTH_INTERVAL` | `5s` | Health-check and lease-expiry interval |
| `NS_WEB_DIST` | `web/dist` | Built frontend directory |
| `NS_CENTER_RPC_ADDR` | `127.0.0.1:9090` | Center address used by an agent |
| `NS_AGENT_SOCKET` | `/tmp/lightwight-ns/agent.sock` | Local agent Unix socket |
| `NS_AGENT_TCP_ADDR` | `127.0.0.1:9091` | Local agent TCP address |
| `NS_SYNC_INTERVAL` | `5s` | Agent sync interval |

The center uses a single JSON file and does not yet replicate data across nodes. Center HTTP and RPC and agent TCP have no built-in authentication or TLS. Keep them on trusted networks; the agent TCP listener binds to loopback by default. The agent keeps its last synchronized snapshot during a center outage, so health changes become visible after connectivity returns.
