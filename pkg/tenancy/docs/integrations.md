# Integration contracts

`Integration` gives queue, outbox, Kafka, CloudEvents, audit, correlation,
idempotency, cache, rate-limit, search, scheduler, workflow, event-sourcing,
and telemetry adapters the same explicit `Send`, `Receive`, and `Key` seams.
Applications own the wire envelope and provider client.

Senders inject only tenant scope and refuse populated metadata. Receivers make
the trust decision before accepting scope into context. Namespace keys use a
versioned length-delimited HMAC over scope, boundary, and logical key, so the
same logical value is collision-resistant and unambiguously separated between
tenants and integration domains, while raw tenant data is not disclosed.

Namespace format v2 uses a `tn2_` prefix and lowercase hexadecimal digest. The
result is valid as an OpenSearch index or alias and within the supported queue,
workflow, cache, and telemetry name alphabets. Consumers still own any provider
length prefix or suffix and MUST keep the opaque tenant namespace intact.

`Integration.Key` also requires tenant scope. System-wide and deliberately
unscoped operations must use a separately designed administrative namespace;
they cannot silently share a tenant integration namespace.

Correlation IDs do not identify tenants. Idempotency records, workflow IDs,
event stream names, scheduler entries, search indexes, queue deduplication IDs,
and cache keys all need their own tenant namespace. Audit events should include
the opaque namespace plus separately protected tenant routing data only where
the audit store's access model requires it.
