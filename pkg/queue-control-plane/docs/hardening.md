# Hardening evidence and release gates

This document maps the control plane's security, failure-isolation, mutation,
and scale claims to executable evidence. A green unit test is not by itself a
deployment claim: optional adapters remain unavailable unless discovery and
the compatibility matrix say they are wired.

## Protocol and failure matrix

| Condition | Required behavior | Executable evidence |
| --- | --- | --- |
| Absent worker or desired-state event | Return an empty result or explicit not-found; never infer healthy or active | `fleet.TestRegistryScopesMatchingWorkerIdentitiesByTenant`, `apihttp.TestHandlerRejectsUnsafeDesiredStateReads` |
| Stale heartbeat | Derive `stale` at read time without changing the reported protocol | `fleet.TestHeartbeatEffectiveState`, `fleet.TestRegistryRepresentsPartitionAndReconnectWithoutFalseState` |
| Duplicate heartbeat | Preserve the current record and report `duplicate` | `fleet.TestRegistryClassifiesDuplicateReorderedAndConflictingHeartbeats` |
| Reordered or delayed heartbeat | Reject the older observation without rolling state backward | `fleet.TestRegistryClassifiesDuplicateReorderedAndConflictingHeartbeats` |
| Same-time conflicting heartbeat | Preserve the record but expose `unknown`, not either claimed state | `fleet.TestRegistryClassifiesDuplicateReorderedAndConflictingHeartbeats` |
| Malformed heartbeat or page | Reject it and fail the public snapshot closed rather than return partial data | `fleet.TestRegistryBoundsWorkersAndReportsRejectedHeartbeats`, `dataplane.TestFleetSourceFailsClosedForInvalidDependenciesAndStatusTraversal` |
| Older or newer worker protocol | Intersect capabilities only inside the supported range; incompatible workers get no enabled capability | `fleet.TestNegotiateDisablesCapabilitiesForIncompatibleVersions`, `dataplane.TestRollingGoQueueHTTPCompatibilityIntegration` |
| Delayed command | Reject it before adapter execution when its bounded deadline has elapsed | `dataplane.TestControllerDispatcherFailsClosedAtEveryBoundary` |
| Malformed, mismatched, or reordered response | Persist an explicit `unknown` outcome; never fabricate success | `dataplane.TestControllerDispatcherFailsClosedAtEveryBoundary`, `control.TestServiceExecuteFailsSafeForInvalidOrUnavailableStructuredOutcome` |
| Worker transport or Valkey loss after write | Return `outcome_unknown`; require lookup and reconciliation before another key | `dataplane.TestControllerDispatcherFailsClosedAtEveryBoundary` |
| Control-plane outage | Ordinary queue delivery continues and worker lifecycle state is unchanged | `dataplane.TestControlPlaneOutageCannotStopOrMutateQueueDelivery` |
| Duplicate lifecycle command | Return the prior result and do not apply the command twice | `dataplane.TestControllerDispatcherEnforcesLifecycleThroughGoQueueHTTP` |
| Unsupported operation or capability | Return unsupported or unavailable; never issue raw backend commands | `dataplane.TestControllerDispatcherFailsClosedAtEveryBoundary`, `fleet.TestNegotiateDisablesCapabilitiesForIncompatibleVersions` |

Workers own delivery and settlement. The control plane communicates only
through versioned `queue/management` contracts and never acquires a Redis or
Valkey client. A management outage can therefore make an administrative
outcome unknown, but it cannot become part of ordinary dequeue, handler,
acknowledgement, rejection, retry, or dead-letter execution.

## Mutation and crash-boundary matrix

Every mutation follows this sequence:

1. validate the complete envelope;
2. authenticate the caller and authorize the exact tenant and object;
3. atomically insert the pending command, desired state where applicable, and
   admission audit event;
4. atomically persist `dispatched` before invoking the adapter;
5. persist `acknowledged` after a valid data-plane response;
6. atomically store the terminal result and completion audit event.

