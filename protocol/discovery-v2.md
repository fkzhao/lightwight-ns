# Discovery and lifecycle extensions

The v1 Agent protocol and registration methods remain supported. Python and Java SDKs add these interfaces in version 0.2.0. Local discovery still uses newline-delimited JSON-RPC over TCP or Unix sockets; Java uses TCP. A v2 client must not silently fall back to a v1 lookup when the server does not support its filters.

## Scoped discovery

`discovery.resolveV2` accepts:

```json
{"jsonrpc":"2.0","id":1,"method":"discovery.resolveV2","params":{"service":"order-service","namespace":"prod","group":"blue","version":"v2","metadata":{"zone":"a"},"allowStale":false}}
```

Namespace and group default to `default` and are normalized to lowercase. Service name and business version match exactly. Metadata filters are combined with AND. A known service with no matching eligible instances returns an empty array; an unknown scoped service returns error -32004.

```json
{"jsonrpc":"2.0","id":1,"result":{"registryVersion":42,"stale":false,"endpoints":[{"instanceId":"example","address":"10.0.0.11","port":8080,"version":"v2","weight":75,"metadata":{"zone":"a"},"stale":false}]}}
```

Eligible instances are enabled, active, healthy, not expired, and have positive weight. Weight is an integer from 0 to 10000, defaults to 100, and is consumed by the SDK weighted selection helper. It does not duplicate endpoints in the result. A v1 lookup searches only the default namespace and group and returns its original address/port format.

## Registration metadata and draining

`POST /api/registrations` additionally accepts `namespace`, `group`, `version`, `weight`, and `metadata`. Metadata has at most 32 entries and 8 KiB of JSON; keys have at most 80 characters and values at most 512. The service definition must already exist. Repeating a registration with the same token may update its metadata while renewing its lease. It never re-enables an administratively disabled instance or reactivates a draining instance.

`POST /api/registrations/{instanceId}/drain` with `{"token":"..."}` changes lifecycle from `active` to `draining`. This excludes the instance from all new discovery results, including historical fallback. Renewal continues while the application waits for existing requests. After requests finish or a shutdown deadline is reached, the application closes the lease and its server. The registry cannot terminate existing business connections or invalidate endpoint lists held independently by an application.

For administrators, `POST /api/services/{serviceId}/instances/{instanceId}/drain` drains an instance and `POST /api/services/{serviceId}/instances/{instanceId}/enabled` with `{"enabled":false}` disables discovery. Both work for SDK instances. Explicit deletion, deregistration, and expiry of a drained instance retire its registration token permanently. Reusing a retired token returns HTTP 410 with `registration_retired`; a new application process obtains a new token when it registers. Ordinary lease expiry permits automatic SDK re-registration.

## Health policy

Service `healthCheck` fields include:

- `type`: `tcp`, `http`, or `grpc`.
- `path`: HTTP health path; HTTP requires a 2xx response.
- `failureThreshold` and `successThreshold`: integers from 1 to 20, defaulting to 3 and 2.
- `intervalMillis` and `timeoutMillis`: 0 inherits the center setting; upper bounds are 3600000 and 60000 ms.
- `grpcService`: service name for the standard `grpc.health.v1.Health/Check`; empty checks overall server health.
- `tls`, `serverName`, and `caPem`: optional gRPC TLS with certificate verification. An empty CA uses system roots.

Only gRPC `SERVING` succeeds; missing health service, `NOT_SERVING`, connection failures, and deadlines fail. There is no automatic downgrade to a TCP check. Checks have approximately 10% scheduling jitter. Instances expose last-check time, last successful probe time, error detail, duration, and consecutive counters in the management snapshot. Health details are transient. Changes of endpoint or policy invalidate in-flight probes and reset counters; restart requires fresh successful probes.

## Change propagation and cache freshness

The Agent uses center RPC `Registry.WatchSnapshot` with its last known revision. The center immediately returns a complete snapshot when the revision changes, or sends an unchanged heartbeat after 25 seconds. There is no delta history to compact; a reconnect receives a current complete snapshot. Older Agents can continue polling `GetSnapshot`, which exposes only default-scope services to prevent cross-scope lookups by clients that do not understand namespaces. A new Agent talking to an older center falls back to 5-second polling.

`agent.status` adds `stale`. If center synchronization has not succeeded within `NS_AGENT_MAX_STALE` (default 30 seconds), v1 and v2 lookups return error -32002 rather than presenting the old cache as current. A successful sync clears this state. Center long polling updates the Agent cache, not lists or business connections independently cached by callers; call the SDK again when choosing destinations.

## Self protection

Each service has an optional `protection` policy. Protection is disabled by default. Defaults when enabled are:

| Field | Default | Meaning |
| --- | --- | --- |
| `minInstances` | 10 | Minimum eligible population before automatic protection |
| `renewalThreshold` | 0.85 | Minimum fraction of eligible SDK instances seen in the heartbeat window |
| `probeFailureThreshold` | 0.5 | Failed fraction of recently probed instances that have previously passed a check |
| `windowSeconds` | 20 | Heartbeat/observation window; must be 10–29 seconds, shorter than the 30-second lease |
| `warmupSeconds` | 60 | Center startup grace period |
| `maxHoldSeconds` | 120 | Maximum duration of a single protection episode, 10–600 seconds |
| `recoverySeconds` | 20 | Stable period before ending a recovered episode |
| `allowStale` | false | Permit historical fallback when the caller also requests it |
| `maxStaleSeconds` | 60 | Maximum age since a successful probe for fallback, 1–300 seconds |

Heartbeat coverage counts distinct instances, so duplicate retries cannot mask missing instances. Disabled and draining instances do not contribute. Protection preserves registration records while marking expired leases; normal discovery continues to exclude failed or expired instances. Explicit deregistration and administrative deletion always take effect. Protection transitions are logged and shown in the Web UI.

Historical fallback is used only when no eligible healthy instance matches the query, the service permits fallback, the caller sets `allowStale`, and protection is active. Only previously healthy instances within the configured age are returned, with `stale=true`. Disabled, draining, zero-weight and non-matching instances remain excluded. The setting never makes a globally expired Agent snapshot usable.

At recovery or the hold deadline, protection ends and a 60-second cooldown prevents immediate re-entry. Expired registrations are cleaned up in batches of at most 10% of the current service population per sweep (at least one instance). Health and protection observations reset on center restart; persisted lifecycle, metadata, leases, and retired tokens survive.
