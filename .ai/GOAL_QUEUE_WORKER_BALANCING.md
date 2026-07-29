# Goal: Provide Priority-Aware Per-Queue Worker Balancing

## Execution Contract

Execute this goal end to end in `/Users/brian/Developer/go-libraries` across
`pkg/queue` and `pkg/queue-control-plane`. Do not stop after adding configuration
types, reporting queue depth, scaling a Kubernetes Deployment, or demonstrating
one backend. Finish only when applications can express, enforce, observe, and
safely change different capacity and service policies for different logical
queues without starving important work or overstating fleet-wide guarantees.

This is a behavioral, public-contract, concurrency, operations, and migration
goal. Implementation MUST use test-driven development, preserve backend-specific
delivery semantics, update both module changelogs and affected documentation,
and satisfy the repository's final verification and review gates.

The implementation MUST retain the established ownership split:

- `queue` owns local worker concurrency, admission, delivery, settlement, and
  the scheduler that allocates local execution slots among logical queues;
- `queue-control-plane` owns authenticated and audited policy authoring,
  desired-state distribution, fleet visibility, and operator workflows;
- Kubernetes owns process and pod supervision;
- HPA or KEDA owns automatic pod replica reconciliation from exported metrics;
- brokers own transport durability, ordering, visibility, pending work, and
  backend-native quotas;
- applications own job classification, resource-cost declarations, downstream
  rate limits, handler idempotency, and bounded external operations.

The control plane MUST NOT poll brokers directly, run job handlers, become a
request-path dependency for ordinary delivery, or silently replace HPA/KEDA.
The queue library MUST NOT embed tenant authorization, Kubernetes clients, or a
global autoscaler.

## Problem Statement

The current libraries can set one fixed goroutine count on one `queue.Queue`
instance through `queue.WithWorkerCount`. One configured `core.Worker`
represents one backend and one logical queue. That model can create separate
instances manually, but it cannot express or enforce a shared worker group's
allocation policy across queues.

Today there is no first-class contract for:

- reserving different concurrency for `posti` and `ups`;
- placing a hard ceiling on one queue while allowing another to use more slots;
- distinguishing business priority from current backlog size;
- borrowing unused capacity without destroying guaranteed capacity;
- preventing a continuously busy low-priority queue from starving critical work;
- preventing a high-volume queue from consuming every worker;
- changing an allocation safely at runtime;
- explaining why a queue received its current allocation;
- separating per-process, per-pod, per-workload, and fleet-wide limits;
- coordinating local concurrency with HPA/KEDA replica changes;
- accounting for jobs with materially different CPU, memory, latency, or
  downstream-rate costs; or
- proving that an advertised allocation is actually enforced.

`queue-control-plane` currently reports concurrency and can issue an explicitly
authorized manual Kubernetes Deployment scale command. Its Horizon migration
matrix intentionally leaves balancing to HPA/KEDA plus an unspecified queue
concurrency policy. It does not author queue allocation policy, reconcile that
policy into workers, calculate desired replicas, or provide queue metrics and
tested HPA/KEDA resources.

This gap matters because jobs and queues are not interchangeable units. Traffic,
latency objectives, job duration, memory use, downstream API quotas, retry cost,
and business importance differ. A worker model that treats all pending jobs as
equal cannot preserve a service contract such as "Posti may use up to 100
concurrent executions while UPS may use up to 10, and urgent webhook work must
retain capacity even during a Posti backlog."

## Current Track Baseline To Re-Verify

Use `/Users/brian/Developer/track` as the primary migration and acceptance
fixture, but do not modify Track as part of this goal unless a later request
explicitly adds that repository to implementation scope.

The initial source audit found:

- `config/horizon.php` groups `default`, `webhook`, and `ups` under one
  auto-balanced supervisor with `maxProcesses = 25` in production;
- UPS indexing jobs are routed explicitly to the `ups` queue;
- non-UPS indexing jobs, including Posti, are routed to the least-loaded member
  of `index-1` through `index-5`;
- those index shards share a simple-balanced supervisor with
  `maxProcesses = 50`;
- `store-1` through `store-5` have `maxProcesses = 100`, but that is the event
  storage stage rather than a Posti-specific allocation; and