| Failure or replay point | Durable and externally visible result | Evidence |
| --- | --- | --- |
| Process death during authorization | No command, audit, desired state, or dispatch | `postgres.TestPostgresProcessDeathRollbackIntegration` against real PostgreSQL |
| Authorization denial or evaluator error | Default deny; no persistence or dispatch | `authz.TestAuthorizerFailsClosed`, `control.TestServiceExecuteAuthorizesEveryAdministrativeAction` |
| PostgreSQL acquisition saturation | No transaction callback and no dispatch | `postgres.TestPostgresTransactionRunnerFailsClosedWhenPoolIsSaturated` |
| Command insert, desired-state write, or admission-audit failure | The transaction rolls back and dispatch is blocked | `postgres.TestJournalAcceptPersistsAcceptedCommandAndAuditAtomically`, `postgres.TestJournalAcceptFailsClosed` |
| Process death during admission | PostgreSQL rolls the entire uncommitted transaction back; no partial row is visible | `postgres.TestPostgresProcessDeathRollbackIntegration` against real PostgreSQL |
| Process death before dispatch commits | Durable `pending` remains and the adapter was not called | `postgres.TestPostgresProcessDeathRollbackIntegration`, `control.TestServiceExecuteDoesNotDispatchWhenDispatchBoundaryCannotPersist` |
| Process death after dispatch | Durable `dispatched` warns that enforcement may have occurred; no terminal result is fabricated | `postgres.TestPostgresProcessDeathRollbackIntegration` against real PostgreSQL |
| Process death after acknowledgement | Durable `acknowledged` preserves the known data-plane boundary for reconciliation | `postgres.TestPostgresProcessDeathRollbackIntegration` against real PostgreSQL |
| Transport loss during dispatch | Terminal outcome is explicit `unknown`, never success | `dataplane.TestControllerDispatcherFailsClosedAtEveryBoundary` |
| Process death during result or completion-audit persistence | PostgreSQL preserves the prior acknowledged state and audit chain; no false terminal state is exposed | `postgres.TestPostgresProcessDeathRollbackIntegration` against real PostgreSQL |
| Result or completion-audit failure | The completion transaction rolls back and the API returns `outcome_unknown` | `postgres.TestJournalCompleteFailsClosed`, `control.TestServiceExecuteExposesUnknownOutcomeWhenCompletionFails` |
| Duplicate key with identical envelope | Return the stored result; no dispatch or second completion audit | `control.TestServiceExecuteDeduplicatesEverySensitiveMutationBeforeDispatch`, PostgreSQL integration |
| Duplicate key with changed envelope | Reject with idempotency conflict | `postgres.TestSQLJournalTransactionAcceptRejectsConflictingDuplicate` |

The process-death integration starts a separate test process, reaches
authorization, live adapter dispatch, and each command, desired-state,
admission-audit, dispatch, acknowledgement, result, and completion-audit
transaction boundary, kills that process, and queries the real database from
the parent. PostgreSQL
commits each command/audit transaction together or exposes neither side. The
disaster-recovery drill verifies the same repositories instead of replacing
them with an in-memory persistence model.

Bulk retry is limited to 1,000 records, record pages to 200, and all purge,
bulk retry, and replay requests require confirmation. Replay additionally
requires an explicit destination, a duplicate policy, and independent
authorization for both the source record and destination queue. Scaling is
limited to 10,000 replicas and scaling to zero requires confirmation.

## Administrative threat model

