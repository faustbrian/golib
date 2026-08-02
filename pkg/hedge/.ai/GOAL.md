# Goal: Hedged Execution

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Build `hedge` as a production-grade policy for reducing tail latency by
starting bounded concurrent duplicate attempts after explicit delays. It MUST
make replay safety, work amplification, winner selection, loser cancellation,
resource ownership, deadlines, and budgets visible.

## Authoritative References

- Failsafe-Go hedge behavior: https://failsafe-go.dev/hedge/
- "The Tail at Scale" and current primary research on hedged requests;
- Go `context`, timer, memory-model, race, and HTTP request-body documentation;
- this repository's retry, HTTP client, idempotency, and resilience contracts.

## Product Boundary

Hedging starts additional attempts while earlier attempts may still be active.
Retry starts a later attempt after a prior attempt finishes. The package MUST
keep these semantics separate.

It MUST NOT infer idempotency, clone arbitrary request bodies, pick application
fallbacks, discover endpoints, load balance, retry sequentially, or conceal the
cost of concurrent attempts.

## Core Policy

Configuration MUST define:

- maximum hedge attempts in addition to the original attempt;
- fixed, scheduled, or dynamic delay strategy;
- total operation context and optional per-attempt child deadline;
- explicit result/error success and cancellation classification;
- a shared additional-work budget;
- attempt factory and result cleanup ownership;
- deterministic clock/timer and observer dependencies; and
- behavior when configuration or the attempt factory fails.

All counts, delays, and budgets MUST be finite and validated before execution.
The zero value MUST NOT enable hedging accidentally.

## Replay And Side-Effect Safety

- Callers MUST explicitly declare or prove that an operation is safe to execute
  concurrently more than once.
- Documentation MUST prohibit automatic hedging for non-idempotent writes,
  payment-like operations, queue acknowledgements, transactions, and
  non-replayable streams unless the downstream contract provides an
  idempotency key and duplicate suppression.
- Each attempt MUST receive independently owned mutable request state.
- Shared byte slices, bodies, readers, response destinations, and callbacks
  require explicit cloning or synchronization.
- The package MUST NOT interpret an idempotency key as proof that every
  downstream hop honors it.

## Scheduling And Budgets

- The original attempt starts once.
- A hedge starts only after its configured delay if no terminal winning result
  has been selected and the budget admits it.
- Dynamic delay functions MAY use caller-provided percentile data but MUST NOT
  maintain an unbounded hidden latency registry.
- Delay `<= 0`, overflow, and schedules beyond the total deadline require
  explicit validated behavior.
- Shared budgets MUST bound outstanding or recent hedge work across executions
  for the same resource.
- Budget exhaustion is observable and returns the best existing attempt result;
  it MUST NOT be reported as a downstream failure.

## Winner And Loser Semantics

- The first result classified as successful wins by default.
- If no attempt succeeds, final error selection MUST be deterministic and
  documented; errors MUST retain bounded attempt metadata without joining
  arbitrary secret-bearing messages.
- Exact ties MUST have deterministic resolution.
- Winner publication MUST be linearizable.
- Outstanding losers MUST receive cancellation immediately after a winner, but
  the package MUST state that cancellation is cooperative.
- Every loser result arriving later MUST be cleaned up through an explicit
  result disposer when it owns resources such as response bodies.
- Returning to the caller MUST NOT allow blocked result publication to leak a
  goroutine.

## Endpoint Diversity

The core policy executes caller-provided attempts and MUST NOT own service
discovery. Adapters MAY expose attempt metadata so an HTTP/RPC client can choose
different healthy endpoints or connections. Documentation MUST explain that
hedging twice to the same saturated pod may add load without reducing latency.

Endpoint diversity MUST preserve authentication, tenant routing, data
residency, consistency, and idempotency requirements. It is not a license to
route to arbitrary replicas or regions.

## Composition

Document and test:

- hedge outside versus inside bulkhead/concurrency admission;
- one breaker observation per attempt versus one logical observation;
- shared versus per-attempt rate permits;
- total versus per-attempt deadlines;
- cache lookup before hedging;
- why retry plus hedge needs a single amplification budget; and
- why adaptive-throttle rejection is not a hedgeable downstream failure by
  default.

The package MUST NOT provide a convenience preset that multiplies retries by
hedges without an explicit hard cap.

## Kubernetes Semantics

Budgets and latency history are process-local unless documented otherwise.
Scale-up can increase aggregate hedges and starts with cold latency data;
rolling updates can mix delay policies; scale-down can cancel in-flight losers.

Docs MUST provide per-pod and fleet-wide amplification calculations, account
for HPA maximum replicas, and define graceful shutdown: stop new logical
operations, stop scheduled hedges, cancel losers, wait for cooperative cleanup,
then report any uncooperative attempts honestly.

## Observability And Errors

Typed outcomes MUST distinguish no hedge needed, hedge started, budget denied,
winner selected, all attempts failed, caller canceled, total deadline, and
cleanup failure. Attempt ordinal, delay, duration, endpoint-safe identity, and
winner/loser status MUST be observable with bounded labels.

Observers MUST not run under locks or affect winner selection. Results, request
bodies, URLs with credentials, and raw errors MUST not become metric labels.

## Documentation And Automation

Document replay safety, idempotency, result cleanup, budgets, delay selection,
endpoint diversity, composition, Kubernetes sizing/drain, examples, migration,
API, FAQ, security, operations, performance, and changelog.

Require meaningful exact 100% statement coverage, exactly 100% viable mutation
kills, deterministic scheduling, race, fuzz, leak, fault, benchmark, API
compatibility, security, supply-chain, docs, and clean-consumer gates.

## Acceptance Criteria

- Work amplification is finite, measurable, and budgeted.
- No unsafe operation is hedged implicitly.
- Exactly one winner is returned and every loser is canceled and cleaned up.
- No timer, goroutine, request body, or result resource leaks.
- Composition and Kubernetes fleet effects are explicit and proven.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