- the least-loaded picker spreads enqueue volume across shards but does not
  reserve execution capacity, express priority, or impose a carrier-wide
  concurrency ceiling.

Re-inspect the live Track branch and deployment configuration before using these
facts in a migration claim. Production topology is not established by local
configuration alone.

The required acceptance scenario is intentionally clearer than that baseline:

- a `posti` queue requests 100 execution slots;
- a `ups` queue requests 10 execution slots;
- critical webhook work retains a non-zero reservation;
- unused reserved capacity MAY be borrowed only according to explicit policy;
- borrowed capacity MUST be preempted only at safe admission boundaries;
- in-flight handlers MUST NOT be killed merely to rebalance slots;
- a UPS backlog MUST NOT grow its execution above 10 unless a reviewed policy
  explicitly makes the limit soft;
- an idle Posti queue MUST NOT strand capacity when borrowing is enabled; and
- every displayed desired, allocated, busy, borrowed, throttled, and unavailable
  value MUST identify whether it is local or fleet-wide.

The numbers 100 and 10 are an acceptance fixture, not universal defaults.

## Required Product Outcomes

### 1. Explicit Worker-Group Model

Add a public, versioned worker-group concept that composes multiple logical
queue runtimes under one local concurrency budget.

A worker group MUST have:

- a stable identity;
- a positive local slot budget;
- one or more uniquely named logical queue members;
- one synchronization owner for allocation and admission state;
- a documented lifecycle and shutdown contract;
- a stable policy revision;
- bounded configuration size;
- an explicit clock where time affects decisions;
- an observable current allocation; and
- no package-global registry or hidden goroutine.

Each logical queue member MUST retain its concrete backend worker. The group
MUST NOT introduce a universal broker interface that erases acknowledgement,
reclaim, ordering, connection, or dead-letter differences. A group MAY contain
different backend implementations only after lifecycle and shutdown behavior is
proven for that combination; otherwise mixed-backend groups MUST fail closed.

Existing single-queue construction MUST remain supported. Applications that do
not opt into worker groups MUST retain their current behavior and performance
within documented tolerances.

### 2. Queue Allocation Policy

Define a validated policy for every logical queue containing at least:

- `minimum`: slots reserved while the queue is eligible for work;
- `maximum`: hard local concurrency ceiling;
- `weight`: relative share of unreserved capacity among eligible peers;
- `priority`: business-service class used before weight for unreserved elastic
  capacity when contention exists; it MUST NOT displace another queue's
  non-borrowable minimum or defeat the documented starvation bound;
- `borrow`: whether unused reservation may be lent to peers;
- `preemptible_borrow`: whether future admissions may withdraw borrowed slots;
- `cost`: positive slot units consumed by one admitted job when known; and
- optional ramp-up and ramp-down bounds that limit allocation churn.

Names MAY change during design if the resulting semantics remain exact. The
public contract MUST distinguish:

- a hard safety cap from a scaling target;
- guaranteed reservation from best-effort weight;
- priority from FIFO ordering;
- a configured value from an observed or inferred value;
- local slots from pod replicas; and
- a per-member policy from a group-wide budget.

Validation MUST reject at least:

- duplicate queue identities;
- zero or negative group budgets;
- negative limits, weights, priorities, or costs;
- `minimum > maximum`;
- total non-borrowable minimum greater than the group budget;
- a queue maximum greater than a documented implementation bound;
- arithmetic overflow when costs, limits, queue counts, or replicas combine;
- unknown policy versions;
- empty policies;
- contradictory borrowing or preemption settings; and
- any policy whose enforcement scope is ambiguous.

Policy validation MUST complete before starting a backend, spawning a goroutine,
polling work, or changing live allocation.

### 3. Deterministic Local Allocator

Implement one deterministic allocator that converts a valid policy and a
bounded current snapshot into desired local queue allocations.

The allocator MUST:

1. preserve the group budget;
2. preserve every hard queue maximum;
3. allocate required non-borrowable reservations first;
4. preserve active critical reservations before lower-priority elastic work;
5. distribute remaining slots predictably by priority and weight;
6. make deterministic tie-breaking independent of map iteration order;
7. avoid starvation for every eligible queue whose policy permits service;
8. avoid allocating slots to a queue known to be paused, draining, terminating,
   incompatible, unavailable, or unable to report required state;
