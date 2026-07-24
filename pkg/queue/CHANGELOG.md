# Changelog

All notable changes are documented here. The project follows semantic
versioning and Keep a Changelog structure.

## [Unreleased]

### Changed

- Retry Valkey integration container creation when Docker starts the container
  without publishing its requested port, and retain the endpoint error for
  diagnosis instead of polling without reporting its cause.
- Regenerated the complete documentation bundle from the current package
  documentation and release notes.
- Run the Redis Pub/Sub shutdown lifecycle benchmark once instead of allowing
  benchmark calibration to repeat its intentionally expensive untimed setup.
- NSQ statistics now synchronize with consumer initialization and shutdown so
  callers cannot observe a partially published consumer or trigger a data race.
- Reject malformed embedded job metadata explicitly in NSQ dead-letter wire
  boundary coverage.
- RabbitMQ restart integration now waits for a successful AMQP constructor
  after container restart instead of assuming container start implies protocol
  readiness under a concurrent full-backend test run.
- Valkey Streams retry and replay now carry bounded original/prior
  dead-letter identifiers and replay generation through native stream entries
  and repeated terminal records. Partial or overflowing lineage is rejected.
- Redis Streams dead-letter retry now carries bounded redrive lineage into the
  durable retry entry, matching replay behavior so another terminal failure
  retains its original and prior dead-letter identifiers.
- Redis Streams and Valkey Streams v1 management records now populate the
  last-delivery time from the package-owned settlement observation and include
  the configured management worker version when it is known.
- Exhausted NSQ and RabbitMQ deliveries now retain the terminal
  `attempts_exhausted` failure code even when the handler supplied a generic
  retryable code. RabbitMQ malformed attempt headers also retain their
  dedicated safe failure code.
- Valkey integration coverage now verifies that recovered handler panics are
  treated as permanent failures and dead-lettered instead of remaining
  pending for another consumer.
- Redis Streams and Valkey Streams record-read transport failures now expose
  secret-safe text and the stable `management.ErrManagementUnavailable`
  sentinel while preserving caller cancellation and the native cause for
  programmatic inspection.
- Valkey Streams record listings now use bounded native-ID cursors across the
  complete retained stream in either occurrence-time direction. Unsupported
  sort fields fail explicitly instead of sorting a truncated local snapshot.
- Valkey Streams source `WithMaxLength` no longer silently trims failure and
  dead-letter records; record eviction is disabled until explicitly enabled.
- Redis Streams source `WithMaxLength` no longer silently trims failure and
  dead-letter records. Record eviction is disabled unless explicitly enabled
  with exact maximum-count retention.
- NSQ now requeues retryable, canceled, and infrastructure failures under a
  bounded broker-attempt policy, then publishes a terminal envelope before
  finishing exhausted, permanent, or malformed source messages.
- RabbitMQ now declares a durable dead-letter exchange and queue by default.
  Retryable failures use confirmed republish with a bounded delivery-attempt
  header; exhausted, permanent, and malformed work is confirmed to the
  terminal destination before the source delivery is acknowledged.
- Redis Streams now defaults to bounded pending recovery and dead-letter
  policy instead of leaving failed and malformed entries indefinitely pending.
- Valkey record readers and the authenticated HTTP transport now preserve
  stable unsupported, not-found, malformed-cursor, unavailable, stale,
  conflict, partial, and unknown management outcomes through `errors.Is`.
- Added the `GO-SAFETY-1` ownership, concurrency, race, fuzz, resource, and
  benchmark standard with an executable `make safety` gate.
- Moved AI planning and hardening briefs into `.ai/` and clarified the
  separate purposes of ownership notices and detailed fork provenance.
- Valkey Streams now dead-letters explicitly permanent or malformed handler
  failures immediately, while canceled and infrastructure failures stay
  recoverable instead of becoming terminal through the attempt limit.

### Added

- Dedicated dead-letter incident/recovery and Laravel Horizon failed-job
  migration guides covering capability negotiation, privacy, retention,
  redrive, ambiguous outcomes, backup, rollback, and intentional differences.
- Stable public codes for unsupported payload versions, lease loss,
  dead-letter destination outages, and administrative quarantine. Durable
  backends now classify terminal-destination append failures and lost stream
  settlement ownership without exposing backend error text.
- Low-cardinality observer fields for the package-owned classification and safe
  failure code on retry, handler failure, acknowledgement failure, rejection,
  and rejection-failure events.
- A pinned, reproducible mutation gate covering failure classification, Redis
  record cursors, lineage/control decisions, root settlement/runtime semantics,
  Redis/Valkey status decisions, and the Valkey native transport at 100% mutant
  coverage and efficacy in pull-request and release CI.
- Rolling-compatible retention capability identifiers for exact count, time,
  and byte policies. Redis and Valkey Streams advertise exact count retention
  only when deliberately configured; absent time/byte capabilities remain an
  explicit unsupported result.
- Bounded optional `job.Metadata` for original job identity, payload schema and
  content type, enqueue time, retry policy, handler and job type, tags, trace,
  tenant, and producer version propagation into operational records.