| Threat | Boundary and required control | Evidence |
| --- | --- | --- |
| Missing or stolen credential | Administrative routes authenticate; probes remain deliberately public; secrets are never returned | `apihttp.TestHandlerRejectsUnsafeCommandRequests`, `server.TestStaticAccessAuthenticatesAndAuthorizesTenantACL` |
| Tenant or object escape | Every decision includes actor, tenant, action, resource type, and exact resource ID | `server.TestStaticAccessEnforcesEveryPermissionTenantAndObjectScope` |
| Implicit allow | Anonymous, not-applicable, evaluator-error, and non-allow decisions deny | `authz.TestAuthorizerFailsClosed`; authorization mutation gate |
| Replay into another queue | Source and destination require separate decisions before persistence | `control.TestServiceExecuteAuthorizesReplaySourceAndDestination` |
| Destructive bulk request | Bounded selection and explicit confirmation are mandatory | root command validation tests and API/CLI/UI mutation-envelope tests |
| CSRF, CORS, or origin confusion | Exact origins, narrow preflight, and double-submit CSRF checks precede unsafe cookie requests | `apihttp` security tests and Chromium browser security suite |
| Credential-bearing redirect | Typed clients refuse all redirects without mutating the caller's HTTP client | `client.TestClientNeverForwardsCredentialsAcrossRedirects` |
| Request smuggling | Ambiguous HTTP framing is rejected by the server before the handler | `server.TestServerRejectsAmbiguousRequestFramingBeforeHandler` |
| XSS or unsafe browser persistence | Untrusted values render as text; credentials remain in memory only; CSP denies external content | Chromium `ui.spec.js` and `security.spec.js` |
| Clickjacking or MIME confusion | Frame ancestors, `X-Frame-Options`, MIME, cache, and referrer protections are set | API and Chromium security tests |
| Payload disclosure | Lists default hidden; reveal requires exact-record `payload_view`; bytes stay bounded and are not persisted | record API, authorization, client, CLI, and Chromium tests |
| Endpoint, token, or backend error disclosure | Tenant configuration is strict, tokens come from bounded files, and public errors use stable redacted codes | management configuration and API security tests |
| Admission denial of service | Request bytes, identities, pages, searches, cursors, rate-limit keys, and documents are bounded | API fuzz, rate-limit, configuration, and pagination tests |

The server does not issue sessions or cookies. If an ingress adds a session,
its cookie policy, fixation prevention, expiry, and logout behavior belong to
that ingress; unsafe cookie requests still encounter the API's CSRF boundary.

## Scale and operational matrix

| Surface or failure | Bound or safe behavior | Evidence |
| --- | --- | --- |
| Fleet cardinality | Registry capacity is fixed; public pages cap at 1,000 and upstream traversal at five 200-item pages | fleet and API tests |
| Reconnect and stale storms | 10,000 workers transition stale and reconnect without loss or false state | `fleet.TestRegistryBoundsTenThousandWorkerStaleAndReconnectStorms` |
| Worker queues and capabilities | Per-worker arrays and concurrency are validated and capped | `fleet.TestHeartbeatValidateRejectsMalformedWorkerStatus` |
| Queue and record backlog | Pages cap at 200; cursors and search are bounded; payload is hidden by default | queue/record API tests and maximum-page benchmarks |
| Command and audit history | Pages cap at 1,000 with opaque bounded cursors; retention batches and run count are capped | PostgreSQL, API, retention, and audit verification tests |
| Command fan-out | One command selects one adapter target; bulk retry carries a bounded data-plane selection | command validation and dispatcher tests |
| Telemetry labels or exporter outage | Only fixed action/outcome labels are emitted; a stopped metric pipeline cannot block a mutation | `control.TestTelemetryActionBoundsUnknownValues`, `control.TestInstrumentedServiceContinuesWhenMetricPipelineIsUnavailable` |
| PostgreSQL saturation | Admission fails before transaction work or dispatch | `postgres.TestPostgresTransactionRunnerFailsClosedWhenPoolIsSaturated` |
| Valkey loss | Dispatch becomes explicit unknown; no direct fallback or raw backend mutation exists | dispatcher failure tests and package dependency checks |
| Browser response and workflow | Browser consumes only bounded API pages, replaces results, and exposes no unbounded client cache | Chromium UI suite |
| Backup and restore | Commands, desired state, audit events, anchors, and migrations restore as one recovery unit | `make disaster-recovery-postgres` |

`make benchmarks` executes the 10,000-worker, 100,000-audit-event, maximum
queue-page, and maximum record-page workloads. Performance comparisons must use
a controlled runner; hosted smoke execution proves that the bounded fixtures
remain runnable, not that shared-runner latency is stable.

## UI, API, and CLI consistency

The API is authoritative. The typed client, CLI, and embedded UI construct the
same public envelope for pause, resume, drain, terminate, retry, bulk retry,
delete, purge, replay, and scale. API validation remains active even when the
CLI or UI validates locally. The browser suite checks every UI action-specific
field and critical controls are labeled and keyboard reachable. CLI tests cover
every mutation and every diagnostic surface.