9. avoid oscillation when queue measurements fluctuate near a threshold;
10. distinguish an empty queue from an unmeasurable queue;
11. retain the last safe allocation when a fresh measurement is unavailable,
    subject to an explicit staleness limit;
12. fail closed when enforcing the requested policy would violate a hard cap;
13. expose a bounded reason for every allocation decision; and
14. run without network IO, callbacks, broker operations, or locks held across
    caller code.

The allocator MUST NOT assume that queue depth alone measures urgency. It MUST
support at least backlog count, oldest-job age or lag, busy slots, recent
throughput, and explicit priority as separately supported inputs. Unsupported
measurements MUST remain unsupported rather than becoming zero.

The initial algorithm SHOULD use a small auditable policy rather than predictive
or machine-learning scheduling. Any adaptive algorithm MUST have a deterministic
reference model and adversarial tests.

### 4. Safe Runtime Concurrency Changes

Make local concurrency safely adjustable without restarting a process.

Increasing a queue allocation MAY admit new deliveries immediately up to the
new limit. Decreasing an allocation MUST:

- stop new admissions above the new target;
- allow already admitted handlers to finish and settle;
- never kill a handler solely for balancing;
- never abandon an owned broker delivery;
- report `converging` while busy work remains above target;
- complete only when observed busy work is within the new allocation; and
- honor caller cancellation and bounded reconciliation deadlines without
  inventing success.

No resize path may close a backend, duplicate a consumer-group membership,
leak goroutines, reuse a closed channel, or race with pause, drain, terminate,
shutdown, or settlement.

The current fixed `WithWorkerCount` API MAY remain the static single-queue
shortcut. Any new mutable concurrency API MUST make ownership and synchronization
explicit and MUST define behavior before start, while running, while draining,
after termination, and during backend failure.

### 5. Admission And Fairness Semantics

Slot allocation and delivery admission MUST be separate observable concepts.
A queue allocated ten slots may have fewer than ten busy jobs; it MUST NOT be
reported as running ten jobs merely because capacity exists.

The group MUST define:

- when a queue becomes eligible;
- how an empty or temporarily unavailable queue yields capacity;
- how often allocation is reconsidered;
- what wakes the allocator;
- how polling is bounded when no backend provides notifications;
- how a returned `ErrNoTaskInQueue` affects only that queue;
- how a blocked `Request` call is canceled or isolated;
- how costly jobs consume more than one slot unit;
- how released jobs and delayed retries re-enter eligibility;
- how priority interacts with FIFO ordering inside one backend queue; and
- how fairness is measured over time rather than asserted from one decision.

Priority MUST NOT reorder messages inside a broker unless that backend already
owns such a contract. Prefer separate logical queues for materially different
service classes. The allocator chooses where the next safe admission capacity
is available; it does not rewrite backend ordering.

### 6. Resource Classes And Downstream Limits

Concurrency is not a sufficient proxy for resource usage. Add an extensible,
bounded resource-class model without turning the queue into a general cluster
scheduler.

At minimum, policy MUST be able to distinguish:

- ordinary IO-bound work;
- CPU-heavy work;
- memory-heavy work;
- long-running work;
- downstream-rate-limited work; and
- a custom application-defined class identifier.

The initial implementation MAY enforce only slot cost plus per-queue caps, but
the public model MUST NOT falsely imply equal resource cost. Documentation MUST
show how applications combine worker allocation with downstream rate limiters,
circuit breakers, timeouts, and Kubernetes resource requests/limits.

Do not place external API rate limiting inside the allocator. A Posti or UPS API
quota remains an application or dedicated rate-limiter responsibility even when
queue concurrency is capped.

### 7. Backend Capability Contract

Every managed backend MUST advertise the measurements and control operations it
can honestly support.

The balancing capability set MUST distinguish at least:

- queue identity;
- pending/depth availability;
- oldest-age or lag availability;
- notification or bounded polling support;
- cancellable receive support;
- dynamic local concurrency support;
- current busy count;
- reclaim or visibility behavior; and
- whether one backend client may safely serve more than one concurrent receive.

Do not enable balancing merely because a backend implements `core.Worker`.
Adapters that cannot cancel a blocking receive, report their identity, or
support safe concurrent requests MUST remain usable in their existing static
mode and MUST reject incompatible group configurations.

