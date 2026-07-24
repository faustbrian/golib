# Policies, truth tables, and classification

## Closed-state opening

Every enabled rule is evaluated only after classified outcomes reach minimum
throughput. An ignored outcome is not classified throughput.

| Rule | Opens when |
| --- | --- |
| Consecutive failures `N` | current failure streak is at least `N` |
| Failure count `N` | retained failures are at least `N` |
| Failure ratio `R` | retained failures / classified outcomes is at least `R` |
| Slow count `N` | retained slow successes plus slow failures are at least `N` |
| Slow ratio `R` | retained slow classified outcomes / classified outcomes is at least `R` |

`OpenWhenAny` opens if any enabled row is true. `OpenWhenAll` opens only if all
enabled rows are true. Rule order does not affect the result; the transition
reason is the stable `policy-opened` reason.

## Consecutive outcomes

| Outcome | Default streak effect | Reset option |
| --- | --- | --- |
| Success | reset | reset |
| Failure | increment | increment |
| Ignored | preserve | reset |
| Slow success | reset | reset |
| Slow failure | increment | increment |

Slow is orthogonal to success/failure. Slow ignored outcomes are never counted
as slow dependency calls.

## Half-open recovery

| Threshold | Success | Failure |
| --- | --- | --- |
| Required successes | close when reached | immediate or after sample |
| Success ratio | evaluate after `MaxProbes` classified completions | immediate or evaluate after sample |
| Ignored | release active slot; do not advance sample | same |

At most `MaxProbes` are active or classified in one half-open generation.
Canceled/expired probes release active capacity and do not advance the sample.

## Classification guide

Classify dependency health, not merely whether the caller obtained its desired
result. Recommended starting points:

| Completion | Typical outcome |
| --- | --- |
| Successful dependency response | success |
| Dependency transport/server failure | failure |
| Caller canceled before admission | no permit and no outcome |
| Caller cancellation after admission | explicit caller policy |
| Local validation/cache hit/rate limit/bulkhead rejection | ignored/no admission |
| HTTP 4xx | protocol/business policy, often ignored or success |
| HTTP 5xx | protocol policy, often failure |
| Queue locally full | ignored |
| Queue broker unavailable | failure |

Use `errors.Is` for wrapped and joined sentinel errors. Be deliberate with typed
nil errors: Go interfaces containing a typed nil are non-nil and therefore fail
under the default classifier. Use `Completion.Context` to distinguish caller
cancellation from a dependency-produced cancellation error. Never retain
`Completion.Context`, `Completion.Result`, or `Completion.Err` in a classifier.

### Classification truth table

| Completion/classifier result | Recorded health | Consecutive failures | Slow dimensions |
| --- | --- | --- | --- |
| default classifier, `Err == nil` | success | reset | slow success when elapsed meets threshold |
| default classifier, non-nil or typed-nil error interface | failure | increment | slow failure when elapsed meets threshold |
| custom `OutcomeSuccess` | success | reset | optional slow success |
| custom `OutcomeFailure` | failure | increment | optional slow failure |
| custom `OutcomeIgnored` | ignored only | preserve or configured reset | never slow |
| custom invalid enum | no record; typed error | unchanged | none |
| protected-operation panic | failure, then same-value re-panic | increment | elapsed panic duration applies |
| classifier panic | failure, then same-value re-panic | increment | elapsed operation duration applies |

Operation results and errors are returned exactly as supplied. Classification
changes breaker health only; it does not wrap or replace the operation error.

## Count versus time windows

Use a count window when request volume is stable and “last N calls” is the right
signal. Use a time window when low/high traffic periods should represent the
same wall-time horizon. Count memory is `O(Size)`; time memory is
`O(BucketCount)`, independent of request volume and process lifetime.

### Window truth table

| Window/input | Retained result |
| --- | --- |
| count, classified insertion below `Size` | append classified outcome |
| count, classified insertion at `Size + 1` | evict oldest classified outcome, append newest |
| count, ignored insertion | increment ignored total; do not consume classified capacity |
| time, first insertion in bucket | initialize that bucket and add outcome |
| time, repeated insertion in bucket | aggregate into the same bucket |
| time, forward jump below full horizon | retain buckets from current minus `BucketCount - 1` through current |
| time, exact oldest-boundary advance | expire the bucket immediately before the inclusive oldest bucket |
| time, idle jump of at least the full horizon | prior buckets are absent from the snapshot without per-gap iteration |
| time, backward timestamp | clamp to latest observed bucket; expired data cannot reappear |
| time, pre-epoch fractional timestamp | select bucket using mathematical floor division |
| time, timestamp outside `UnixNano` range | preserve the full signed bucket identity without aliasing |

Ignored time-window outcomes live in their timestamp bucket and expire with it.
All count/time storage is fixed at construction and invalid classes fail without
mutating the window.

## Timing

The system clock preserves Go's monotonic component during in-process elapsed
time. Serialized timestamps are wall-clock observations and should not be used
to reconstruct duration. A backward injected-clock movement clamps execution
elapsed time to zero. Jitter only shortens the selected open interval and never
exceeds the configured schedule.

Time-window bucket selection uses mathematical floor division across the Unix
epoch. Timestamps outside `time.Time.UnixNano`'s representable range use wide
integer arithmetic that preserves the full signed quotient and timestamp
ordering. Backward bucket movement does not resurrect expired data.

### Timing truth table

| Boundary | Result |
| --- | --- |
| open admission before `NextProbeAt` | reject with `ErrOpen` |
| open admission exactly at `NextProbeAt` | enter half-open once and apply probe capacity |
| permit completion/cancel before TTL deadline | accept terminal action |
| permit completion/cancel exactly at TTL deadline | expire and return `ErrPermitExpired` |
| waiting capacity freed before absolute `MaxWait` | retry admission |
| observed time reaches absolute `MaxWait` | reject with `ErrHalfOpenWaitTimeout`, even if timer delivery lags |
| execution clock moves backward | elapsed duration is zero |
| jitter sample in `[0, 1)` | shorten duration by at most configured jitter |
| jitter panic, non-finite, or out-of-range sample | use unjittered duration |
| injected clock/timer/random callback | execute before or after, never while holding the state mutex |
