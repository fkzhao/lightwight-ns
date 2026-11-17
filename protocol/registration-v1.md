# Center registration protocol v1

The registration SDK talks directly to the center's HTTP API. A service definition and its TCP or HTTP health policy must already exist. Use `Content-Type: application/json` for requests. All instances are identified by a center-generated `instanceId`.

## Register or renew idempotently

`POST /api/registrations`

```json
{"serviceName":"order-service","address":"10.0.0.11","port":8080,"token":"64 lowercase hexadecimal characters"}
```

The token is a random 256-bit secret generated once per lease. Repeating the same request with the same token renews the existing instance. A successful response is:

```json
{"instanceId":"example-id","leaseTtlSeconds":30,"expiresAt":1790000000}
```

`expiresAt` is a Unix timestamp in seconds. The SDKs renew every 10 seconds. A new or changed instance is unavailable until its health check passes. If renewal stops, the center removes the instance during the next health-check cycle after the 30-second lease expires. The SDK may retry registration with the same token after a network outage; a new instance ID can be returned if the old lease has expired.

## Explicit renewal and deregistration

`POST /api/registrations/{instanceId}/renew` with `{"token":"..."}` extends a current lease. The SDKs use idempotent registration instead because it also recovers an expired lease. `DELETE /api/registrations/{instanceId}` with the same token deregisters the instance on graceful shutdown.

The center stores only a SHA-256 hash of the token, not the token itself. A different token cannot renew or delete the lease. The management API and registration API currently have no authentication or TLS; deploy the center on a trusted network or behind an authenticated gateway. This repository does not yet implement automatic leader election or replicated storage.
