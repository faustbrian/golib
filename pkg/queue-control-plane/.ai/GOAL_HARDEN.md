# Hardening Goal: Queue Control Plane

## Objective

Prove that the control plane remains secure, truthful, bounded, auditable, and
non-disruptive under partitions, stale workers, backend failure, malicious users,
rolling upgrades, and large fleets.

## Required Audits

### Protocol And Failure Isolation

- Test absent, stale, duplicated, reordered, delayed, malformed, older, and newer
  worker events and command responses.
- Prove control-plane outage cannot corrupt queue delivery or worker state.
- Prove durable pause/resume/drain commands converge or expose explicit unknown
  outcomes without false success.
- Verify protocol negotiation and unsupported capabilities fail safely.

### Administrative Mutation Safety

- Exercise duplicate retry, replay, delete, purge, pause, resume, drain, and
  scaling requests with idempotency and audit assertions.
- Prove destructive bulk actions require bounded selection and explicit
  confirmation.
- Test process death at every command, persistence, and audit boundary.
- Verify failed audit persistence blocks sensitive mutations according to policy.

### Security

- Exhaustively test authentication and authorization for every API, CLI, and UI
  operation, including object-level and tenant/queue scope.
- Mutation-test authorization checks and default-deny behavior.
- Test CSRF, CORS, origin, session, token, redirect, injection, XSS, clickjacking,
  request smuggling, rate limiting, and payload disclosure.
- Ensure queue payloads, credentials, tokens, and backend endpoints remain
  redacted without privileged explicit access.

### Scale And Operations

- Bound heartbeat cardinality, worker/queue/job listings, searches, filters,
  command fan-out, history, retention, telemetry labels, and browser payloads.
- Test large fleets, reconnect storms, stale-worker storms, dead-letter backlogs,
  metric backend outages, PostgreSQL saturation, and Valkey loss.
- Validate backup, restore, retention, schema migration, and disaster recovery.
- Prove UI/API/CLI consistency and accessibility for critical workflows.

## Required Deliverables

- Data-plane/control-plane failure and protocol compatibility matrices.
- Administrative authorization and destructive-action threat model.
- Fault-injection, mutation, race, browser-security, and scale suites.
- Horizon migration parity and intentional-divergence report.
- Incident, backup, recovery, retention, and upgrade runbooks.
- Updated API, CLI, UI, security, operations, FAQ, and `CHANGELOG.md`.

## Release Blockers

- Control-plane failure can corrupt or silently stop normal queue delivery.
- Unauthorized, unaudited, non-idempotent, or falsely successful mutation.
- Payload/credential disclosure, tenant escape, XSS, CSRF, or privilege bypass.
- Unbounded fleet, history, telemetry, command, search, or browser behavior.
- Raw Redis/Valkey queue operations duplicated outside `queue`.
- Missing meaningful 100% coverage or a failing GitHub Actions gate.

## Completion Criteria

- Protocol, partition, mutation, authorization, scale, and recovery suites pass.
- Race, fuzz, mutation, browser, vulnerability, compatibility, and performance
  gates pass.
- Every Horizon capability is implemented or documented as intentionally
  delegated or excluded.
- No release blocker remains and `CHANGELOG.md` is current.
