# Resilience architecture

`golib` keeps resilience policies small and independently adoptable. Each
module owns one decision boundary; applications own the explicit order in
which those boundaries compose. There is no default stack, global registry,
service locator, or automatic policy discovery.

Use [`resilience`](../pkg/resilience) only when multiple focused policies need
one outcome vocabulary, deterministic composition, or a shared retry-and-hedge
work budget. A package can otherwise be used directly.

## Package selection

| Need | Package | It owns | It does not own |
| --- | --- | --- | --- |
| Repeat completed work | [`retry`](../pkg/retry) | finite sequential attempts, backoff, attempt classification | concurrency isolation, circuit state, replay safety |
| Stop calls to an unhealthy dependency | [`circuit-breaker`](../pkg/circuit-breaker) | dependency-health admission state | retry, timeout, fallback, bulkhead capacity |
| Enforce work per time or quota | [`rate-limit`](../pkg/rate-limit) | rate or quota admission, including explicit distributed backends | in-flight concurrency |
| Reuse a prior result | [`cache`](../pkg/cache) | freshness, stale eligibility, coalesced loading, backend records | generic fallback selection |
| Count weighted permits | [`semaphore`](../pkg/semaphore) | owned weighted permits | work queues, dependency health |
| Isolate a finite resource | [`bulkhead`](../pkg/bulkhead) | fixed concurrency, bounded waiting, partitions | adaptive capacity |
| Duplicate slow in-flight work | [`hedge`](../pkg/hedge) | finite concurrent attempts after explicit delays | sequential retry, replay safety |
| Learn safe in-flight capacity | [`concurrency-limit`](../pkg/concurrency-limit) | adaptive process-local admission | time-based quotas |
| Shed likely-overload work | [`adaptive-throttle`](../pkg/adaptive-throttle) | probabilistic admission from recent outcomes | fixed quotas, circuit state |
| Prove failure behavior | [`fault-injection`](../pkg/fault-injection) | deterministic test faults | production orchestration |
| Compose policies and bound extra work | [`resilience`](../pkg/resilience) | scopes, outcomes, ordering, observation, retry/hedge budget | focused algorithms |

Do not create a generic timeout wrapper around `context.WithTimeout`. The
caller owns the logical-operation deadline; the owning adapter or policy owns
queue, admission, attempt, transport, and shutdown deadlines. Cancellation is
cooperative and does not prove that ignored work or an external side effect
stopped.

Do not create a generic fallback runner. Serving stale data, selecting another
vendor, returning a default, or degrading functionality is application policy
with domain-specific authorization, freshness, and financial consequences.

## Outcome taxonomy

Composed policies distinguish these outcomes:

| Outcome | Meaning | May teach dependency-health policies? |
| --- | --- | --- |
| Success | Invoked downstream work completed successfully | yes |
| Operation failure | Invoked downstream work failed | when the application classifier says so |
| Local rejection | A local policy denied admission before downstream work | no |
| Cancellation | Caller cancellation ended cooperative work or waiting | only under explicit application policy |
| Deadline | A known deadline expired | only when its scope proves a downstream failure |
| Ignored | The result is intentionally excluded from policy learning | no |
| Policy failure | A policy, observer, clock, or other mechanism failed | no |

Errors preserve `errors.Is` and `errors.As`. Observations use bounded policy,
stage, reason, and scope identifiers. Raw URLs, credentials, tenant IDs,
arbitrary errors, and attacker-controlled labels must not become metric labels
or retained event metadata.

## Composition rules

Policy order changes observable behavior. Constructors and application code
must show the order rather than selecting it from environment configuration.
With policies declared outer-to-inner, a typical outbound call is:

```text
caller deadline
  cache lookup
    logical retry or hedge
      rate or quota admission
        adaptive throttle
          circuit-breaker admission
            bulkhead or adaptive-concurrency admission
              attempt deadline
                transport call
```

This is an example, not a universal preset. Decide each boundary explicitly:

- Put cache first only when an eligible cached result should avoid all
  downstream admission and health policies.
- Acquire logical-operation quotas outside retry when one caller operation
  consumes one unit. Acquire attempt quotas inside retry only when every
  physical attempt must consume a unit.
- Put a circuit breaker inside retry when it must observe each physical
  downstream attempt. Put it outside only when the final logical result is the
  dependency-health signal.
- Treat local breaker, throttle, rate, bulkhead, and adaptive-limit rejection
  as permanent for retry unless an explicit bounded application policy says
  otherwise.
- Make every hedge attempt acquire its own downstream concurrency permit.
  Sharing one permit hides the actual amplification.
- Keep fallback outside policies whose state must not learn from the fallback.
- Do not sample local rejection as downstream overload in an adaptive
  algorithm unless that algorithm explicitly owns the rejected boundary.

### Retry timeline

```text
logical call starts
  attempt 1 acquires attempt policies -> downstream failure -> releases
  backoff waits within caller deadline and shared budget
  attempt 2 acquires attempt policies -> success -> releases
logical call completes once
```

Queue wait and backoff consume the caller's total deadline. An attempt deadline
may be shorter, but no child context can extend the caller deadline.

### Hedge timeline

```text
logical call starts
  attempt 1 acquires its own permits ---------------------> canceled loser
  hedge delay
  attempt 2 draws shared budget and acquires permits -> success winner
logical call settles winner, accounts both attempts, drains bounded cleanup
```

The operation must be replay-safe before hedging. Cancellation of a losing
attempt is cooperative and does not retract an already committed side effect.

