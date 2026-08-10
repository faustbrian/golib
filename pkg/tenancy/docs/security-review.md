# Security review and isolation matrix

Review date: 2026-08-10

## Security conclusions

- Tenant identity is routing data, never authentication or authorization.
- Missing, untrusted, repeated, conflicting, malformed, oversized, and
  pre-scoped transport identity fails closed at the owned propagation seams.
- Tenant identity, metadata, administrative reasons, capabilities, and scopes
  redact ordinary, Go-syntax, and structured `log/slog` diagnostics. Explicit
  serialization and value access remain trusted-boundary operations.
- Namespace inputs are versioned and length-delimited before keyed HMAC
  encoding. Tenant, boundary, domain, and logical-key changes produce distinct
  collision-resistant namespaces without exposing raw inputs. Version 2 emits
  lowercase hexadecimal names accepted by supported first-party providers.
- Background tasks preserve submission values, deadlines, and cancellation
  while also remaining cancellable through group ownership.
- PostgreSQL enforcement uses a restricted application login, paired scoped
  permissive and restrictive forced-RLS policies, transaction-local scope
  verification, bounded cleanup, and physical-connection discard after reset
  or connection failure.
- Administrative iteration is sequential, audited before each attempt,
  bounded, resumable only against stable snapshot cursors, and derives every
  tenant operation independently from an unscoped base.

## Executable isolation matrix

| Threat or boundary | Negative proof |
| --- | --- |
| Invalid and enumerable identifiers | `TestTenantIDRejectsNonCanonicalAndHostileValues`, `FuzzTenantIDRoundTrip`, `TestScopeDiagnosticsDoNotDiscloseOwnedIdentity` |
| Conflicting context identity | `TestContextPropagationRejectsConflictsAndKeepsParentLifetime`, `TestTenantEnforcementRejectsAbsentSystemAndCrossTenantScope` |
| Generic propagation spoof, ambiguity, overwrite, and replay | `TestPropagationCodecRejectsAmbiguousSpoofedAndMalformedMetadata`, `TestPropagationCodecRefusesOverwriteSystemScopeAndContextConflict`, `FuzzPropagationExtraction` |
| HTTP direct access and duplicate headers | `TestMiddlewareAcceptsOnlyExplicitlyTrustedTenantHeader`, `TestHTTPExtractionRejectsDuplicateCaseVariants`, `FuzzHTTPHeaderExtraction`, clean-consumer authenticated-hop fixture |
| JSON-RPC direct access and duplicate raw keys | `TestJSONRPCExtractAndAcceptRequireExplicitTrust`, `TestJSONRPCRejectsDuplicateConflictingMalformedAndOversizedMetadata`, `FuzzJSONRPCMetadata` |
| Queue, event, cache, search, workflow, audit, and telemetry contract replay and retry | `TestIntegrationStateModelRejectsCrossTenantReplayAndRetry`, `TestPropertyEveryIntegrationFailsClosedAcrossRandomizedSequences`, external clean-consumer provider compositions |
| Live queue retry and dead-letter persistence | `scripts/test-redis-integration.sh` executes Redis Streams reclaim, retry, dead-letter inspection, missing scope, conflicting scope, and cross-tenant queue isolation under `-race` |
| Cache and namespace cross-tenant collisions | `TestNamespaceEncoderSeparatesScopesDomainsAndAmbiguousParts`, `TestNamespaceOutputIsSafeForFirstPartyProviderNames`, `TestPropertyTenantNamespacesNeverAlias`, `TestPropertyConcurrentOperationsCannotObserveAnotherTenant` |
| Live search persistence | `scripts/test-opensearch-integration.sh` executes two-tenant negative isolation against OpenSearch 2.19.6 and 3.8.0 under `-race` |
| Goroutine lifetime and scope reuse | `TestGroupTaskPreservesSubmitContext`, `TestGroupRaceBoundariesAndWaitCancellation`, `TestGroupStressCloseAndShutdownDoNotLeak`, `TestIntegrationConcurrentSoak` |
| PostgreSQL absent scope, system scope, RLS composition, prepared plans, rollback, cancellation, stale pool state, connection loss, reconnect, and concurrent reuse | `TestPostgreSQLRLSAndPoolReuseIsolation` against live PostgreSQL with `-race` |
| PostgreSQL primary failover through a proxy | `scripts/test-postgres-failover.sh` streams a pinned PostgreSQL 18 primary to a second host, interrupts an open tenant transaction, promotes the replica, redirects the same pool, and proves tenant and unscoped isolation under `-race` |
| Durable workflow retry/resume and audit attribution | `scripts/check-postgres-composition.sh` reopens first-party PostgreSQL workflow state across retry, rejects cross-tenant lease execution, verifies terminal tenant identity, and context-filters first-party audit records under `-race` |
| Administrative partial failure, retry, resume, and attribution | `TestAdministrativeIterationStopsAtAuditOperationAndCancellation`, `TestAdministrativeResumeRepeatsFailedTenantWithCompleteAttribution`, cursor-cycle and page-bound tests, external fsync journal fan-out fixture |
| External-module adoption and migration | `scripts/check-clean-consumer.sh` executes authenticated HTTP, conflicting JSON-RPC, explicit SQL predicate, provider compositions, no-fallback scoped cache, and durable administrative fixtures |
| Direct provider, replacement-context, and telemetry-label bypass | `scripts/check-analyzers.sh` executes the blocking `analysis.yml` policy against negative consumer and reviewed-adapter fixtures |
| Structured-log identity disclosure | `TestOwnedIdentityStructuredLogsAreRedacted` executes both standard JSON and text `log/slog` handlers against every owned identity-bearing value |
| Trace and log tenant disclosure | external `TestTelemetryTraceAndStructuredLogsUseOnlyOpaqueTenantScope` records only an opaque tenant namespace through the OpenTelemetry SDK and standard JSON `log/slog`, and rejects missing scope |

## Trust and support boundaries

Provider clients and envelopes remain application-owned. The `Integration`
tests and declared consumer fixtures prove only the adapters they execute; they
do not protect a provider implementation that bypasses those adapters. Every
provider adapter must rerun extraction on delivery, replay, retry, dead-letter,
or resume and must use its boundary namespace at persistence.

The PostgreSQL callback is trusted application SQL. Because it receives an
unrestricted `*sql.Tx`, it can temporarily change the custom setting, perform
work, and restore it before final readback. Application-owned SQL interfaces,
review, and restricted database privileges must prevent that behavior. The
live failover fixture proves the manager across a switching TCP proxy and
streamed replica promotion. It does not prove a consumer's production proxy
health checks, promotion fencing, or retry policy.

Administrative cursors must identify a stable snapshot. The package rejects
cycles and excessive pages but cannot cryptographically bind a consumer-owned
cursor to its source snapshot. Asynchronous fan-out is intentionally outside
`IterateTenants`; callers requiring it must durably own per-tenant completion,
retry, idempotency, and audit attribution.

The package-specific analyzer policy proves only its declared exact provider
constructors, context sources, typed tenant values, and telemetry sink. It does
not infer undeclared wrappers, reflection, generated calls, dynamic SQL, or a
provider added without updating the policy. Runtime negative tests remain
authoritative for the owned seams.
