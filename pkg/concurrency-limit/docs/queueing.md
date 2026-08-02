# Queueing, metadata, and fairness

Immediate rejection is the default and simplest mode. Optional queueing is
strict global FIFO with a validated absolute count and wait bound. Context
cancellation, reset, drain, timeout, and a full queue all release callers. A
grant racing cancellation is completed as ignored so capacity cannot leak.

No dynamic queue multiplier is implemented. This avoids a learned limit
silently multiplying memory. If a future dynamic bound is added, it must retain
an absolute maximum.

`Metadata` is diagnostic only. Priority is range bounded and partitions must be
selected from a construction-time list with at most 64 keys. Neither changes
admission ordering, so callers cannot inflate priority, no tenant receives a
hidden reservation, and FIFO cannot starve an older waiter. Authorization and
tenant identity remain outside the module.

Use queueing only for short bursts. A long local queue consumes the caller's
deadline and hides load shedding; prefer immediate rejection when upstream can
back off or serve a fallback.
