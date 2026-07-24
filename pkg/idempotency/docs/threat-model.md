# Threat model

This model treats the idempotency store as correctness-critical infrastructure.
It covers the semantic core, the memory, PostgreSQL, and Valkey adapters, and
the supplied HTTP, JSON-RPC, queue, webhook, command, outbox, logging, and
telemetry integrations.

## Security and correctness objectives

The package must preserve these properties while a record is retained:

1. At most one ownership proof is current for a logical key.
2. Every takeover advances the attempt and fencing token.
3. A stale or expired owner cannot mutate the idempotency record.
4. A different fingerprint, including a different policy version, conflicts
   instead of replaying an unrelated result.
5. A completed or failed record replays only its bounded persisted result.
6. Backend uncertainty fails closed unless the caller explicitly selects
   untracked execution.
7. Diagnostic output does not disclose request identity or replay data.

These objectives do not make an external business effect exactly once. A
handler can finish its effect and die before recording completion. The
application must use the fencing token, a unique business identity, a shared
PostgreSQL transaction, an outbox, or reconciliation at that boundary.

## Assets and trust boundaries

| Asset | Required protection |
| --- | --- |
| Logical key and fingerprint | Stable identity, conflict detection, confidentiality |
| Ownership proof | Unforgeable in normal operation, atomically checked, never logged |
| Fencing token | Monotonic within one retained record and enforced by side effects |
| Replay result and metadata | Integrity, bounded storage, restricted disclosure |
| Retained record | Atomic transitions, compatible decoding, intentional deletion |
| Backend clock | Authoritative lease decisions for durable adapters |

Caller-supplied keys, payloads, headers, JSON, and metadata are untrusted.
Application handlers are trusted to use the returned ownership proof correctly
but may crash, stall, ignore cancellation, or continue after lease expiry. The
network and backend may delay, disconnect, restart, promote a replica, or
return an unknown result. Operators may deploy mixed versions or unsafe
retention, pool, eviction, and replication settings. Logs, metrics, and traces
may be visible to people who must not see tenant or request data.

## Threats, controls, and residual obligations

| Threat or failure | Package control | Application or operator obligation |
| --- | --- | --- |
| Concurrent duplicate requests | Atomic acquisition; one acquired outcome | Do not run the handler for in-progress or conflict outcomes |
| Expired owner continues running | New owner gets a greater fence; stale mutations fail | Fence the business write; stop work after heartbeat failure |
| Same key, different request | Version and SHA-256 digest both participate in equality | Version canonicalization changes and retain compatible policy behavior |
| Ambiguous JSON | RFC 8785 canonicalization rejects duplicate keys and bounded hostile input | Select explicit limits and define the business fields included |
| Process dies after its side effect | Lease recovery and inspection expose ambiguity | Reconcile by business identity or use a shared transaction/outbox |
| Backend response is lost | Error is returned; reconnect inspection can reveal durable state | Inspect before retrying an operation with an unknown result |
| PostgreSQL transaction aborts | Record and business write roll back together in `CompleteTx` | Retry the whole transaction only under a bounded policy |
| Valkey primary is lost | One-key scripts remain atomic; a replicated record can survive promotion | Configure persistence and replication durability for the required loss window |
| Valkey cluster routing | Every record uses one opaque hash-tagged key | Do not infer multi-key application atomicity from this property |
| Eviction removes history | `Open` rejects policies other than `noeviction` | Monitor memory and rejected writes; never use eviction as cleanup |
| Oversized or adversarial input | Explicit key, version, token, result, metadata, and canonicalization bounds | Bound transport bodies before handing them to the package |
| Memory exhaustion | Memory store has a bounded record capacity | Size `MaxRecords`; use a durable backend for multi-process workloads |
| Rolling deployment changes format | Persisted schema and fingerprint policy versions fail closed | Follow the documented reader/writer rollout order |
| Sensitive diagnostics | Stable reason codes, bounded attributes, optional keyed correlation digest | Never log raw identity, payload, response, token, or metadata |
| Retention cleanup races a late process | Cleanup never proves that a process stopped | Retain records for the full retry and fencing obligation |

## Diagnostic data policy

The package's log and telemetry adapters emit bounded operation, backend,
outcome, state, and reason fields. They do not emit raw keys, tenant or caller
identifiers, fingerprint digests, owner tokens, fencing tokens, payloads,
results, or metadata. If an application needs correlation, it must use
`NewHMACKeyHasher` with a separately managed secret and emit only the returned
bounded digest. The secret must not be logged or stored beside the event.

Application logging at handler boundaries follows the same policy. Hashing is
not a substitute for access control or retention limits: restrict diagnostic
access, define a log-retention period, and rotate correlation secrets according
to the application's privacy policy.

## Residual risks and non-goals

- External effects that cannot enforce a fence, uniqueness constraint, shared
  transaction, or reconciliation can still occur more than once.
- Asynchronous Valkey replication can lose acknowledged writes that were not
  replicated before primary loss. The failover test explicitly waits for one
  replica; production durability must be configured independently.
- Deleting an expired retained record ends its fencing domain. Reusing the same
  key later creates a new record whose fence begins at one.
- The package does not authenticate callers, authorize tenants, encrypt stored
  results, schedule heartbeats, or implement an unbounded retry or wait loop.
- A compromised backend or application process can read stored data and can
  violate assumptions outside the package's checks.

Review this model whenever identity fields, canonicalization, persistence
formats, transition scripts, retention, topology claims, or diagnostic fields
change.
