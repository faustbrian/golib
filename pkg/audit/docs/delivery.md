# Delivery and failure modes

Every sink call accepts `context.Context`. Callers must provide deadlines for
external operations. Cancellation before an append is a confirmed rejection;
a commit failure is unknown and must be reconciled by record ID. Validation,
capacity, duplicate-content conflict, and statement failures before commit are
confirmed rejections. A successful identical resubmission is reported as a
duplicate. Sinks preserve input order in bounded batches; the memory and
PostgreSQL adapters commit a batch atomically and return no partial success.

Core performs no implicit retry. Reconcile unknown outcomes and retry only the
same immutable record ID and canonical bytes. A different record under the
same ID is rejected. Adapter and buffer capacities are explicit; backpressure
is returned rather than discarded. The absolute core batch ceiling is 1,000.

Fail-open alerts and durable-buffer writes are part of the caller-selected
result. They run with an explicit nonzero `RecoveryTimeout` bounded by
`MaxRecoveryTimeout`, even if the primary-operation context has ended. If a
secondary action fails, the operation returns a public classification without
retaining arbitrary dependency diagnostics. Cancellation and deadline
classifications remain inspectable. Panicking custom `Is`, `As`, or outcome
classifiers are contained and conservatively mapped to an opaque failure.

Every sink acknowledgement is checked against submitted record IDs, statuses,
batch length, and input order. A malformed acknowledgement is an unknown
outcome and is never allowed to trigger an unqualified success. A confirmed
committed error is reported as persisted and does not enter fail-open or buffer
fallback.

No production component starts a background goroutine. A `DurableBuffer` must
report finite record, batch, and persisted-byte capacity before a recorder is
constructed. The adapter owns its worker lifetime, shutdown, retry schedule,
and durable storage; core rejects zero, incoherent, or over-ceiling limits.
Panics while reading buffer limits are contained as invalid configuration, and
an observation-only clock panic cannot change an append result.
This makes leak and shutdown behavior an adapter responsibility rather than a
hidden recorder lifecycle. The memory adapter does not implement
`DurableBuffer`: it is process-local, loses state with the process, and is not
a durable deployment.
