# Agent discovery protocol v1

The machine-local agent accepts TCP on `127.0.0.1:9091` and a Unix domain socket at `/tmp/lightwight-ns/agent.sock` by default. Both transports use the same protocol: one UTF-8 JSON-RPC 2.0 request and one response per newline. A connection may carry multiple sequential requests. Each frame is limited to 1 MiB. Requests must include `jsonrpc`, `id`, `method`, and `params`; requests without an ID are not supported.

## Resolve a service

Request:

```json
{"jsonrpc":"2.0","id":1,"method":"discovery.resolve","params":{"service":"order-service"}}
```

Successful response:

```json
{"jsonrpc":"2.0","id":1,"result":{"registryVersion":12,"endpoints":[{"address":"10.0.0.11","port":8080}]}}
```

`endpoints` contains only enabled instances whose latest center health check succeeded. A known service with no available instances returns an empty array. Endpoint order is unspecified. `registryVersion` identifies the cached registry snapshot.

## Agent status

Request:

```json
{"jsonrpc":"2.0","id":2,"method":"agent.status","params":{}}
```

Successful response:

```json
{"jsonrpc":"2.0","id":2,"result":{"ready":true,"registryVersion":12,"lastSyncAt":"2026-01-01T00:00:00Z"}}
```

`ready` becomes true after the first successful sync. `lastSyncAt` is an RFC 3339 timestamp, or an empty string before the first sync. During a center outage, the agent retains its last successful snapshot.

## Errors

Errors use the standard JSON-RPC error object and preserve the request ID. Codes are `-32700` (parse error), `-32600` (invalid request), `-32601` (method not found), `-32602` (invalid params), `-32001` (agent not ready), and `-32004` (service not found). The TCP/Unix protocol is intended for local use; the agent does not provide authentication.

The center-to-agent snapshot sync is a separate Go `net/rpc` protocol on port 9090. SDKs never call that protocol.