Redis Streams and Valkey Streams are the first required durable reference
backends. Prove their behavior with real services, pending entries, consumer
groups, reclaims, connection loss, and recovery. Do not infer NSQ, RabbitMQ,
Redis Pub/Sub, Core NATS, or Ring parity from stream tests. Add each backend only
with its own capability declaration and executable evidence.

### 8. Versioned Desired Allocation State

Extend the queue management protocol with a versioned desired allocation
record. It MUST be additive and rolling-upgrade safe.

The desired record MUST include:

- tenant;
- worker-group target;
- policy schema version;
- positive monotonic revision;
- complete bounded queue policies;
- authored time;
- originating command ID;
- actor and reason through the control-plane journal; and
- an explicit enforcement scope.

The worker MUST apply each revision at most once, reject rollback, reject two
different policies claiming one revision, and acknowledge the actual local
state after convergence. A failed read or application MUST preserve the last
successfully applied policy and ordinary queue delivery.

A missing desired record MUST mean "no authored change," not "use unlimited
capacity" and not "reset to defaults." Startup configuration remains the
fallback until a valid authored revision is applied.

Older workers MUST remain visible but receive no unsupported balancing command.
Newer workers outside the control plane's protocol range MUST also receive no
command. Capability intersection MUST control whether policy authoring is
enabled.

### 9. Local, Workload, And Fleet Scope

Every limit and observation MUST declare one of these scopes:

- `process`: one Go process;
- `pod`: all managed groups in one pod when explicitly coordinated;
- `workload`: one Kubernetes Deployment or equivalent workload;
- `fleet`: all compatible workers for one tenant and group; or
- `external`: a broker, downstream service, or platform limit not enforced by
  these libraries.

The first implementation MUST enforce `process` scope. It MAY expose workload
and fleet aggregates, but MUST NOT claim a global hard cap unless a durable,
partition-tolerant distributed concurrency mechanism is implemented and proven.

If ten replicas each enforce `ups.maximum = 10`, the honest fleet ceiling is up
to 100, not 10. Documentation, API models, CLI output, and UI labels MUST make
that multiplication visible.

Applications requiring a fleet-wide carrier cap MUST choose and document one
of:

- a dedicated workload with a calculable `replicas × local maximum` ceiling;
- an existing broker-native maximum-consumer contract;
- a downstream distributed rate/concurrency limiter; or
- a future separately specified distributed slot lease.

This goal MUST NOT quietly add a distributed semaphore to `queue`. If one is
needed, specify its lease, fencing, partition, renewal, expiry, fairness, and
side-effect semantics as a separate public contract and goal.

### 10. HPA And KEDA Integration

Keep pod scaling outside the control plane while making it usable.

Export bounded, low-cardinality metrics sufficient for HPA or KEDA to scale a
dedicated queue workload, including where supported:

- pending work;
- oldest work age or lag;
- allocated slots;
- busy slots;
- reservation utilization;
- borrowed slots;
- throttled admissions;
- allocation convergence;
- successful and failed policy applications; and
- stale or unsupported input state.

Metrics MUST NOT label tenant IDs, job IDs, payload values, actor names, error
text, or unbounded queue names. Queue identities used as labels MUST come from
a bounded deployment allowlist or be exported through per-target instruments
that avoid uncontrolled cardinality.

Provide reviewed HPA and KEDA examples for:

1. one dedicated workload per carrier queue;
2. one shared worker group with local weighted balancing; and
3. a hybrid model with reserved critical queues and an elastic shared pool.

Examples MUST show stable scale-up and scale-down windows, minimum and maximum
replicas, backlog and age thresholds, disruption behavior, and the resulting
fleet concurrency calculation. They MUST NOT present example thresholds as
universal production values.

The control plane MAY show or validate autoscaler ownership, but MUST NOT fight
an HPA/KEDA reconciliation loop. Manual scale remains an incident or maintenance
operation and MUST warn when an autoscaler can overwrite it.

### 11. Control-Plane API And Operator Workflow

Add tenant-scoped read and mutation surfaces for queue allocation policy.

Operators MUST be able to:

- inspect configured, desired, allocated, busy, borrowed, and effective
  capacity for every queue;
