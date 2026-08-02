# Goal: Circuit Breaker Resilience Integration

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Audit and extend the implemented `circuit-breaker` package for coherent
composition with the completed resilience stack. This goal supplements the
executed base goals and MUST NOT turn the package into a generic policy runner.

## Required Additions

- Standardize outcome classification so bulkhead, rate-limit, adaptive-limit,
  adaptive-throttle, retry-budget, cache, and hedge-local rejections are ignored
  by default rather than recorded as dependency failures.
- Define whether a shared breaker observes each physical attempt or the final
  logical operation and provide explicit adapters for both reviewed models.
- Add bounded open-duration schedules and jitter only if not already complete,
  with deterministic reset and random-source behavior.
- Make manual isolate, force-open, reset, and disable operations generation-safe
  and observable without ambient configuration.
- Ensure snapshots and events expose process-local scope, policy revision,
  generation, probe utilization, and rejection reason.

## Kubernetes Semantics

Breaker state MUST remain pod-local by default. A new or restarted pod begins
with fresh state and can send probes while peers remain open. Documentation
MUST cover cold start, aggregate probes, mixed rollout revisions, scale-up,
scale-down, and SIGTERM drain.

An open dependency breaker MUST NOT fail liveness. Readiness behavior, if an
application chooses it, MUST consider whether removing pods would amplify the
dependency outage. Distributed breaker state remains out of scope absent a
separate consensus and failure-semantics design.

## Composition Deliverables

Publish tested timelines for breaker placement relative to retries, hedges,
per-attempt timeout, bulkhead, adaptive limiting/throttling, rate limiting, and
cache. Document when half-open probes may retry or hedge; safe defaults SHOULD
allow one bounded physical probe rather than amplify it.

## Acceptance Criteria

- Only genuine configured dependency outcomes affect health state.
- Stale attempts and old pod-policy generations cannot corrupt current state.
- Half-open traffic remains bounded across every supported composition.
- Existing API compatibility and documented behavior are preserved.
- Meaningful exact 100% statement coverage and exactly 100% viable mutation
  kills remain blocking requirements.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
