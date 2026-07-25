# Hardening Goal: Durable Idempotency

## Objective

Prove that `idempotency` preserves one current owner and deterministic replay
under concurrency, process death, lease expiry, backend failure, rolling
deployments, and hostile input.

## Required Audits

- Model every state transition and reject illegal or stale-owner transitions.
- Kill execution before and after acquire, handler side effect, heartbeat,
  completion, failure, result write, and cleanup.
- Prove fencing behavior when an expired owner continues running.
- Exercise duplicate completion, late heartbeat, clock skew, hot keys, lease
  extension, cancellation, and backend reconnect.
- Test PostgreSQL deadlocks, serialization failure, failover-like loss, pool
  saturation, transaction rollback, and cleanup contention.
- Test Valkey script/function atomicity, expiry races, failover, cluster routing
  where claimed, reconnect, eviction policy, and lost connections.
- Fuzz canonicalization, duplicate object keys, Unicode, numeric forms, content
  encodings, oversized values, and cross-version fingerprints.
- Verify bounded result storage, metadata, retries, waiters, polling, cleanup,
  and diagnostics.
- Ensure keys, payloads, responses, and tenant information are redacted or
  hashed according to documented policy.

## Required Deliverables

- Formal state-transition and crash-point matrices.
- PostgreSQL and Valkey conformance and failure-injection suites.
- Fencing and stale-owner proof tests.
- Versioned fingerprint and persisted-record compatibility corpus.
- Threat model, findings report, resource budgets, and benchmark baselines.
- Updated API, operations, recovery, security, FAQ, and `CHANGELOG.md`.

## Release Blockers

- Two owners can both complete successfully for one active record.
- A stale owner can overwrite a newer owner without detection.
- Fingerprint conflict can be replayed as if equivalent.
- Unbounded wait, lease, result, retry, cleanup, or memory behavior.
- Any exactly-once claim unsupported by the implementation.
- Missing meaningful 100% coverage or a failing GitHub Actions gate.

## Completion Criteria

- State, crash, fencing, backend, rolling-version, and hostile-input suites pass.
- Race, fuzz, vulnerability, compatibility, and performance gates pass.
- Every ambiguity and recovery obligation is documented precisely.
- No release blocker remains and `CHANGELOG.md` is current.
