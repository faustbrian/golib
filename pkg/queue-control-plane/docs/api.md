# HTTP API reference

The administrative API uses JSON over HTTP. Except for probes, version, and
capability discovery, requests authenticate with both
`X-Queue-Control-Key-ID` and `X-Queue-Control-Key`.

All responses set defensive browser headers and `Cache-Control: no-store`.
General requests are rate limited to 120 requests per minute per authenticated
subject, or per remote address before authentication. Mutations additionally
use independent per-subject counters for each action; payload and diagnostics
reads have their own counters. These limiters are in-memory and not shared
across replicas.

The server rejects ambiguous HTTP request framing before an API handler runs.
The typed client does not follow redirects, because operator credentials must
never be replayed to a redirected endpoint. Configure the final HTTPS API URL.

## Discovery and probes

| Method and path | Authentication | Response |
| --- | --- | --- |
| `GET /health/live` | no | `{"status":"live"}` |
| `GET /health/ready` | no | `{"status":"ready"}` or HTTP 503 |
| `GET /version` | no | version, commit, and UTC build time |
| `GET /v1/capabilities` | no | enabled API source names |

Capabilities describe server wiring, not backend support for every command.
The production process reports `commands`, `audit`, `command_results`, and
`desired_state`, plus optional worker, queue, workload, and record sources.

## Commands

`POST /v1/tenants/{tenant}/commands` accepts at most 1 MiB, requires
`Content-Type: application/json`, rejects unknown JSON fields, and returns a
durable command result.

```json
{
  "idempotency_key": "example",
  "reason": "scheduled maintenance",
  "action": "pause",
  "target": {"kind": "queue", "name": "critical"},
  "requested_at": "2026-07-16T08:00:00Z",
  "confirmed": false
}
```

The authenticated subject becomes the actor; clients cannot supply an actor.
Identity fields are limited to 256 bytes and reasons to 1,024 bytes.

| Action | Allowed target kinds | Extra requirements |
| --- | --- | --- |
| `pause`, `resume` | `queue`, `worker_group` | none |
| `drain`, `terminate` | `worker`, `worker_group` | none |
| `retry`, `delete` | `failure`, `dead_letter` | none |
| `bulk_retry` | `failure`, `dead_letter` | confirmation and `selection.limit` from 1 to 1,000 |
| `purge` | `queue`, `failure`, `dead_letter` | confirmation |
| `replay` | `failure`, `dead_letter` | confirmation, destination, and replay policy |
| `scale` | `workload` | replicas from 0 to 10,000; zero requires confirmation |

Replay policy is `reject_duplicate` or `replace_duplicate`. Replay never makes
an exactly-once claim.

Command results have a durable opaque `command_id`, the caller-supplied
`idempotency_key`, `tenant_id`, `status`, optional safe `failure`, and the
applicable `dispatched_at`, `acknowledged_at`, and `completed_at` timestamps.
Status is `pending`, `dispatched`, `acknowledged`, `succeeded`, `failed`,
`unsupported`, `timed_out`, `canceled`, `partial`, or `unknown`. `accepted`
remains readable for older records. Unsupported, timed-out, canceled, partial,
and unknown results are distinct terminal outcomes; clients must not treat
them as a clean failure that is safe to repeat with a new key.

`pending` means admission is durable but dispatch has not crossed its durable
boundary. `dispatched` means enforcement may have occurred. `acknowledged`
means a data-plane result is known but terminal persistence is incomplete. A
command is `canceled` only before dispatch; later cancellation stays unknown.
New command history records also expose the bounded `deadline`,
`authentication_method`, and required `capability`. A valid worker response
adds `worker_id`, negotiated `protocol`, and `capability_available`; absent
acknowledgement data remains absent instead of being inferred.

`GET /v1/tenants/{tenant}/commands/{key}` returns a previously stored result
and requires `view` permission on `workload/commands`.

`GET /v1/tenants/{tenant}/commands` returns newest-first command history and
requires the same permission. `limit` is from 1 to 1,000 and defaults to 100.
`cursor` is an opaque continuation value from `next_cursor`; clients must not
construct or modify it. Each entry contains the immutable actor, reason,
action, target, request time, confirmation and action options, plus the durable
result. The page is tenant-scoped and uses `(requested_at, idempotency_key)` as
its stable ordering boundary.

## Workers

`GET /v1/tenants/{tenant}/workers` exists only when a worker source is wired.
It requires `view` on `workload/fleet`.

Query parameters:

- `limit`: 1 to 1,000; default 100.
- `after`: exclusive worker-ID cursor.
- `state`: `running`, `paused`, `draining`, `stopped`, `stale`, or `unknown`.
- `queue`: exact queue name.

Results are sorted by worker ID and contain a compatibility assessment. When
the management tenant document is configured, the production process follows
at most five validated 200-item upstream pages. Transport failure, malformed
output, repeated cursors, or a larger fleet fails closed with
`503 worker_status_unavailable` instead of returning a partial fleet.

## Queues

