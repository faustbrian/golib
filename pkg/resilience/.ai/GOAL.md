# Goal: Resilience Policy Composition

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Build `resilience` as the minimal shared composition foundation for focused
resilience policies. It MUST own execution composition, common outcome
vocabulary, total-deadline propagation, and shared work-amplification budgets
without implementing retry, breaker, rate, cache, semaphore, bulkhead, hedge,
adaptive-limit, adaptive-throttle, timeout, or fallback algorithms.

## Authoritative References

- Failsafe-Go policy composition: https://failsafe-go.dev/policies/
- Go `context`, memory model, errors, generics, race, fuzz, and profiling docs;
- this repository's root resilience goals and every participating package goal.

## Core Contracts

Provide small generic contracts for:

- a logical execution with caller context and immutable metadata;
- a physical attempt with stable ordinal, origin, start time, and parent;
- typed outcomes: success, operation failure, local rejection, cancellation,
  deadline, ignored, and policy failure;
- a policy that explicitly wraps the next execution stage;
- an immutable executor built from an ordered policy list;
- event observation outside policy locks; and
- a shared additional-work budget usable by retry and hedge.

The API MUST preserve typed results and original errors. It MUST avoid `any`
where generics provide a clear contract and MUST not force all policies to
share mutable global state.

## Composition Semantics

- Policy order MUST be inspectable and deterministic.
- Construction MUST reject nil, duplicate-incompatible, cyclic, or otherwise
  invalid compositions before execution.
- Outer and inner policy timing and error visibility MUST be documented.
- Local rejection MUST remain distinct from downstream failure.
- A policy MUST explicitly declare whether it operates once per logical call or
  once per physical attempt.
- Policy callbacks MUST not run while shared composition locks are held.
- The executor MUST not create a goroutine merely to invoke synchronous work.
- Asynchronous use is caller-owned through ordinary Go concurrency.

Provide timeline/introspection output that is bounded, immutable, secret-safe,
and suitable for tests and diagnostics without retaining application results.

## Shared Work Budget

The budget MUST be the single owner of retry-plus-hedge amplification:

- count original and additional work under explicitly documented rules;
- support finite total, concurrent, and rolling-window additional-work limits;
- use bounded resource identities and cardinality;
- provide atomic admission and exactly-once completion/refund where applicable;
- distinguish denied work from downstream failure;
- support deterministic clocks and snapshots;
- remain process-local unless an independent distributed implementation is
  explicitly designed; and
- prevent nested executors from accidentally bypassing or double-counting the
  same logical budget.

`retry` and `hedge` MUST consume this contract rather than implement competing
shared budget state. Compatibility adapters MAY preserve preexisting package
APIs under SemVer.

## Context And Deadline Model

The caller context owns the total cancellation/deadline. Policies MAY derive
shorter child contexts for queue wait or attempts but MUST NOT extend the total
deadline. Cancellation is cooperative; the executor MUST NOT claim that an
operation stopped when it ignored context.

The package MUST NOT add a generic timeout policy that merely races a goroutine
against a timer. Scope-specific deadlines remain with callers and owning
adapters.

## Errors And Observability

Stable typed errors MUST expose policy identity, stage, attempt, rejection
reason, budget state, and safe causes through `errors.Is`/`errors.As`. They MUST
not stringify arbitrary results, URLs, credentials, or unbounded error trees.

Observers receive bounded start, admission, attempt, rejection, completion,
and cancellation events. Observer panic, blocking, or reentrancy MUST not alter
the execution result or corrupt accounting.

## Kubernetes Semantics

Executors are immutable and reusable within a pod. Stateful policies and
budgets are pod-local unless their package explicitly provides a distributed
backend. Documentation MUST cover replica multiplication, cold start, mixed
policy revisions, HPA feedback, SIGTERM, drain, and abrupt process loss.

The package MUST not supervise replicas, coordinate pods, or mark dependency
degradation as process liveness failure.

## Boundaries

- No algorithms owned by focused resilience modules.
- No generic fallback decision or business degraded-mode selection.
- No service locator, reflection registry, hidden default stack, global
  executor, automatic policy discovery, or environment configuration.
- No transport, database, queue, Kubernetes, or OpenTelemetry dependency in the
  core module.
- No promise that arbitrary side effects are idempotent or cancelable.

## Documentation And Automation

Document composition order, timelines, execution scopes, errors, contexts,
budgets, custom policy authoring, Kubernetes effects, migration, examples, API,
FAQ, security, performance, and every supported policy combination.

Require meaningful exact 100% statement coverage, exactly 100% viable mutation
kills, model/property tests, race, fuzz, leak, fault, benchmark, API
compatibility, docs, security, supply-chain, and clean-consumer gates.

## Acceptance Criteria

- Every shared composition concept has one owner.
- Retry and hedge share one enforceable amplification budget.
- Policy order and scope are visible and executable.
- No focused algorithm is duplicated or hidden.
- Cancellation, errors, and Kubernetes-local state are represented honestly.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