- see local versus aggregate scope;
- see unsupported or stale measurements;
- preview a policy change without applying it;
- submit a complete replacement policy with a unique idempotency key;
- inspect pending, applied, converging, rejected, timed-out, partial, and
  unknown outcomes;
- compare desired and acknowledged revisions;
- see the reason a queue is capped, idle, borrowing, or starved; and
- roll forward with a new revision after a bad policy.

Do not implement in-place rollback that rewrites history. A rollback is a new
audited desired revision containing the previous reviewed policy.

Authorization MUST be action, tenant, worker-group, and queue scoped. Reading
capacity MUST NOT imply permission to change it. Changing a policy that reduces
a non-borrowable critical reservation, raises a hard cap, or permits borrowing
MUST require explicit confirmation and a bounded reason.

The existing command journal MUST preserve idempotency and distinguish a lost
acknowledgement from a rejected or failed enforcement. A control-plane timeout
MUST NOT be treated as permission to issue a different idempotency key without
reconciliation.

### 12. Status And Protocol Models

Extend status models without fabricating data.

Worker-group status MUST include:

- group identity and enforcement scope;
- policy and applied revisions;
- group budget;
- per-queue minimum, maximum, weight, priority, cost, and borrow policy;
- desired allocation;
- current admitted and busy work;
- borrowed and lent slots;
- convergence state;
- last decision time and bounded reason code;
- backend identity and capability set;
- measurement support flags;
- staleness; and
- compatibility state.

All lists and strings MUST have explicit bounds. All numeric conversions MUST be
checked before conversion. Status validation MUST reject impossible totals such
as allocations beyond group budget, busy work beyond a hard cap unless
explicitly marked as converging after a decrease, or borrowed capacity with no
eligible lender.

Status pages MUST remain bounded and cursor-paginated. Historical allocation
charts belong in the telemetry platform, not an unbounded control-plane table.

### 13. Failure And Partition Semantics

Document and test at least these failures:

- policy source unavailable;
- policy revision conflict or rollback;
- malformed, oversized, or unknown-version policy;
- queue status unavailable or stale;
- one backend disconnected while peers remain healthy;
- one `Request` blocked during a downscale;
- allocator tick canceled;
- worker process crash during convergence;
- control-plane crash before and after durable command admission;
- acknowledgement lost after policy application;
- rolling deployment with old and new worker protocols;
- HPA scaling while a local policy is converging;
- queue paused, drained, resumed, or terminated during reallocation;
- handler panic or timeout while above a newly reduced target;
- unavailable queue becoming healthy after its capacity was borrowed;
- sustained high-priority backlog;
- sustained low-priority backlog with intermittent high-priority arrivals;
- all queues empty;
- all queues saturated;
- total minima equal to the group budget;
- borrower demand exceeding every maximum;
- clock movement where decision timing is relevant; and
- integer and duration boundaries at every public input.

A control-plane or policy-source partition MUST NOT stop ordinary work already
admitted under the last valid policy. A backend partition MUST affect only the
queues whose backend operation failed unless shared connection ownership makes
broader failure unavoidable and documented.

### 14. Shutdown And Lifecycle

The worker group MUST have one explicit lifecycle owner.

Shutdown MUST proceed in this order:

1. stop policy reconciliation;
2. stop new group admission;
3. cancel or join idle receive operations according to backend capability;
4. allow admitted handlers to finish within the caller's bounded context;
5. settle every owned delivery;
6. release backend workers exactly once; and
7. join every owned goroutine.

Pause and drain MAY target one queue or the complete group. A group drain MUST
not be reported complete while a member still owns admitted work. A per-queue
pause MUST make its capacity available only when the policy permits borrowing.

No lock may be held across a backend request, handler callback, settlement,
network IO, channel send/receive that may block, or unbounded allocation pass.

## Required Acceptance Scenarios

### Scenario A: Track-Like Static Isolation

Given a local group budget of at least 112 slots:

- `webhook`: minimum 2, maximum 2, highest priority, non-borrowable;
- `posti`: minimum 80, maximum 100, high weight, borrowable;
- `ups`: minimum 5, maximum 10, lower weight, borrowable.

Prove:

