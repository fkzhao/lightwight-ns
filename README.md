# lightwight-ns

An initial Go service registry with a web administration interface. Administrators manually register services, machine IP addresses, and instance ports. A local agent can retrieve versioned registry snapshots through read-only RPC. The agent itself has not been implemented yet.

## Data model

- **Service:** A unique service name and description.
- **Machine:** A unique machine name, IP address, and description.
- **Instance:** A service deployed on a machine at a specific port. A port can be assigned only once per machine. Disabled instances are excluded from RPC discovery results.
- **Registry snapshot:** Each successful change increments the version and persists the snapshot to a JSON file.

## Run locally

Requires Go 1.22+ and Node.js 22.13+.

```bash
go run ./cmd/center
```

In another terminal, start the frontend development server:

```bash
cd web
npm ci
npm run dev
```

Open the URL printed by Vite. During development, Vite proxies `/api` requests to the Go server. To serve the built frontend directly from Go, run `npm run build` in `web`, then start the Go server from the repository root and open `http://127.0.0.1:8080`.

## Interfaces

The management API listens on `127.0.0.1:8080` by default:

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/snapshot` | Read the complete registry snapshot |
| POST | `/api/services`, `/api/machines`, `/api/instances` | Create a record |
| PUT | `/api/{resource}/{id}` | Update a record |
| DELETE | `/api/{resource}/{id}` | Delete a record |

`resource` is one of `services`, `machines`, or `instances`.

The agent-facing `net/rpc` server listens on `127.0.0.1:9090` by default under the service name `Registry`:

- `Registry.GetSnapshot` accepts `registry.SnapshotRequest{KnownVersion}`. It returns `Changed=false` when the version has not changed. Use a fresh response object for each call.
- `Registry.Discover` accepts `registry.DiscoverRequest{ServiceName}` and returns the addresses and ports of enabled instances.

RPC request and response types are defined in `internal/registry/rpc.go` and can be reused by an agent in the same Go module.

## Configuration

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `NS_DATA_FILE` | `data/registry.json` | Registry snapshot file |
| `NS_HTTP_ADDR` | `127.0.0.1:8080` | Web interface and management API address |
| `NS_RPC_ADDR` | `127.0.0.1:9090` | Agent-facing RPC address |
| `NS_WEB_DIST` | `web/dist` | Built frontend directory |

This MVP has one registry center backed by one JSON file. The management API and RPC server do not yet provide authentication. Protect these interfaces with a controlled network or gateway before exposing them to other machines, then add center redundancy and agent caching for a multi-machine deployment.
