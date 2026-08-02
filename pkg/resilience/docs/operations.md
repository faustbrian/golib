# Operations and security

Track outcomes by bounded policy and reason, not raw URL, tenant, credential,
request body, or error text. Useful signals include additional admissions,
budget denials, active permits, permit expiry, execution deadlines, operation
failures, and policy failures.

Sustained execution-limit denial means the configured per-call amplification
is containing work. Sustained concurrent or rolling-window denial means the
resource is overloaded or policies are amplifying too aggressively. Permit
expiry indicates abandoned ownership and should be investigated.

Observers execute synchronously after the operation outcome and budget
accounting settle, and must be fast, concurrency-safe, and nonblocking. Their
panics are recovered. A blocked observer delays return to the caller but cannot
consume the operation deadline or alter the settled result. Export to telemetry
through a bounded adapter, not an unbounded goroutine per event.

Resource and logical identifiers must be low-cardinality operational names,
not customer or secret values. Built-in maps, histories, event timelines, and
identifiers are explicitly bounded.