- `posti` can reach 100 but never 101 local concurrent handlers;
- `ups` can reach 10 but never 11;
- webhook work can start without waiting behind either carrier;
- unused Posti or UPS reservations are available according to policy;
- returning demand reclaims borrowed capacity only through new-admission
  decisions; and
- no in-flight delivery is canceled or duplicated by rebalancing.

If the group budget cannot simultaneously satisfy the desired maxima, status
must show the shortfall and the deterministic allocation reason.

### Scenario B: Priority Under Flood

Hold a continuously replenished low-priority queue above its maximum useful
backlog while periodically enqueueing critical jobs. Prove a bounded critical
start delay under the declared local capacity and backend polling assumptions.

Do not claim a latency service level from queue scheduling alone. Measure and
state the contribution of poll interval, handler occupancy, scale-up delay,
broker delivery, and downstream latency.

### Scenario C: Safe Downscale

Start work at the old maximum, author a lower maximum, and prove that:

- the policy becomes `converging`;
- new admissions stop above the target;
- existing jobs complete and settle;
- the applied revision is acknowledged only with honest actual state; and
- cancellation of the operator request yields an inconclusive command result
  without rolling back work already enforced.

### Scenario D: Fleet Multiplication

Run at least two worker replicas with a per-process UPS maximum of 10. Prove
that fleet status reports the possible aggregate as 20, not 10, and that a pod
scale event updates the aggregate without changing the local policy revision.

### Scenario E: Rolling Compatibility

Run old, current, and newer protocol workers together. Prove that only
compatible workers receive allocation policy, all remain visible, and aggregate
capacity excludes unsupported enforcement from guaranteed-capacity claims.

### Scenario F: Backend Outage

Remove one durable backend while another queue continues. Prove bounded error
handling, no hot spin, no capacity fabrication, no leaked receive operation,
honest stale/unavailable status, and recovery when the backend returns.

## Testing Requirements

### Unit And Model Tests

Use deterministic table, property, and state-machine tests for:

- policy validation;
- allocation conservation;
- minimum and maximum enforcement;
- weighted distribution;
- priority and starvation bounds;
- deterministic tie-breaking;
- borrowing and return of capacity;
- cost-unit accounting;
- hysteresis and ramp limits;
- status validation;
- revision monotonicity; and
- overflow and maximum-size inputs.

For every valid allocation, independently verify at least these invariants:

```text
sum(allocation) <= group budget
minimum <= allocation <= maximum while the queue is eligible and not lending;
an idle queue may yield only the reservation its policy marks lendable
busy <= maximum, except an explicit converging downscale snapshot
borrowed <= lendable idle reservation
no unsupported measurement influences a decision as if it were zero
```

Use a structurally independent reference allocator in tests. Do not duplicate
the production algorithm line for line.

### Concurrency Tests

Use deterministic coordination, never timing sleeps, to prove:

- concurrent enqueue, allocation, pause, resize, drain, and shutdown;
- no admission beyond a hard cap;
- no slot leak after handler panic or settlement failure;
- no double release;
- no deadlock during downscale;
- no goroutine leak from blocked or canceled receive;
- monotonic revision application; and
- race-free status snapshots.

All affected packages MUST pass `go test -race` and targeted stress tests.

### Backend Integration Tests

Use real Redis Streams and Valkey Streams services to prove:

- distinct named queues in one group;
- actual concurrent-handler maxima;
- idle queue borrowing;
- pending reclaim during worker loss;
- connection failure isolation;
- downscale with in-flight deliveries;
- settlement after reallocation; and
- restart convergence from the last durable desired policy.

Add backend-specific tests before claiming support for any additional backend.

### Control-Plane Integration Tests

Use real PostgreSQL and authenticated queue management endpoints to prove:

- authorized policy preview and submission;
- denied cross-tenant and cross-group operations;
- atomic command, audit, and desired-policy persistence;
- idempotent duplicate submission;
- changed-content idempotency conflict;
- applied, converging, rejected, timed-out, partial, and unknown outcomes;
- rolling protocol negotiation; and
- restoration of commands, allocation desired state, audit chain, and revision
  acknowledgement after backup and restore.

### Kubernetes And Autoscaler Tests

Validate all HPA/KEDA resources structurally and, where a disposable cluster is
available, prove:

