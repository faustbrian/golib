# Sampling and tuning

Execution starts when a permit is granted, so local queue wait is never part of
the measured RTT. Go `time.Time` subtraction preserves the monotonic component
from `time.Now`; a backward or invalid injected clock produces zero duration
and increments `ClockErrors` instead of corrupting state.

The recent estimator retains at most `Capacity` durations in a ring. It sorts a
copy only when closing a window and uses the exact nearest-rank quantile over
those retained values. Memory is `O(Capacity)` and selection error within the
retained set is zero; when arrivals exceed capacity before the minimum duration,
the estimate represents the latest bounded sample set rather than all arrivals.

The baseline is a decaying minimum: lower recent quantiles replace it
immediately, while increases move by `BaselineSmoothing`. This permits workload
shifts without instantly treating every slower class as healthy. Bimodal work
should use separate limiters only when it consumes distinct constrained
resources; otherwise tune quantile and smoothing from representative traces.

Sparse windows older than `MaxDuration` are discarded until `MinSamples` can be
met. Idle time never changes the limit. All counters saturate rather than wrap,
and all algorithm floating state must remain finite.

Start with per-pod bounds derived from downstream capacity, use at least 20–50
samples per window, and prefer p90 for mixed latency. Increase smoothing only
when real capacity shifts faster than noise. Keep maximum increases small;
decreases may be larger but remain clamped to `MinLimit` so recovery probes
continue.
