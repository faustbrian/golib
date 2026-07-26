# Queue dispatch and idempotency

`queue.Dispatcher` sends a JSON envelope through `queue` containing schedule
revision ID, coordination ID, name, task, occurrence, attempt, idempotency key,
owner, fencing token, parameters, metadata, and W3C trace context. Select a
durable `queue` backend; an in-memory or pub/sub backend does not make
scheduled work durable.

The optional `idempotency.Executor` acquires `idempotency` ownership keyed by
the occurrence before dispatch. It completes the record after successful queue
submission and releases it after failed submission so a later tick can retry.
Use a persistent idempotency store in multi-replica deployments.
The idempotency fingerprint uses the coordination identity, so the same
physical occurrence remains deduplicated while old and new revisions coexist.

There is an unavoidable crash window around external queue submission and
idempotency completion. A retry is suppressed while ownership remains in
progress, but a stale takeover can submit again after the lease expires.
Consumers must treat the envelope key as idempotent. Neither Kubernetes,
leases, nor this wrapper provide exactly-once delivery.
