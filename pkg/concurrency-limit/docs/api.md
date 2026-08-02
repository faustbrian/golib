# API and defaults

## Construction

`Config` requires `MinLimit >= 1`, `MaxLimit >= MinLimit`, an initial limit
inside those bounds, and an `Algorithm`. `NewDefaultAlgorithm` is available but
must still be selected explicitly so adoption cannot silently change existing
capacity policy.

Zero sampling values normalize to:

| Setting | Default |
| --- | ---: |
| Minimum window | 100 ms |
| Maximum sparse window | 1 s |
| Minimum samples | 20 |
| Retained samples | 256 |
| Recent quantile | p90 |
| Baseline smoothing | 0.1 |
| Maximum increase per update | 1 |
| Maximum decrease per update | clamped to `MinLimit` |
| Abandoned permit TTL | 5 minutes |

Queueing is disabled by default. Enabling it requires both a positive absolute
`MaxQueued` and `MaxWait`. Metadata priority defaults to the single value zero;
partitions must be predeclared.
Injected clocks and timers are treated as fault boundaries: constructor,
channel, and cleanup panics become bounded clock failures or are contained
after a terminal queue result.

## Admission and completion

`Acquire(ctx, optionalMetadata...)` returns a `Permit` or a stable sentinel
error. Supply at most one `Metadata`. `Permit.Complete` accepts exactly one
valid `Outcome`; concurrent or later duplicate completion returns
`ErrPermitCompleted`. An invalid outcome does not consume the permit.
The process-local permit identifier is monotonic and never wraps; after the
full `uint64` sequence is exhausted, admission returns
`ErrIdentifierExhausted` until a new limiter is constructed.

`Execute` and `ExecuteWithMetadata` acquire, invoke once, classify, and
complete. The default classifier maps a canceled context to ignored, a nil
error to success, and another error to dependency failure. Configure a
classifier to identify explicit overload. A classifier panic completes the
permit as ignored and returns `ErrClassifierPanic`; an operation panic records
dependency failure and re-panics the original value.

## Lifecycle and snapshots

`BeginDrain` rejects new calls and releases queued calls. `ReapExpired`
explicitly reclaims permits older than the TTL. `Reset` clears learned and
aggregate state, increments the generation, invalidates active and queued
permits, and restores the initial limit. Call reset only during a known
lifecycle boundary or after drain.
Reset and algorithm decision application are serialized, so a sampling window
from the previous lifecycle cannot overwrite the restored cold-start state.

`Snapshot` contains only bounded values: limit, in-flight, queue depth, sample
counts, baseline latency, rejections, outcomes, lifecycle generation, safety
counters, and algorithm diagnostics. No mutable collection is returned.
