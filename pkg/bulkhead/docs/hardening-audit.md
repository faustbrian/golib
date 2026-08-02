# Hardening audit

## Reference baseline

Consulted on 2026-08-02:

- Microsoft Azure Bulkhead pattern, updated 2026-03-19;
- Shopify Semian `main` at
  `8835b3da31b31c45970cf229ee4a8e8a61e3ce51`;
- Failsafe-Go bulkhead v0.9.6, tag
  `583dc0ef699f43945280680ae56d74cb88042bd6`;
- Fortify bulkhead v1.6.0, tag
  `dbe88d4265ec41ac245318f983eea88ddcc50415`;
- `golang.org/x/sync` v0.22.0, tag
  `1eb64d4bc0cde6da1bb8ebc7f178bb577508e5d0`;
- Go 1.26.5 context, memory-model, synchronization, timer, race, fuzz, and
  profiling documentation;
- Kubernetes 1.36 pod lifecycle, readiness, and HPA documentation.

Semantic differences are intentional: this package adds weighted permits,
strict bounded FIFO queues, explicit resource identity and partitions,
duplicate-release errors, observer containment, and application drain.
Failsafe-Go uses unit permits and a maximum wait without this package's
partition registry or queue-cardinality contract. Semian's process/system
semaphore model is not reproduced; this package is process-local Go memory.
Fortify uses unit permits, a worker goroutine for queued execution, best-effort
queue ordering, and `Close` without admitted-work drain.

## Requirement evidence

| Invariant | Executable evidence |
| --- | --- |
| isolation | fixed-partition saturation test |
| conservation | randomized reference histories, duplicate-release race, exact coverage and mutation |
| FIFO and bounded queue | ordered waiter and weighted head-of-line tests |
| cancellation and timeout | caller cancellation, absolute delayed-timer deadline, and shutdown tests |
| panic/error cleanup | generic execution tests |
| uncooperative work | retained-capacity test |
| observer containment | error, panic, reentrant snapshot, and synchronous-latency tests |
| drain | queued shutdown, active drain timeout, close races, and final snapshot |
| bounded identity | hostile configuration fuzzing and explicit registry tests |
| concurrency | repeated admission/close, cancellation/release, timeout/release, completion/panic/close, and release/drain/removal races |
| invocation boundary | rejected, canceled, timed-out, and closed execution tests with zero protected calls |
| exact release | admitted success, error, cancellation, and panic event/accounting tests |
| starvation | 64 lighter arrivals blocked behind a weighted FIFO head and then completed |
| eviction | concurrent remove winners, retained closed pointer, and replacement revision tests |
| orchestration | SIGTERM, readiness, incomplete drain, abrupt kill, scale-up/down, and mixed-revision model tests |
| composition | retry and circuit-breaker interoperability module with zero amplification or downstream classification |

Production code creates no goroutine. Waiters own one bounded timer and one
channel; all terminal paths stop the timer and remove the queue node. Goleak
wraps the test process.

Comparative benchmarks cover admitted and rejected fast paths, parallel
throughput, wait wake-up p50/p95/p99, FIFO violations, cancellation churn,
observers, partitions, and allocations. Fast-path candidates are Failsafe-Go,
direct `x/sync/semaphore`, and Fortify; advanced measurements remain specific
to this package where candidate semantics are not equivalent.

## Lock and callback audit

The bulkhead mutex is the only owner of policy state. The x/sync semaphore owns
permit counts. Clock calls and observer callbacks occur outside the mutex.
Channel close under the mutex wakes only package-owned waiter control flow.
No lock spans protected work, external I/O, observer code, clock code, or
blocking channel operations.

## Residual responsibility

The package cannot terminate an uncooperative callback, provide completion
after abrupt process kill, enforce cluster-wide capacity, choose retry or
fallback policy, or prevent a caller from abandoning a standalone permit.
Applications must size against maximum simultaneous pods and use bounded
transport contexts.