- Redis Streams, Valkey Streams, and NSQ dead-letter records now populate
  supplied v1 job metadata while preserving broker source identity separately.
- Valkey Streams `WithRecordRetention` for deliberate exact maximum-count
  retention independent of source-stream trimming.
- Redis Streams `WithRecordRetention` for deliberate exact maximum-count
  retention independent of source-stream throughput trimming.
- NSQ `WithDeadLetter` terminal-topic configuration and `DecodeDeadLetter`
  conversion to hidden-by-default backend-neutral management records.
- RabbitMQ `DeadLetterConfig` and `WithDeadLetter` overrides for package-owned
  terminal topology, maximum delivery attempts, classification headers, and
  stable failure codes.
- Allowlisted Redis Streams replay with durable reject-or-replace duplicate
  tracking, bounded registry capacity, replay lineage propagation, and
  explicit partial outcomes at non-atomic Redis boundaries.
- Native Redis Streams retry, bounded bulk retry, delete, and confirmed
  record-purge commands with durable append-before-delete ordering, bounded
  in-memory idempotency, serialized mutations, and explicit partial outcomes.
- A bounded Redis Streams `management.RecordReader` with native-ID cursors,
  occurrence-time ordering, bounded search, strict v1 decoding, and explicit
  hidden/redacted/revealed payload inspection.
- Package-managed Redis Streams failure records, malformed-delivery handling,
  bounded `XAUTOCLAIM` recovery, classified terminal settlement, and
  append-before-ack dead-letter records.
- Error-aware delivery settlement callbacks, allowing durable adapters to
  receive classified handler outcomes without breaking legacy acknowledgers.
- Stable failure classifications and management outcome errors that preserve
  wrapped causes and resolve joined failures with safety-first precedence.
- Deterministic `management.ResolveFailure` classification/code selection for
  wrapped and joined failures, independent of join ordering.
- A versioned backend-neutral dead-letter envelope carrying bounded source,
  timing, classification, diagnostics, lineage, and retention metadata through
  the authenticated management transport.
- Versioned Valkey failure and dead-letter records that durably retain terminal
  classification, safe failure code, source identity, stream, consumer group,
  attempts, and broker-derived times while continuing to read legacy records.

- A reconciled dead-letter architecture with explicit state transitions,
  current backend paths, capability gaps, crash boundaries, and data-plane
  ownership.
- A focused dead-letter completion goal covering terminal settlement,
  backend capability parity, redrive safety, retention, and control-plane
  interoperability.
- A stable `management` package for rolling-version negotiation and
  fail-safe worker/control-plane capability intersection.
- Bounded management worker and queue status contracts with explicit support
  flags for backend-dependent queue measurements.
- Bounded status-reader pages for backend-neutral worker and queue
  observations with validated opaque cursors and adapter output.
- Bounded bearer-authenticated `managementhttp` worker and queue status
  transport with explicit snake-case JSON, strict decoding, request/output
  validation, cancellation, response limits, and secret-safe failures.
- Bounded bearer-authenticated `managementhttp` command transport implementing
  `management.Controller`, with strict request/result validation, correlated
  acknowledgements, cancellation, and secret-safe failures.
- Bounded bearer-authenticated `managementhttp` failure and dead-letter
  transport implementing `management.RecordReader`, with strict list and
  inspection validation and least-privilege payload disclosure.
- Validated management command and acknowledgement contracts with explicit
  action/target compatibility, deadlines, destructive-action confirmation,
  replay policy, redacted failure codes, and partial or unknown outcomes.
- Confirmed management bulk retry with a backend-owned selection capped at
  1,000 failure or dead-letter records.
- Revisioned desired-state reader, applier, and bounded reconciler contracts
  that reject rollback and same-revision conflicts, preserve retryability, and
  do not start hidden polling goroutines.
- Queue-owned worker lifecycle enforcement for pause, resume, drain, and
  terminate commands, including admission barriers, in-flight accounting,
  native backend status decoration, and durable desired-state application.
- Bounded failure and dead-letter listing, inspection, cursor, search, sort,
  adapter-output, and hidden-by-default payload disclosure contracts.
- Native Valkey failed-attempt retention and `management.RecordReader`
  inspection for failure and dead-letter streams, with bounded pagination,
  hidden-by-default payloads, and advertised backend capabilities.
- Native Valkey retry, bounded bulk retry, delete, and record-purge commands,
  including atomic active-failure redrive and explicit partial or unknown
  outcomes at backend ambiguity boundaries.
- Native Valkey replay to explicitly allowlisted streams, with source
  preservation and atomic reject-or-replace duplicate policy backed by a
  bounded durable registry.
- A native `valkey-go` standalone Streams worker with bounded,
  credential-safe configuration, package-owned stream semantics, deferred
  acknowledgement, stale-delivery reclaim, terminal dead-lettering, honest
  consumer-group statistics, and monotonic lifecycle counters.
- A shared stream-worker conformance suite that freezes package-owned Redis
  Streams and Valkey Streams lifecycle guarantees without coupling clients.
- External compile-time and source-level Streams API contracts that prevent
  native client types from entering stable queue signatures.
