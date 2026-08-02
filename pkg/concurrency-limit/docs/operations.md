# Operations and dashboards

Export snapshots without caller-controlled labels:

- current limit, in-flight work, queue depth, and draining generation;
- total eligible samples, recent sample count, and baseline latency;
- immediate rejection, queue timeout, and expired-permit totals;
- each terminal outcome total;
- observer, classifier, algorithm, and clock safety counters; and
- algorithm reason, gradient, queue estimate, and achieved throughput.

Recommended dashboards overlay demand, admitted throughput, current limit,
in-flight, queue depth, rejection ratio, execution p50/p90/p99, downstream
errors, pod replicas, CPU, and memory. Alert on sustained saturation plus
rejection, growing queue, no recovery after downstream latency normalizes,
expired permits, nonzero safety counters, and limit divergence across rollout
versions.

For an overload incident, first determine whether latency rises before errors,
whether achieved throughput plateaus as in-flight grows, and whether rejection
is local or downstream. Lower maximums only with downstream capacity evidence.
Do not hide a retry storm with a larger queue. If the algorithm oscillates,
increase samples or baseline age before widening step sizes.

The package creates no goroutine, timer outside a queued call, log, trace,
metric backend, or storage. Observers run synchronously outside the state lock;
keep them bounded and nonblocking. Their panic cannot break admission but is
counted.