- scale-up from supported backlog or lag metrics;
- stable scale-down;
- per-replica local limits remain unchanged;
- aggregate capacity updates with ready replicas;
- manual scale conflict is surfaced; and
- workload drain occurs before termination where the platform permits it.

Registration-only assertions are insufficient. Tests MUST exercise runtime
allocation behavior or validate deployable resource contracts.

### Fuzz And Mutation Tests

Fuzz policy decoding, validation, desired-state transport, status decoding, and
allocator inputs with bounded corpora. Every discovered failure MUST receive a
deterministic regression case.

Mutation testing MUST kill viable changes to maxima, minima, priorities,
weights, borrowing, revision comparisons, scope checks, confirmation checks,
capability negotiation, and status support flags.

## Performance Requirements

Publish reproducible benchmarks for:

- allocation decisions at 2, 10, 100, and the maximum supported queue count;
- status snapshots at maximum queue and worker counts;
- idle and saturated admission;
- rapid but bounded policy changes;
- 10,000-worker fleet aggregation;
- backend outage handling; and
- comparison against an equivalent fixed single-queue configuration.

Report latency, operations per second, bytes, allocations, CPU profile, queue
count, group budget, backend, Go version, hardware, and statistical method.

Declare reviewed allocator latency and allocation budgets before implementation,
then enforce them in the benchmark gate. The allocator MUST remain bounded by
configured queue count and MUST NOT create one goroutine per decision. A
single-queue application that does not enable balancing MUST remain within its
predeclared latency and allocation regression budgets.

## Security And Privacy Requirements

- Authenticate and authorize every control-plane read and mutation.
- Scope policy by tenant and exact worker group.
- Require confirmation for materially risk-increasing changes.
- Bound queue names, group names, reasons, policy members, numeric values,
  request bodies, pages, and response bodies.
- Keep credentials, endpoints, payloads, error text, and job parameters out of
  policy, status, metrics, traces, audit hashes, and generated examples.
- Reject unknown JSON fields and trailing input at administrative boundaries.
- Keep worker management tokens separate from operator credentials.
- Do not allow a policy to name an unconfigured backend target or escape a
  tenant's workload mapping.
- Audit privileged reads and every policy mutation before dispatch.
- Preserve explicit partial and unknown outcomes; never retry a mutation merely
  because its response was lost.

## Documentation And Migration Deliverables

Update at least:

- `pkg/queue/README.md`;
- `pkg/queue/docs/architecture.md`;
- `pkg/queue/docs/api.md`;
- `pkg/queue/docs/backend-support.md`;
- `pkg/queue/docs/delivery-semantics.md`;
- `pkg/queue/docs/lifecycle.md`;
- `pkg/queue/docs/management.md`;
- `pkg/queue/docs/performance.md`;
- `pkg/queue/docs/horizon-migration.md`;
- `pkg/queue/CHANGELOG.md`;
- `pkg/queue-control-plane/README.md`;
- `pkg/queue-control-plane/docs/architecture.md`;
- `pkg/queue-control-plane/docs/api.md`;
- `pkg/queue-control-plane/docs/compatibility.md`;
- `pkg/queue-control-plane/docs/deployment.md`;
- `pkg/queue-control-plane/docs/horizon-migration.md`;
- `pkg/queue-control-plane/docs/kubernetes.md`;
- `pkg/queue-control-plane/docs/operations.md`;
- `pkg/queue-control-plane/docs/security.md`;
- `pkg/queue-control-plane/docs/ui.md`;
- `pkg/queue-control-plane/CHANGELOG.md`; and
- root manifests and generated documentation affected by new public packages,
  goals, commands, or capabilities.

Provide a Horizon migration guide that maps:

- one Horizon supervisor to one worker group or dedicated workload;
- `maxProcesses` to an explicitly scoped local maximum or calculated fleet
  capacity;
- queue lists to member policies;
- `balance=auto` to the supported priority/weight allocator;
- separate supervisors to hard isolation boundaries;
- `balanceMaxShift` and `balanceCooldown` to bounded ramp and hysteresis;
- Horizon queue metrics to supported queue status and telemetry; and
- process counts to Go concurrency only after resource and handler behavior are
  benchmarked.

