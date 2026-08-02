# Goal: Harden The Resilience Stack

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Prove that the resilience modules remain correct when policies interact under
contention, cancellation, overload, dependency failure, Kubernetes scaling,
rolling replacement, and shutdown. This goal begins only after the package
goals and supplemental resilience goals are complete.

## Threat And Failure Model

The hardening plan MUST cover:

- retry storms, hedge amplification, retry-after abuse, and synchronized
  clients;
- queue saturation, permit leaks, starvation, priority inversion, and unfair
  admission;
- stale or delayed completions mutating a newer policy generation;
- contexts canceled before admission, while waiting, during execution, and at
  the same instant as completion;
- uncooperative operations that ignore cancellation;
- timer, ticker, goroutine, response-body, and observer leaks;
- clock rollback, forward jumps, duration overflow, random-source failure, and
  counter saturation;
- callback panic, reentrancy, blocking, recursive execution, and malicious
  classification;
- Valkey/PostgreSQL timeout, partition, failover, retry ambiguity, and partial
  success where distributed adapters exist;
- pod termination during every lifecycle state;
- empty local history after scale-up and mixed revisions during rollout; and
- HPA feedback loops where rejection lowers CPU while demand remains high.

## Cross-Policy Model Checking

Create deterministic reference models and state-machine tests for supported
compositions. Generate operation histories containing admission, execution,
completion, cancellation, timeout, rejection, retry, hedge, cache, state
transition, reconfiguration, and shutdown events.

The suite MUST prove:

- one logical operation cannot exceed its total deadline or shared work budget;
- every admitted attempt is accounted exactly once;
- rejected work does not consume execution capacity or become a false
  downstream failure sample;
- cancellation removes all outstanding waits and eventually releases all
  permits;
- loser hedges cannot overwrite the winner or leak owned resources;
- stale generations cannot mutate current breaker or adaptive state;
- policy order matches documented timelines; and
- no composition creates an undocumented retry, wait, fallback, or goroutine.

Use deterministic schedulers/clocks where practical and randomized stress to
exercise real synchronization. Keep every discovered interleaving as a stable
regression.

## Kubernetes Failure Campaigns

Use containerized or process-level integration campaigns to model multiple
replicas without claiming a full Kubernetes conformance suite. Test:

- scale from one to many replicas and back while traffic continues;
- rolling replacement with mixed policy revisions;
- SIGTERM at idle, queued, admitted, retrying, hedging, half-open, and observer
  delivery states;
- termination-grace expiry and abrupt process loss;
- local-state reset and cold-start ramp behavior;
- backend failover while distributed rate limits or caches are active;
- metrics disappearing or resetting during pod churn; and
- overload where HPA scales slowly, reaches maximum replicas, or receives
  stale custom metrics.

Tests MUST assert bounded amplification, drain behavior, accepted-work
accounting, and honest local-versus-global guarantees. They MUST NOT treat
Kubernetes restarts as a correctness mechanism for leaked state.

## Verification Matrix

Each module MUST pass:

- exact 100% statement coverage with meaningful state and outcome assertions;
- exactly 100% viable mutation kills without permissive package thresholds;
- `go test -race` under targeted high-contention histories;
- deterministic leak checks after success, error, rejection, cancellation,
  panic, and shutdown;
- fuzzing of configuration, durations, keys, weights, classifiers, and event
  sequences;
- property tests for boundedness, conservation of permits, monotonic attempt
  numbering, and valid probability/limit ranges;
- fault-injection campaigns using `fault-injection` without creating a reverse
  production dependency;
- API compatibility, examples, documentation, vulnerability, license, SBOM,
  provenance, and clean-consumer checks; and
- benchmarks with allocations, p50/p95/p99 latency, throughput, fairness, and
  contention across representative concurrency levels.

Coverage MUST NOT be obtained through vacuous assertions. Mutation exclusions
require the repository's narrow reviewed-equivalence process; reduced mutation
thresholds or bypass flags are forbidden.

## Comparative Benchmarks

Benchmark equivalent semantics against current maintained versions of
Failsafe-Go and relevant focused libraries. Separate:

- direct function execution overhead;
- admitted and rejected fast paths;
- contended wait and wake-up behavior;
- policy state update cost;
- observer disabled and enabled cost;
- one policy versus realistic compositions; and
- process-local versus distributed backend latency.

Comparisons MUST use identical deadlines, attempts, queue rules, classifiers,
result types, concurrency, and observability. Results MUST disclose unsupported
or materially different behavior rather than presenting it as a speedup.

## Operational Review

For every stateful policy, publish:

- safe defaults and capacity-sizing examples per pod and across replicas;
- alerts and dashboards that distinguish rejection from downstream failure;
- HPA metric recommendations and feedback-loop warnings;
- rollout and rollback behavior across policy revisions;
- drain and termination-grace sizing;
- fail-open/fail-closed decisions and consequences;
- degraded-mode and dependency-outage runbooks; and
- forensic snapshots that are bounded and secret-safe.

## Completion Audit

Before completion:

1. Trace every exported API to tests, docs, examples, and mutation evidence.
2. Trace every composition claim to a deterministic interaction test.
3. Trace every goroutine, timer, permit, waiter, and callback to ownership and
   shutdown proof.
4. Trace every Kubernetes claim to executable evidence or label it explicitly
   as an operational prerequisite.
5. Audit dependency direction and reject cycles or a generic framework core.
6. Run the complete affected module and reverse-dependant repository gates.
7. Review the final diff for bypasses, stale goals, contradictory defaults,
   accidental global state, and undocumented behavior.

## Acceptance Criteria

- No viable mutant, uncovered statement, race, leak, unbounded queue, permit
  loss, or undocumented composition remains.
- The stack degrades predictably under overload and recovers without manual
  process restart.
- Pod churn changes only the explicitly documented local state and capacity.
- Shutdown either completes accepted work within the bound or reports its
  interruption honestly; it never silently strands local work.
- Performance claims are reproducible and compare equivalent behavior.
- All blocking local and CI gates pass with no threshold bypass.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
