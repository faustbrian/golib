# Delay selection

A fixed delay repeats between launches. A schedule supplies one positive delay
for every hedge. A dynamic function receives only the hedge ordinal and the
previous delay; percentile storage remains caller-owned and bounded.

The Tail at Scale suggests targeting the long tail, commonly a current p95 or
p99, instead of hedging normal requests. Measure by resource and workload;
cold or mixed-rollout data can be misleading. Zero or negative dynamic delays
are terminal policy failures. The total timeout bounds schedules whose next
timer falls beyond the remaining operation lifetime.
