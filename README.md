# lightwight-ns

A small service registry for microservices. The registry center owns service definitions, machine addresses, ports, and health checks. A lightweight Go agent runs on each machine, caches the center's snapshot, and returns healthy endpoints by service name. Go, Python, and Java SDKs can discover through the local agent or register an instance directly with the center.

## Architecture

```text
Service process ── registration SDK ── HTTP/JSON ── Registry center
                                                    │
                                        health checks and SQLite WAL
                                                    │
Service process ── discovery SDK ── local Agent ── Go RPC snapshot sync
                         TCP or Unix socket
```

The center remains the source of truth. The agent's TCP and Unix listeners use newline-delimited JSON-RPC 2.0, so the discovery SDKs do not depend on Go's gob encoding. The center-to-agent sync still uses Go `net/rpc`. See [discovery protocol](protocol/discovery-v1.md), [registration protocol](protocol/registration-v1.md), and [v2 lifecycle and filtering](protocol/discovery-v2.md).

## Modules

| Path | Responsibility |
| --- | --- |
| `core/registry-center/` | Center process: management and registration HTTP API, center RPC listener, health checker, and built Web UI. |
| `core/agent/` | Local agent process: center sync, cache, Unix socket listener, and TCP listener. |
| `core/internal/registry/` | Service and instance model, validation, SQLite WAL persistence, memory cache, leases, JSON migration, and center RPC. |
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

Requires Go 1.25 or newer. SQLite is embedded through a Go driver, so no database server or C toolchain is required. The Web development build also requires Node.js 22.13 or newer. Python SDK requires Python 3.9 or newer; Java SDK requires Java 17 and Maven.

From the repository root, start the center and agent in separate terminals:

```bash
go run ./core/registry-center
go run ./core/agent
```

The center listens on `127.0.0.1:8080` for HTTP and `127.0.0.1:9090` for agent snapshot sync. The agent listens on `/tmp/lightwight-ns/agent.sock` and `127.0.0.1:9091`, and receives changes using center long polling. Set `NS_CENTER_RPC_ADDR` on remote machines to the center's reachable RPC address.

When upgrading an existing installation, restart both center and agent. The former `registry-center/`, `local-agent/`, `client/`, and `discover/` paths moved to `core/registry-center/`, `core/agent/`, `sdk/go/discovery/`, and `tools/discover/`. Go callers should replace the old `Dial`/`Discover` API with `DialUnix` or `DialTCP` and `Resolve(ctx, serviceName)`.

For Web development:

```bash
cd web
npm ci
npm run dev
```

Vite proxies `/api` to the center. To serve the Web UI directly from the center, run `npm run build` in `web/`, then start the center from the repository root.

## Storage and health scheduling

The center stores services, instances, and registration leases in `data/registry.db`, with SQLite WAL enabled and `synchronous=FULL`. A lease renewal updates only its instance row and does not change the discovery revision. Discovery and snapshot reads use the in-memory registry. Health results stay in memory; a restart resets them to Checking until a fresh probe succeeds. Changes are published to readers only after their SQL transaction commits. SQLite schema v2 adds scoped service identities and instance configuration through an atomic migration; existing records keep their IDs and enter the default namespace and group.

On first startup, a new database automatically imports the JSON file with the same directory and base name: `data/registry.json` becomes `data/registry.db`. Both the nested instance format and the older machines/instances format are supported. The JSON file is preserved and never imported again after a successful migration. Invalid JSON stops startup; fix the input and restart to retry. Existing `NS_DATA_FILE=/path/registry.json` configuration still works and selects `/path/registry.db`. Set `NS_DB_FILE` for new deployments.

Health probes use 128 workers by default, with at most one active probe per instance. Each instance is scheduled again after its previous check completes plus its configured interval, with about 10% jitter. Service policies can override the global interval and timeout. Three failures remove an available instance; two successes restore it by default. Results are published in batches of up to 64 or every 200 ms, independently of slow probes. Lease expiry is checked at least once per second under normal load. Configure concurrency and timeout for the number of instances and network latency. For example, 1000 probes that all consume the 2-second timeout need roughly 16 seconds of worker time at concurrency 128, so a 5-second interval is not a guarantee that every instance is checked within 5 seconds during an outage.

