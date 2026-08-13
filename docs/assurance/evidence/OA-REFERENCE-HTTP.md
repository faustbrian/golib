# OA-REFERENCE-HTTP Evidence

Observed at `2026-08-13T10:27:45Z` on `darwin/arm64` with Go `1.26.5`.

## Scope

This report covers the maintained non-production service at
`pkg/service/integration/reference-http`. The service composes only public
Golib APIs for process lifecycle, management probes, configuration, routing,
JSON-RPC, correlation, RFC 9421 HTTP signatures, content digests, signed
capabilities, bearer authentication, RBAC authorization, tenancy, validation,
OpenTelemetry instrumentation, and fail-closed audit recording.

## Exercised Behavior

- startup and management listener availability;
- readiness withdrawal while a dependency is unavailable and recovery without
  process restart;
- valid signed and capability-bound JSON-RPC traffic;
- correlation and request identity propagation to response, result, and audit;
- rejection of missing HTTP signatures and content digests;
- rejection of a tampered capability under an otherwise valid HTTP signature;
- rejection of invalid bearer credentials and cross-tenant authorization;
- bounded invalid-parameter handling;
- graceful context-driven shutdown and restart with fresh listeners;
- race-detector execution of the complete reference-service test package.

## Verification

The repository module gate passed format, tidy, Go safety, vet, tests, lint,
staticcheck, vulnerability, secret, license, SBOM, NilAway, and documentation
checks. Gates intentionally not required for this non-releasable integration
harness were reported as not applicable. A separate race run passed even
though the catalog does not require race evidence for interoperability
harnesses.

## Claim Boundary

This evidence proves the HTTP reference composition only. It does not prove
Linux, container, ECS, managed dependency, durability, load, soak, chaos,
rolling-deployment, rollback, recovery-drill, or production behavior. Those
remain separate mandatory operational-assurance scenarios.
