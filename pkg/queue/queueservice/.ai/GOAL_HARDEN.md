# Goal Harden: Queue Service Lifecycle Adapter

## Mission

Prove queue resources drain and recover correctly through backend failure,
process termination, duplicate signals, and horizontal scaling.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Model producer and worker lifecycle states and every allowed/forbidden
  transition, ownership transfer, and terminal result.
- Inject callback errors/panics at startup, publish, handler, drain, release,
  shutdown, and rollback boundaries.
- Race run, readiness, health, repeated stop, cancellation, and backend failure;
  detect double close, lost work, goroutine leaks, and stale readiness.
- Use Redis and Valkey durable backends to test disconnect, reconnect, lease
  expiry, handler timeout, redelivery, dead-letter failure, and shutdown.
- Kill pods before/after handler effects and settlement; prove the documented
  duplicate window and no acknowledgement of incomplete work.
- Verify one total termination budget and behavior when it expires.
- Stress scale-up/down and rolling deployment so intake ownership and
  visibility/lease semantics remain backend-correct.
- Benchmark adapter overhead and drain latency independently from backend work.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests with no unresolved lifecycle, loss,
duplication, race, security, or backend finding.