### Retry plus hedge

Retry and hedge must draw from one
[`resilience.WorkBudget`](../pkg/resilience/docs/budgets.md). Separate budgets
can multiply work. If a logical operation allows `R` retries and `H` hedges per
retry, the naive upper bound is:

```text
physical attempts per logical call = (1 + R) * (1 + H)
```

With fan-out `F`, redirect or pagination factor `P`, and `N` replicas, the
fleet-level upper bound for `L` incoming logical calls per replica becomes:

```text
downstream calls = L * N * F * P * (1 + R) * (1 + H)
```

A shared additional-work budget replaces the uncoordinated retry/hedge product
with one reviewed finite ceiling. Redirects, pagination, and fan-out still need
their own bounds; the resilience budget does not infer them.

## Kubernetes semantics

Unless a package explicitly documents a distributed backend, its memory state
is process-local and therefore pod-local. A configured capacity `C` across
`N` simultaneously serving replicas permits up to `C * N` aggregate local
admissions. Uneven routing can saturate one pod while fleet capacity remains.

Every stateful-policy deployment must account for:

- cold pods beginning without breaker, limiter, throttle, cache, or budget
  history;
- mixed revisions temporarily applying different policy values during a
  rolling update;
- scale-out multiplying local capacity before traffic and observations settle;
- abrupt pod loss discarding local accounting without undoing side effects;
- SIGTERM closing new admission before canceling waits, draining accepted
  work, releasing permits, stopping timers, and closing observers; and
- configuration rollouts temporarily doubling effective capacity while old
  and new replicas overlap.

Use a distributed backend only when the invariant is truly cluster-wide.
`rate-limit` can provide explicit backend semantics; in-memory breakers,
bulkheads, semaphores, adaptive limiters, throttlers, and budgets must not
simulate distributed truth through best-effort pod communication.

### Health and autoscaling

Dependency failure or an open breaker must not fail process liveness. During
drain, readiness may stop new traffic. Report dependency degradation
separately so Kubernetes does not restart healthy processes and amplify an
outage.

Safe operational views combine admitted, rejected, delayed, in-flight,
executed, latency, queue depth, and downstream load. CPU or latency alone can
create an unsafe HPA loop: early rejection lowers local work and latency, which
can suppress scale-out while demand remains high. Missing metric series must
have an explicit meaning and labels must remain low-cardinality.

## HTTP clients

[`http-client`](../pkg/http-client) owns reviewed outbound policy profiles, but
profiles remain visible and overridable. They must not retry a non-replayable
body, hedge non-idempotent work, extend a caller deadline, classify local
admission as a remote failure, or hide whether a permit applies once per
logical call or once per attempt.

Before adopting a profile, decide:

1. Which methods and bodies are replay-safe.
2. Which response and transport failures are retryable.
3. Whether `Retry-After` is authoritative and still fits the total deadline.
4. Whether redirects and pagination share the same operation budget.
5. Whether the breaker observes attempts or final logical results.
6. Whether cache and fallback results should bypass downstream policies.
7. Which local or distributed capacity contract the deployment requires.

## Migration

1. Inventory existing retries, timeouts, caches, concurrency controls,
   fallbacks, client middleware, and infrastructure-level retries.
2. Classify each as logical-operation, physical-attempt, process-local, or
   distributed behavior.
3. Remove duplicate owners before adding composition. In particular, disable
   hidden transport or proxy retries that would multiply explicit attempts.
4. Introduce typed outcome classification and caller-owned deadlines first.
5. Add the smallest focused policy, then verify failure, cancellation,
   shutdown, and scale behavior before adding another.
6. Enable one shared retry-and-hedge budget before combining those policies.
7. Roll out by dependency or operation class while comparing admitted work,
   downstream calls, tail latency, errors, and resource use.

## Anti-patterns

- A universal `Execute` helper that silently installs retry, timeout, breaker,
  cache, and fallback policies.
- Retrying every error or HTTP status without replay and commitment analysis.
- Treating local saturation as a downstream failure.
- Sharing one concurrency permit across hedge attempts.
- Giving retry and hedge independent amplification budgets.
- Using liveness to report a dependency outage.
- Claiming process-local capacity is cluster-wide.
- Racing an uncooperative operation against a timer and reporting it stopped.
- Running observers while policy locks are held.
- Using tenant IDs, URLs, raw errors, or credentials as metric labels.
- Letting HPA scale only from post-rejection CPU or latency.

## Package documentation

The focused modules document their exact public contracts and evidence:

- [`resilience` composition](../pkg/resilience/docs/composition.md),
  [budgets](../pkg/resilience/docs/budgets.md), and
  [Kubernetes semantics](../pkg/resilience/docs/kubernetes.md)
- [`retry`](../pkg/retry), [`hedge`](../pkg/hedge), and
  [`circuit-breaker`](../pkg/circuit-breaker)
- [`semaphore`](../pkg/semaphore), [`bulkhead`](../pkg/bulkhead), and
  [`concurrency-limit`](../pkg/concurrency-limit)
- [`rate-limit`](../pkg/rate-limit),
  [`adaptive-throttle`](../pkg/adaptive-throttle), and
  [`cache`](../pkg/cache)
- [`http-client`](../pkg/http-client) and
  [`fault-injection`](../pkg/fault-injection)

Benchmark claims are valid only for equivalent policy semantics and published
environments. Lower latency obtained by omitting accounting, ownership,
resource bounds, or cancellation behavior is not an equivalent comparison.
