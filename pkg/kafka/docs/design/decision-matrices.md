# Production policy decision matrices

These decisions precede the pre-v1 API redesign. A row marked planned is a
design constraint, not a support claim.

## Package and dependency boundary

| Surface | Decision | Ownership and dependency rule | Status |
| --- | --- | --- | --- |
| Root `kafka` module | Own Kafka-specific policy contracts and a franz-go implementation. | May depend on franz-go and kadm internally; ordinary public APIs must not expose their types. | Implemented boundary; broader policy remains pre-v1 |
| Authentication | Root owns TLS, mTLS, PLAIN, SCRAM, and bounded OAUTHBEARER provider contracts. | No process-wide mutable credentials; providers own refresh and expiry semantics. | Implemented; secured-broker evidence required |
| `adapters/mskiam` nested module | Translate the supported AWS Go signer into the root token-provider contract. | The AWS SDK and signer must not enter the root module. | Planned |
| `adapters/gotelemetry` nested module | Translate stable hooks into selected OpenTelemetry Kafka conventions. | OpenTelemetry must not be required for correctness or imported by the root. | Planned |
| Test support | Publish conformance suites only for real consumer-facing seams. | No exported franz-go fakes or mock choreography contracts. | Planned |
| Application adapters | Outbox, event-sourcing, service health, schemas, and routing stay downstream. | The root never imports application or another golib module. | Existing direction, verification required |

## Delivery and ordering

| Question | Decision | Observable consequence |
| --- | --- | --- |
| Producer acknowledgement | Require all in-sync replicas and franz-go idempotence by default. | Success means the broker acknowledged the record under current ISR policy; it does not prove consumer processing or external side effects. |
| Cancellation | Distinguish stopping the caller's wait from stopping delivery. | A result must identify delivery success, definite failure, or ambiguity; cancellation alone must not be described as a broker rejection. |
| Ordering | Preserve order per topic-partition under the configured idempotent producer. | No global order, no order across partitions, and no application retry that silently creates a second logical record. |
| Keys and partitions | Require an explicit keyed or unkeyed producer policy. Record partition selection defaults to automatic; exact non-negative partitions use a separate validated value. | A zero-value key cannot accidentally opt into unkeyed production, and a zero-value partition policy cannot accidentally pin records to partition zero. |
| Batches | Return one result for every submitted record in input order. | Partial batch failure is visible without collapsing successful records into one aggregate error. |
| Async | Bound admitted records and bytes and make backpressure explicit. | Admission failure, delivery completion, callback panic, drain, abort, and close have separate outcomes. |
| Consumer guarantee | Provide at-least-once processing with automatic commits disabled. | Handler success precedes settlement; commit ambiguity, rebalance, cancellation, panic, timeout, and process death may redeliver. |
| Partition settlement | Advance only the highest contiguous successful offset owned by the current generation. | A later success never commits past a failed earlier record in the same partition; independent partitions may advance. |

## Failure and retry policy

| Area | Stable categories or strategies | Policy |
| --- | --- | --- |
| Produce | retryable, permanent, authorization, fenced, oversized, timeout, shutdown, fatal, ambiguous | Preserve `errors.Is`/`errors.As` identity and safe causes; never require string parsing. |
| Consume | handler, panic, timeout, commit, ownership lost, rebalance, authorization, fatal group | State whether the record can be redelivered and whether any offset was durably settled. |
| Retry | stop, bounded in-process retry, versioned retry topic, terminal dead letter, application delegation | No hidden infinite loop. Backoff is bounded and cancellation-aware. |
| Dead letter | acknowledged publish followed by separate source settlement, or one Kafka transaction | Expose the duplicate/loss window. Preserve source coordinates and safe classification without payload disclosure. |
| Transactions | abortable, fatal, fenced, cancelled, commit outcome unknown | Exactly-once wording is limited to a proven Kafka read-process-write boundary with read-committed consumers and offsets in the same Kafka transaction. |
| Replay | invalid plan, unavailable range, retention/truncation, compaction gap, handler failure, incomplete | Fail closed for an exact request and return completed, failed, skipped, and remaining coordinates. |

## Lifecycle and concurrency ownership

| Resource | Owner | Required lifecycle |
| --- | --- | --- |
| franz-go client | Constructed policy object | Constructor validates first; partial construction closes; close is bounded and idempotent. |
| Producer buffer | Producer | Bounded by records and bytes; drain or abort is explicit; no silent drop. |
| Transaction | One serialized transaction session | Callback lifetime is fenced; concurrent use has an explicit error; commit/abort/close are bounded. |
| Group partition | Current consumer generation | One sequential worker per partition; generation revocation stops admission and fences settlement. |
| Cross-partition work | Group runner | Optional bounded parallelism; caller owns `Run` goroutine and cancellation. |
| Replay partition | Replay invocation | Ascending per-partition order; optional bounded parallelism; no global-order claim. |
| Hook callback | Calling operation | Synchronous, bounded by policy, receives immutable/copied metadata, cannot re-enter lifecycle methods, and panics are contained and reported. |
| Credential provider | Security configuration/provider | Bounded context, explicit expiry, concurrent-safe refresh, no global cache, and no secret in diagnostics. |

Locks must not span application callbacks, broker I/O, blocking channel sends,
or unbounded waits. Constructors start no application-owned background work.

## Security policy

| Mode | Decision | Release evidence required |
| --- | --- | --- |
| Verified TLS | Production default, TLS 1.2 minimum, system or copied caller roots. | Hostname failure, root failure, TLS versions, rotation, and redaction. |
| mTLS | Owned certificate-provider contract. | Client authentication, rotation, expiry, cancellation, and redaction. |
| SASL/PLAIN | Reject unless verified TLS is active. | Successful auth plus plaintext rejection and credential redaction. |
| SCRAM-SHA-256/512 | Owned username/password provider over verified TLS. | Both mechanisms, rotation, auth denial, and redaction. |
| OAUTHBEARER | Bounded refreshing token provider over verified TLS. | Expiry, refresh, cancellation, concurrency, clock skew, failure, and redaction. |
| MSK IAM | Optional nested adapter using AWS's supported Go signer. | Provisioned/serverless modes actually exercised, credential-chain refresh, region, expiry, cancellation, and IAM denial. |
| Plaintext | Separately named development-only opt-in. | API naming and validation prevent confusion with the production default. |

## Health policy

| Signal | Meaning | Restart policy |
| --- | --- | --- |
| Liveness | The process and owned runner are not irrecoverably wedged. | Broker outage alone does not fail liveness. |
| Readiness | Configurable evidence that this instance may safely accept work. | Broker outage may degrade readiness only under explicit thresholds/hysteresis. |
| Dependency health | Current bounded Kafka connectivity and authorization state. | Diagnostic input, not an automatic restart instruction. |
| Inspection | Read-only cluster, topic, group, offset, durability, and transaction facts. | Never mutates infrastructure. |

## Evidence status vocabulary

`implemented` means the API and focused deterministic behavior exist.
`integration-tested` additionally requires a real broker. `supported` requires
the published compatibility/auth/failure matrix. `unverified` means no support
claim is made even when franz-go likely implements the protocol.
