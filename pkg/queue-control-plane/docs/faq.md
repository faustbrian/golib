# Troubleshooting and FAQ

## Why did a command return `dispatch_failed`?

Without `QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE`, the process durably records
non-Kubernetes commands and fails dispatch safely. With it, inspect the stored
result and worker management endpoint. `dispatch_failed` does not prove that a
Redis, Valkey, queue, or worker operation was attempted.

## Why is `/health/live` healthy while `/health/ready` fails?

Liveness reports that the process can serve HTTP. Readiness checks the durable
dependency and returns 503 when the instance should not receive traffic. Check
the PostgreSQL DSN, network policy, TLS configuration, credentials, and schema
without logging the DSN.

## Why does a diagnostic endpoint return 404?

Routes are registered only when their source is wired. Worker and queue routes
require the management tenant mapping. Workload routes require the Kubernetes
tenant mapping. Use `/v1/capabilities` to discover current wiring.

## Why does `/ui/` return 404?

The console is opt-in. Set `QUEUE_CONTROL_UI_ENABLED=true` on serving replicas
and restart them. `/ui` redirects to `/ui/`; API-only deployments intentionally
do not register either path.

## Why do I receive `unauthenticated`?

Supply both `X-Queue-Control-Key-ID` and `X-Queue-Control-Key`, or set the three
CLI environment variables. Confirm that every replica has restarted with the
same current access document. Do not print the secret while troubleshooting.

## Why do I receive `forbidden` with valid credentials?

Authentication and authorization are separate. Match the ACL subject to the
key's `subject`, then verify tenant, action, resource type, and exact resource
ID. Deny overrides allow.

## Why do browser requests receive `origin_forbidden`?

Add the exact scheme and host to `QUEUE_CONTROL_ALLOWED_ORIGINS`. Paths,
wildcards, user information, query strings, fragments, and near-matching
subdomains are rejected. Restart the process after changing configuration.

## Can I retry a timed-out request?

Read the command by its original idempotency key first. Repeating the identical
envelope with the same key is idempotent. Never issue a new key while the
original outcome is unknown.

## Will a control-plane outage stop queue delivery?

No. Workers own dequeue, handling, acknowledgement, rejection, retry, and
dead-letter behavior through `queue`. A control or management outage may
make an administrative command unknown, but normal delivery continues with
the last worker-owned state. Do not work around the outage with direct Redis
or Valkey mutations.

## Can the control plane replace HPA, KEDA, or Kubernetes?

No. The adapter provides visibility and an explicit manual scale action only.
It does not reconcile desired replicas, restart pods, or supervise workers.

## Where are job payloads?

They are intentionally absent from current public models. Future inspection
must be hidden by default, redacted, separately authorized, bounded, and
tested before release.

## How do I verify or retain audit history?

Use the one-shot retention mode with a reviewed tenant policy as documented in
the deployment guide. It verifies the audit chain before and after bounded
deletion, preserves legal holds, and then removes only eligible terminal
commands. Do not delete rows directly.

## Where is the hardening evidence?

The [hardening matrix](hardening.md) maps partitions, crash boundaries,
authorization threats, scale limits, and release gates to exact tests and
operator behavior.