The migration guide MUST warn that one PHP process is not automatically
equivalent to one Go goroutine. Capacity values require measurement of CPU,
memory, handler latency, downstream limits, and failure behavior.

Include a Track-specific worked example showing how the acceptance policy could
represent Posti, UPS, and critical webhook workloads, while clearly labeling it
as a proposed Go deployment rather than current Track production state.

## Rollout Requirements

Roll out in independently reversible phases:

1. **Observe only**: publish queue identity, busy concurrency, supported depth
   and lag, and worker-group compatibility without changing allocation.
2. **Static groups**: enforce explicit fixed minima/maxima matching existing
   deployment isolation; borrowing disabled.
3. **Shadow allocation**: calculate desired dynamic allocations and compare them
   with actual fixed behavior without enforcement.
4. **Bounded borrowing**: enable lending for one low-risk group with hard maxima
   and rollback revision prepared.
5. **Priority enforcement**: enable critical reservations and prove starvation
   and latency behavior under load.
6. **Autoscaler integration**: connect reviewed HPA/KEDA resources only after
   metrics, scale math, and disruption behavior are proven.
7. **Broader backend adoption**: enable each additional backend only after its
   capability and failure suite passes.

Every phase MUST define entry criteria, rollback trigger, rollback action,
operator owner, observation window, and success measurements. Rollback MUST be
a new desired revision or deployment rollback; do not mutate audit history.

During Horizon coexistence, Horizon and Go workers MUST NOT consume the same
queue unless wire format, claim, retry, acknowledgement, visibility timeout,
and duplicate behavior are explicitly proven compatible. Prefer separate queue
ownership and shadow traffic.

## Non-Goals

This goal does not authorize:

- implementing a general workflow or DAG engine;
- adding Kafka as a task queue merely to obtain balancing;
- replacing broker-native ordering or delivery semantics;
- implementing exactly-once job execution;
- killing in-flight jobs to satisfy a new allocation immediately;
- placing application credentials or payload inspection in the allocator;
- embedding HPA, KEDA, or Kubernetes reconciliation inside the control plane;
- a cluster-wide Kubernetes operator;
- automatic production tuning without reviewed limits;
- a distributed fleet semaphore without a separate specification;
- modifying or deploying Track;
- deleting Horizon queues or failed-job history; or
- claiming that equal concurrency means equal throughput or resource cost.

## Required Verification

During development, run the narrowest affected package and integration gates.
Before completion, run at least:

```sh
make inventory
make check MODULES=pkg/queue
make check MODULES=pkg/queue-control-plane
make ci-changed BASE=<verified-base-revision>
```

Also run every newly introduced real-backend, PostgreSQL, browser, Kubernetes,
autoscaler, stress, fuzz, mutation, benchmark, and disaster-recovery gate. Reuse
valid expensive evidence only when the complete input fingerprint is proven
unchanged.

Verification MUST report exact commands and outcomes, distinguish focused from
aggregate proof, and name every unavailable external boundary. A passing unit
allocator test is not evidence of backend enforcement, fleet-wide caps,
autoscaling, or a successful Track migration.

## Completion Criteria

This goal is complete only when:

- applications can declare materially different queue minima, maxima,
  priorities, weights, borrowing, and job costs;
- the queue data plane enforces those policies at safe admission boundaries;
- hard local caps and the group budget are never exceeded;
- lowering capacity drains naturally without killing or abandoning work;
- starvation and borrowing behavior are proven deterministically;
- Redis Streams and Valkey Streams pass real-backend balancing, failure,
  reclaim, and restart tests;
- every concurrency-sensitive package passes race, stress, and leak checks;
- the control plane authors, audits, distributes, and reports versioned desired
  policies with explicit outcomes;
- old, current, and newer workers remain safe during rolling upgrades;
- local, pod, workload, and fleet scopes are never conflated;
- HPA/KEDA examples and metrics are deployable and do not create competing
  reconciliation ownership;
- the Track-like Posti 100 / UPS 10 / critical webhook acceptance scenarios
  pass with honest scope labels;
- all affected public documentation, API baselines, changelogs, manifests, and
  release notes are current;
- the complete final diff has no unresolved review finding; and
- all affected release gates pass against the final input fingerprints without
  unexplained skips, warnings, stale evidence, or unsupported claims.