- An explicit API-compatibility gate in local, pull-request, and release
  automation for both native Streams backends.
- Complete Valkey 9 adoption, configuration, TLS, ACL, deployment, upgrade,
  observability, troubleshooting, recovery, migration, and example guidance.
- Valkey delivery, option, native-response, and settlement-state fuzz targets
  in the mandatory safety gate.
- Allocation-reporting Valkey enqueue, consume, reclaim, acknowledge, retry,
  and shutdown benchmarks.
- Valkey package-wide goroutine leak verification across success, failure,
  cancellation, reclaim, and forced-shutdown tests.
- A pinned Valkey 9.1 Testcontainers matrix covering native lifecycle, ACL,
  TLS, restart, network partition, reclaim races, poison handling, retries,
  dead-letter failures, and handler panics; its independent gate blocks
  releases alongside the retained Redis Streams integration gate.
- A standardized OSS repository skeleton covering policy, documentation,
  legal notices, Go tooling, pinned CI, security, and release automation.
- Evidence-driven audit and hardening goal covering lifecycle safety,
  backend-specific delivery semantics, failure injection, and operations.
- Consolidated core, Redis, Redis Streams, NATS, NSQ, and RabbitMQ packages.
- Error-returning backend constructors.
- Structured lifecycle observation.
- Explicit post-handler settlement for durable backends.
- Bounded backend connection, request, and NSQ touch configuration.
- Backend and logical queue identity on lifecycle events.
- Redis Streams group depth, lag, pending, and oldest-job-age statistics.
- Hermetic Redis and NATS scenarios plus Redis enqueue, consume, ack, retry,
  and shutdown benchmarks.
- Consistent repository automation for Go 1.25, CI, dependency review,
  guarded semantic releases, and generated portable AI documentation.
- Bounded non-panicking delivery decoding and fuzz targets for every backend.
- Lifecycle, failure-model, performance, integration-evidence, and hardening
  reports.
- Hermetic Sentinel, lossy-delivery, and same-endpoint broker restart evidence.

### Fixed

- Made in-memory ring shutdown completion durable so draining the final task
  cannot lose its wake-up when it races with the shutdown waiter.
- Handler panics, plain failures, acknowledgement failures, and rejection
  failures now cross logs, observations, and durable settlement as bounded
  safe classified errors while retaining programmatic causes.
- Fuzz gates now use one worker by default to avoid coordinator deadline
  shutdowns under shared-runner contention. `FUZZ_WORKERS` can raise the bound.
- Redis connection-error tests no longer depend on host scheduler timing.
- Backend integration matrices now run automatically for direct `main` and
  `develop` pushes in addition to pull requests and release verification.
- Timeout coverage now coordinates on context cancellation instead of racing
  scheduler timing against a fixed sleep.
- Valkey restart integration now waits for actual native-client readiness
  instead of assuming a fixed container startup delay.
- Valkey backend errors now retain programmatic causes while redacting native
  endpoint, credential, payload, and metadata text from public error strings.
- Redis connection setup now honors its timeout on all platforms without
  hidden dial retries.
- Redis Sentinel startup now rejects unreachable endpoint sets before creating
  go-redis failover pools that can outlive a canceled Windows dial.
- Redis Sentinel startup now validates the Sentinel protocol within the shared
  connection deadline before constructing failover pools.
- Queue-only benchmarks now size their ring for duration-based runs.
- Bound fuzz-smoke concurrency to avoid deadline flakes on high-core hosts.
- Custom metric collectors are now honored.
- Backend startup errors no longer continue with nil clients.
- Redis Streams, NSQ, and RabbitMQ no longer acknowledge before handling.
- Core NATS no longer rejects valid messages by calling its reply-based `Ack`.
- Malformed backend deliveries return decoding errors instead of zero-valued jobs.
- Repeated startup and release-before-start no longer duplicate schedulers or
  deadlock the in-memory ring.
- Callback and settlement panics are isolated without corrupting worker counts.
- Redis debug output no longer exposes credentials or connection strings.
- Redis Pub/Sub constructors now await subscription acknowledgement before an
  immediate publish can proceed.
- Redis Streams consumes pre-start work and does not duplicate PEL entries on
  shutdown.
- RabbitMQ publishes persistent jobs, waits for publisher confirms, rejects
  malformed deliveries without requeue, and bounds publish waits.
- NSQ finishes malformed poison messages instead of redelivering forever.
- Network messages, retry metadata, scheduler intervals, and default in-memory
  admission now have explicit safety bounds.
- NSQ startup and Redis Streams reader shutdown are serialized with teardown;
  Core NATS drains callbacks without duplicate completion-channel closes.
- RabbitMQ establishes its queue binding before confirmed publication.
- Credential-bearing Redis, NATS, and RabbitMQ client errors are redacted while
  retaining their causes for programmatic inspection.
- Queue request wake-ups now converge on one shutdown check so full-coverage CI
  results do not depend on scheduler selection between simultaneously ready
  channels.

[Unreleased]: https://github.com/faustbrian/golib/pkg/queue/compare/v0.0.0...HEAD
