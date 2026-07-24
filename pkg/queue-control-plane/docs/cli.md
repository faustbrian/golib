# CLI reference

The `queue-control` CLI emits one compact stable JSON value on standard output
by default and errors on standard error. Prefix any command with
`--output human` for indented, HTML-escaped JSON intended for terminal reading,
or `--output json` to select the machine format explicitly. Control characters
remain JSON escapes in both modes. Exit codes are 0 for success, 1 for API or
output failure, and 2 for invalid usage.

```sh
queue-control --output human failures list --tenant tenant-1
queue-control --output json command get \
  --tenant tenant-1 --idempotency-key retry-batch-1
```

Set all three environment variables:

```sh
export QUEUE_CONTROL_URL=https://control.example.com
export QUEUE_CONTROL_KEY_ID=operations
export QUEUE_CONTROL_KEY='secret-from-a-vault'
```

The key is sent only in `X-Queue-Control-Key`; avoid placing it in shell
history, process arguments, logs, or committed environment files.

## Diagnostic commands

```text
queue-control workers list --tenant TENANT [--limit N] [--after ID]
  [--state STATE] [--queue QUEUE]

queue-control retention status --tenant TENANT [--limit N] [--after ID]
  [--queue QUEUE]

queue-control queues list --tenant TENANT [--cursor CURSOR] [--limit N]

queue-control workloads list --tenant TENANT [--limit N]
  [--continue TOKEN]

queue-control audit list --tenant TENANT [--after SEQUENCE] [--limit N]

queue-control command get --tenant TENANT --idempotency-key KEY

queue-control command list --tenant TENANT [--cursor CURSOR] [--limit N]

queue-control failures list --tenant TENANT [--cursor CURSOR] [--limit N]
  [--search TEXT] [--sort FIELD] [--direction asc|desc]

queue-control failures get --tenant TENANT --id ID
  [--payload hidden|redacted|revealed] [--diagnostics]

queue-control dead-letters list --tenant TENANT [--cursor CURSOR] [--limit N]
  [--search TEXT] [--sort FIELD] [--direction asc|desc]

queue-control dead-letters get --tenant TENANT --id ID
  [--payload hidden|redacted|revealed] [--diagnostics]
```

The corresponding server capability must be enabled and the caller needs the
documented `view` ACL entry.

`command list` returns newest-first durable command history. Pass its opaque
`next_cursor` unchanged to `--cursor` to continue; `--limit` is at most 1,000.

`queues list` accepts at most 200 observations. Pass its opaque `next_cursor`
unchanged to `--cursor`. A metric whose `supported` field is false is not an
observed zero.

`retention status` reports the retention modes each worker currently
advertises as configured and which modes negotiated successfully with this
control-plane version. The current queue status contract does not disclose
numeric limits, so `limit_known` is always false; the CLI never guesses a
value. Pass `next_cursor` unchanged to `--after` to continue.

Failure and dead-letter lists accept at most 200 records. Payload inspection
is hidden by default. `--payload revealed` requires a separate `payload_view`
ACL decision for the exact record and may still return a more-redacted result.
`--diagnostics` independently requires `diagnostics_view` on that exact record.

## Mutation commands

Mutation names are:

```text
pause resume drain terminate retry bulk-retry delete purge replay scale
```

Every mutation requires:

```text
--tenant TENANT
--idempotency-key KEY
--reason TEXT
--target-kind KIND
--target NAME
```

The CLI supplies the current UTC time unless `--requested-at RFC3339` is
provided. Use a stable, operation-specific idempotency key and retain it for
result lookup.

Pause a queue:

```sh
queue-control pause \
  --tenant tenant-1 \
  --idempotency-key pause-critical-20260716 \
  --reason 'scheduled maintenance' \
  --target-kind queue \
  --target critical
```

Bulk retry at most 500 matching failures:

```sh
queue-control bulk-retry \
  --tenant tenant-1 \
  --idempotency-key retry-batch-20260716-1 \
  --reason 'recovered dependency' \
  --target-kind failure \
  --target selection-1 \
  --limit 500 \
  --confirm
```

Replay requires an explicit destination and policy:

```sh
queue-control replay \
  --tenant tenant-1 \
  --idempotency-key replay-order-123 \
  --reason 'operator-approved recovery' \
  --target-kind dead_letter \
  --target order-123 \
  --destination recovered-orders \
  --replay-policy reject_duplicate \
  --confirm
```

Scale a configured Kubernetes Deployment:

```sh
queue-control scale \
  --tenant tenant-1 \
  --idempotency-key scale-workers-20260716 \
  --reason 'incident capacity increase' \
  --target-kind workload \
  --target billing-workers \
  --replicas 12
```

`bulk-retry`, `purge`, and `replay` require `--confirm`. Scaling to zero also
requires `--confirm`. The CLI performs local shape validation, but the server
remains authoritative for authentication, authorization, idempotency, durable
audit, and adapter availability.

Every CLI mutation maps to the same versioned API envelope exercised by the
typed client and embedded UI. A duplicate must reuse the original key and the
identical flags; changed content under an existing key is a conflict, not a
new command. The client refuses redirects, so a 3xx response means the base
URL or ingress route must be corrected.

## Current command behavior

Kubernetes `scale` is dispatched only when tenant mapping and in-cluster
credentials are configured. Other mutations are dispatched through the
tenant's authenticated `queue` management endpoint when configured. That
endpoint must supply a native `management.Controller`; Redis Streams and
Valkey Streams implement failure/dead-letter reads, retry, bounded bulk retry,
allowlisted durable replay, delete, and record purge. Queue purge does not yet
have native enforcement.
Unavailable operations report a structured unsupported outcome. These
fail-closed boundaries prevent the control plane from inventing backend queue
semantics.
