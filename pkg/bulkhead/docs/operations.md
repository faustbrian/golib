# Operations

## Metrics

Export snapshots or events with bounded labels:

- `resource` and `policy_revision`;
- capacity, active and available weight;
- queue depth;
- admissions and executions;
- rejections partitioned by stable reason;
- caller cancellations;
- wait and execution duration as separate histograms;
- draining and drained state.

Do not label with raw URLs, tenant IDs, arbitrary context values, error text,
credentials, or unbounded partition keys. Observer errors are deliberately not
fed back into admission.

## Alerts

Useful symptoms include sustained capacity rejection, queue saturation,
high-percentile wait approaching `MaxWait`, active weight pinned at capacity,
and drain duration approaching the termination deadline. A single rejection
is expected load shedding, not necessarily an incident.

Correlate with connection-pool use, downstream latency/error rate, replica
count, rollout surge, request fan-out, and caller deadline exhaustion.

## Incident runbook

1. identify the resource and policy revision;
2. verify active weight, queue depth, and rejection reason;
3. compare per-pod saturation and traffic skew;
4. confirm downstream and pool capacity before raising the limit;
5. stop retry or hedge amplification before adding queue depth;
6. preserve liveness while deciding readiness and traffic routing separately;
7. drain old revisions before concluding aggregate capacity changed.

Increasing queue depth does not create capacity and can convert rejection into
latency and memory retention. Increasing per-pod capacity can exceed the
downstream fleet limit after HPA or rollout surge.

## Drain runbook

Stop traffic first, close all bulkheads, cancel application request roots, and
drain under the pod deadline. `ErrDrainIncomplete` means live admitted work was
not reclaimed. Record it as ambiguous rather than successful cancellation.
