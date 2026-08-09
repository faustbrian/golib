# Security review and isolation matrix

Review date: 2026-08-09

## Security conclusions

- Tenant identity is routing data, never authentication or authorization.
- Missing, untrusted, repeated, conflicting, malformed, oversized, and
  pre-scoped transport identity fails closed at the owned propagation seams.
- Tenant identity, metadata, administrative reasons, capabilities, and scopes
  redact ordinary and Go-syntax diagnostic formatting. Explicit serialization
  and value access remain trusted-boundary operations.
- Namespace inputs are versioned and length-delimited before keyed HMAC
  encoding. Tenant, boundary, domain, and logical-key changes produce distinct
  collision-resistant namespaces without exposing raw inputs.
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
| Queue, event, cache, search, workflow, audit, and telemetry contract replay and retry | `TestIntegrationStateModelRejectsCrossTenantReplayAndRetry`, `TestPropertyEveryIntegrationFailsClosedAcrossRandomizedSequences` |
| Cache and namespace cross-tenant collisions | `TestNamespaceEncoderSeparatesScopesDomainsAndAmbiguousParts`, `TestPropertyTenantNamespacesNeverAlias`, `TestPropertyConcurrentOperationsCannotObserveAnotherTenant` |
| Goroutine lifetime and scope reuse | `TestGroupTaskPreservesSubmitContext`, `TestGroupRaceBoundariesAndWaitCancellation`, `TestGroupStressCloseAndShutdownDoNotLeak`, `TestIntegrationConcurrentSoak` |
| PostgreSQL absent scope, system scope, RLS composition, prepared plans, rollback, cancellation, stale pool state, connection loss, reconnect, and concurrent reuse | `TestPostgreSQLRLSAndPoolReuseIsolation` against live PostgreSQL with `-race` |
| Administrative partial failure, retry, resume, and attribution | `TestAdministrativeIterationStopsAtAuditOperationAndCancellation`, `TestAdministrativeResumeRepeatsFailedTenantWithCompleteAttribution`, cursor-cycle and page-bound tests |
| External-module adoption and migration | `scripts/check-clean-consumer.sh` executes authenticated HTTP, conflicting JSON-RPC, explicit SQL predicate, and no-fallback scoped cache fixtures |

## Trust and support boundaries

Provider clients and envelopes remain application-owned. The `Integration`
tests prove the tenancy contract, not a provider implementation that bypasses
it. Every provider adapter must rerun extraction on delivery, replay, retry,
dead-letter, or resume and must use its boundary namespace at persistence.

The PostgreSQL callback is trusted application SQL. Because it receives an
unrestricted `*sql.Tx`, it can temporarily change the custom setting, perform
work, and restore it before final readback. Application-owned SQL interfaces,
review, and restricted database privileges must prevent that behavior. The
live connection-loss test proves backend replacement, not multi-host primary
failover through a production proxy.

Administrative cursors must identify a stable snapshot. The package rejects
cycles and excessive pages but cannot cryptographically bind a consumer-owned
cursor to its source snapshot. Asynchronous fan-out is intentionally outside
`IterateTenants`; callers requiring it must durably own per-tenant completion,
retry, idempotency, and audit attribution.

No executable package-specific analyzer policy currently proves that
applications cannot call raw provider or telemetry sinks. The analyzer guide
defines the required consumer-side rules and their blind spots; runtime
negative tests remain authoritative for the owned seams.