## Horizon ownership and intentional divergence

The Horizon migration matrix assigns each workflow to this control plane,
`queue`, Kubernetes/HPA/KEDA, or the telemetry platform. Process
supervision, balancing, historical time-series storage, notification delivery,
and generic tags are intentionally external. Native queue purge remains
unavailable; Redis and Valkey record operations stay inside `queue`;
the control plane reports unsupported and does not duplicate backend logic.
These exclusions are safe deployment constraints, not silent partial support.

## Real backend dead-letter matrix

| Backend | Real operations through `queue` HTTP and control dispatch |
| --- | --- |
| Redis Streams 6.2.22 | Failure and dead-letter list, hidden and revealed inspect, retry, bounded bulk retry, allowlisted replay, duplicate rejection, destination consumption, delete, record purge, and capability negotiation |
| Valkey Streams 9.1.0 | Failure and dead-letter list, hidden and revealed inspect, retry, bounded bulk retry, allowlisted replay, duplicate rejection, destination consumption, delete, record purge, and capability negotiation |

`dataplane.TestRealGoQueueBackendsThroughManagementHTTP` creates actual
retryable and terminal deliveries in disposable Redis and Valkey services. It
reads records through the authenticated management client and performs every
mutation through `ControllerDispatcher`; it never acquires a native backend
client. Queue purge, time/byte retention, and runtime retention configuration
remain explicitly unsupported because the pinned `queue` contract does not
export those operations.

## Upstream contract blockers

The following workflows are release blockers rather than simulated control-
plane features at the pinned `queue` revision:

- `management.CommandResult` has one terminal status and no bounded per-record
  bulk result collection. Bulk retry therefore preserves `partial` and
  `unknown` honestly but cannot expose per-record outcomes yet.
- `management.CommandAction` exports bounded bulk retry but no bulk-delete
  action. Single delete and collection purge remain distinct; the control
  plane does not fetch pages and loop locally to imitate bulk delete.
- the management protocol has no cancellation command. This release records
  `canceled` only before dispatch; after dispatch it preserves `dispatched` or
  `unknown` for reconciliation.
- retention capabilities describe backend-owned configured policy but expose
  no runtime configuration mutation. `retention status` is truthful;
  `retention_configure` remains reserved and unavailable.
- `management.PageRequest` exports cursor, limit, search, sort, and direction,
  but no queue, classification, attempt, age, retention, or time-range filters
  and no snapshot token. The API rejects unknown filters and does not emulate
  unstable client-side pagination across changing backend pages.

Removing any blocker requires a published `queue` contract with equivalent
Redis and Valkey evidence before the control plane may advertise the workflow.

Scheduled alert evaluation is a deployment blocker, not an in-process control-
plane responsibility. `alerts.TelemetryExporter` supplies the bounded, fixed-
label `telemetry` bridge; before alerting is advertised, the deployment must
run the evaluator and exporter from an application-owned scheduler and connect
the resulting metric to the platform alerting backend.

## Release gate

A release candidate is blocked unless all of the following are green for the
exact pushed commit:

```sh
make check
make nilaway
make fuzz
make mutation
make api-compatibility
make security
make benchmarks
make browser-check
make browser
npm --prefix _browser audit --audit-level=high
make integration-postgres
make integration-queue
make disaster-recovery-postgres
```

Hosted CI additionally runs PostgreSQL 16, 17, and 18, real Redis and Valkey,
multi-platform container validation/build, Chromium, archive, API
compatibility, vulnerability, mutation, and recovery jobs. A local result does
not substitute for the GitHub result on the pushed SHA.

No release may proceed if ordinary queue delivery depends on control-plane
availability; any mutation is unauthorized, unaudited, non-idempotent, or
falsely successful; any tenant, payload, or credential can escape its boundary;
any owned cardinality is unbounded; raw Redis/Valkey operations exist outside
`queue`; meaningful coverage falls below 100%; or any required hosted gate
fails.