`GET /v1/tenants/{tenant}/queues` exists only when a tenant-scoped
`queue` status source is wired. It requires `view` on `queue/queues`.
`limit` is 1 to 200 and defaults to 100. `cursor` is an opaque continuation
value capped at 1,024 bytes.

Each queue contains its backend, logical name, UTC observation time, and
measurements for depth, lag, pending work, oldest age, throughput, runtime,
successes, failures, retries, reclaims, dead letters, and settlement errors.
Every measurement includes `supported`; an unsupported value is always
rendered as zero and must not be interpreted as measured data.

The production binary wires the authenticated `queue` HTTP status-reader
transport when the management tenant document is configured.

## Desired state

`GET /v1/tenants/{tenant}/desired-state/{kind}/{name}` returns the current
durable `queue` convergence record for one `queue`, `worker`, or
`worker_group`. It requires `view` permission on the exact target and accepts
no query parameters. The response contains `target`, `state`, positive
monotonic `revision`, `changed_at`, and the originating `command_id`.

The typed client exposes `GetDesiredState` and `DesiredStateReader`. The latter
binds one tenant and implements `management.DesiredStateReader` for direct use
with `management.DesiredStateReconciler`. A missing authored record returns
HTTP 404 `not_found`; workers must not infer that absence means active.

## Failures and dead letters

These routes exist when a tenant-scoped `queue` record source is wired:

```text
GET /v1/tenants/{tenant}/failures
GET /v1/tenants/{tenant}/dead-letters
GET /v1/tenants/{tenant}/failures/{id}
GET /v1/tenants/{tenant}/dead-letters/{id}
```

Lists require `record_list` on the corresponding `failure/failures` or
`dead_letter/dead_letters` resource. `limit` is 1 to 200 and defaults to 100.
`cursor` is opaque; `search` is capped at 256 bytes; `sort` is
`occurred_at`, `queue`, or `attempts`; and `direction` is `asc` or `desc`.

Inspection requires `record_inspect` on the exact record and defaults to
`payload=hidden`. `payload=redacted` may return only redacted metadata.
`payload=revealed` requires both `record_inspect` and the separate
`payload_view` permission on the exact record before the source is asked for
bytes. Revealed data is base64-encoded JSON and remains bounded by `queue`
to one mebibyte. Privileged responses use a constant attachment disposition,
`no-store`, JSON content type, and `nosniff`; backend content types remain inert
metadata. Adapters may return a more-redacted representation than requested,
never a less-redacted one.

Privileged diagnostics are independently hidden by default.
`diagnostics=revealed` requires `diagnostics_view` on the exact record. Payload
and diagnostics permissions do not imply one another. The control plane masks
either field when an adapter returns more visibility than requested.

Every revealed payload or diagnostics request receives a distinct audit
command ID and is appended to the tenant audit chain before the record source
is called. If that write fails, the API returns `503 audit_unavailable` and no
privileged backend read occurs. Sensitive-read audit entries intentionally have
no caller idempotency key and report `payload_view` or `diagnostics_view` with
result `authorized`.

The production binary wires the authenticated `queue` record transport when
the management tenant document is configured. Startup requires every tenant
client to implement `management.RecordReader`; the worker handler must still
be configured with a native backend reader for these requests to succeed.
Redis Streams and Valkey Streams workers provide that reader when their
failure stream is configured.

## Kubernetes workloads

`GET /v1/tenants/{tenant}/workloads` exists only when the Kubernetes tenant
mapping is configured. It requires `view` on `workload/kubernetes`.

`limit` is from 1 to 500 and defaults to 100. `continue` is the opaque
Kubernetes continuation token, bounded to 4,096 bytes. The response contains
Deployment generation and replica status only.

## Audit history

`GET /v1/tenants/{tenant}/audit` requires `audit_view` on `workload/audit`.
`after` is an exclusive sequence cursor. `limit` is from 1 to 1,000 and
defaults to 100. Entries include the previous and current SHA-256 hashes as
hexadecimal strings. `hash_version` 1 preserves historical chains; version 2
also seals the durable command ID and is used for every new event.

## Errors

Errors use `{"code":"..."}` and never include backend or credential detail.
Stable codes include:

| HTTP status | Codes |
| --- | --- |
| 400 | `invalid_request` |
| 401 | `unauthenticated` |
| 403 | `forbidden`, `origin_forbidden`, `preflight_forbidden`, `csrf_failed` |
| 404 | `command_not_found`, `not_found` |
| 409 | `idempotency_conflict` |
| 413 | `request_too_large` |
| 415 | `unsupported_media_type` |
| 429 | `rate_limited` |
| 503 | `audit_unavailable`, dependency-specific unavailable codes |
| 503 | `outcome_unknown` or failed readiness |
| 500 | `internal_error` |

Do not automatically retry `outcome_unknown`. Read the command result first.
An idempotent retry must reuse the identical command envelope and key.
The complete protocol, mutation, and fault evidence is maintained in the
[hardening matrix](hardening.md).