Run one center process per database on a local disk. SQLite WAL files are not a shared database for multiple center nodes or network filesystems. For a simple backup, stop the center and copy its data directory; retain any `-wal` file present with the database. Never delete WAL files from a running database. See [SQLite WAL documentation](https://www.sqlite.org/wal.html).

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

New instances require two successful probes by default before becoming available. Existing instances require three consecutive failures before removal. Agent long polling propagates changes after the center publishes them.

### Java

Build the SDK with `mvn -f sdk/java/pom.xml package`. It uses Java 17 and Gson. Add `io.github.fkzhao:lightwight-ns-sdk:0.2.0` to a Maven project after installing locally with `mvn -f sdk/java/pom.xml install`.

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


## Filtering, weighted selection, and graceful shutdown

The Web UI manages service namespace/group, per-service health thresholds, gRPC health with optional TLS, and self-protection policies. Instances include business version, weight, metadata, lifecycle, and recent probe details. `active` instances may be returned; `draining`, disabled, zero-weight, and expired instances are excluded. SDK instances can be disabled or drained through the Web UI without their next renewal reactivating them.

Go adds `RegisterWithOptions(ctx, name, ip, port, registration.Options{Namespace: "prod", Group: "blue", Version: "v2"})`, `ResolveV2(ctx, discovery.Query{Service: "order-service", Namespace: "prod", Group: "blue", Version: "v2"})`, and `discovery.PickWeighted(result.Endpoints)`. The existing methods retain default-scope behavior.

Python accepts registration keyword arguments `namespace`, `group`, `version`, `weight`, and `metadata`. For discovery, use `agent.resolve_v2("order-service", namespace="prod", group="blue", version="v2", metadata={"zone": "a"})`, then `pick_weighted(result.endpoints)` from `lightwight_ns`.

Java adds a `register` overload accepting `RegistrationOptions`, `agent.resolveV2(new ResolveQuery(...))`, and `result.pickWeighted()`. Both languages preserve their previous methods. Filters require a v2 Agent and are never silently ignored by falling back to v1.

For graceful shutdown, call the lease's `Drain` (Go) or `drain` (Python/Java), stop accepting new business requests, wait for existing requests or a deadline, then close the lease. Renewal continues during draining. The center does not close business connections held by callers. Runnable HTTP examples with signal/shutdown hooks are in `examples/go-graceful/main.go`, `examples/python/graceful_service.py`, and `examples/java/GracefulService.java`. Create `order-service` with TCP checks or HTTP `/health` first. Examples use port 8081 and the default namespace/group.

```bash
go run ./examples/go-graceful
# Or, in a separate test run:
PYTHONPATH=sdk/python python3 examples/python/graceful_service.py
# Java, after building the SDK and classpath as above:
javac -cp "sdk/java/target/classes:$(cat sdk/java/target/classpath.txt)" -d sdk/java/target examples/java/GracefulService.java
java -cp "sdk/java/target:sdk/java/target/classes:$(cat sdk/java/target/classpath.txt)" GracefulService
```

Self protection is opt-in per service. It can temporarily retain registration records during a broad heartbeat or probe failure, but strict discovery still removes failed/expired endpoints. Historical fallback requires explicit opt-in from both service and v2 caller, has a time limit, and marks results stale. Explicitly deleted or drained registrations cannot be recreated by delayed requests using their retired token. See the complete policy, bounds, and API examples in [the v2 protocol](protocol/discovery-v2.md).

## Test the full discovery path

This test uses a service called `order-service` with TCP health checks and a mock TCP listener on `127.0.0.1:8081`. Start center and agent as shown above. In the Web UI at `http://127.0.0.1:8080`, create the service with TCP health checks, then manually add `127.0.0.1:8081` beneath it. Check that 8081 is free first.

1. Before starting the listener, allow three failed checks (about 20 seconds). The Web UI should show **Unreachable** and both `go run ./tools/discover order-service` and `go run ./tools/discover -tcp order-service` should print `[]`.
2. In another terminal, start a mock listener: `python3 -m http.server 8081 --bind 127.0.0.1`. After two successful checks and an Agent update (allow about 12 seconds), the UI should show **Available** and both lookup commands should print `["127.0.0.1:8081"]`.
3. Stop the mock listener with Ctrl+C. After three failed checks and an Agent update (allow about 25 seconds), both lookup commands should return `[]` again.
4. Restart the mock listener. It should reappear in discovery. This confirms health recovery as well as removal.

The center probes from its own machine. If center and service run on different hosts, register an IP address the center can reach. For an SDK-managed instance, start the mock listener first, then register that instance using an SDK. It should appear in the UI with an **SDK** badge, become available after probing, and disappear after the lease closes or expires.

The automated checks are:

```bash
go test ./core/... ./sdk/go/... ./tools/...
PYTHONPATH=sdk/python python3 -m unittest discover -s sdk/python/tests -v
mvn -q -f sdk/java/pom.xml test
npm run build --prefix web
python3 tests/e2e.py
python3 tests/e2e.py --java
```

The Go tests include migration/restart recovery, concurrent renewals for 100 services and 1000 instances, and health scheduling across 1000 endpoints. Run the isolated storage benchmark with `go test ./core/internal/registry -run '^$' -bench BenchmarkLeaseRenewal1000Instances -benchtime=2000x -benchmem`. These tests use temporary databases and do not change the running center's data. Benchmark results depend on disk latency and do not establish a production capacity guarantee.

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
| POST | `/api/registrations/{instanceID}/drain` | Stop new discoveries while keeping the lease alive |
| POST | `/api/services/{serviceID}/instances/{instanceID}/drain` | Administratively drain an instance |
| POST | `/api/services/{serviceID}/instances/{instanceID}/enabled` | Enable or disable discovery, including SDK instances |
| DELETE | `/api/registrations/{instanceID}` | Deregister an SDK instance |

The service input includes `name`, `description`, and `healthCheck`, such as `{"type":"tcp"}` or `{"type":"http","path":"/health"}`. A manual instance input includes `address`, `port`, and `enabled`. The registration API is specified in `protocol/registration-v1.md`.

## Configuration

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `NS_DB_FILE` | `data/registry.db` | Center SQLite database; takes precedence over `NS_DATA_FILE` |
| `NS_DATA_FILE` | unset | Legacy path setting; a `.json` suffix selects the sibling `.db` database |
| `NS_HTTP_ADDR` | `127.0.0.1:8080` | Center HTTP address |
| `NS_RPC_ADDR` | `127.0.0.1:9090` | Center-to-agent sync address |
| `NS_HEALTH_INTERVAL` | `5s` | Delay between completed checks of each instance |
| `NS_HEALTH_TIMEOUT` | `2s` | Maximum duration of one TCP or HTTP probe |
| `NS_HEALTH_CONCURRENCY` | `128` | Maximum simultaneous probes, from 1 to 4096 |
| `NS_WEB_DIST` | `web/dist` | Built frontend directory |
| `NS_CENTER_RPC_ADDR` | `127.0.0.1:9090` | Center address used by an agent |
| `NS_AGENT_SOCKET` | `/tmp/lightwight-ns/agent.sock` | Local agent Unix socket |
| `NS_AGENT_TCP_ADDR` | `127.0.0.1:9091` | Local agent TCP address |
| `NS_SYNC_INTERVAL` | `5s` | Agent reconnect delay after a failed sync |
| `NS_AGENT_MAX_STALE` | `30s` | Maximum age of a successful center sync; minimum 1 second |

The center uses a single SQLite database and does not replicate data across nodes. Center HTTP and RPC and agent TCP have no built-in authentication or TLS. Keep them on trusted networks; the agent TCP listener binds to loopback by default. The Agent keeps its last synchronized snapshot during a center outage. After `NS_AGENT_MAX_STALE` (30 seconds by default), discovery returns an explicit expired-cache error until a sync succeeds.
