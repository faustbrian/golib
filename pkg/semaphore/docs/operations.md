# Operations and security

Size capacity from the protected local resource, not request rate. Weighted
units must represent one documented, stable resource quantity. `MaxWaiters`
bounds memory and waiting demand; a full queue is overload evidence and should
normally fail fast rather than trigger unbounded retries.

Monitor snapshot utilization (`Acquired / Capacity`), queue depth, rejection
reasons, cancellation rate, and drain deadlines. Admissions are not downstream
successes, and queue-full or closed rejections must not be recorded as
dependency failures by an outer circuit breaker.

Observers are synchronous delivery hooks outside the lock. Keep them fast,
non-blocking, bounded, and safe for concurrent and reentrant calls. If an
adapter buffers events, that adapter owns its queue bound, goroutine lifecycle,
loss policy, and shutdown. Observer panics are ignored so telemetry cannot
change permit accounting. Concurrent delivery is not a global ordering
guarantee; consumers should use each event's immutable transition snapshot.

The API accepts no keys, credentials, arbitrary labels, or error text. Permit
IDs are process-local monotonic diagnostics and must not be treated as secrets,
authorization, distributed fencing tokens, or globally unique identifiers.
Snapshots expose aggregate local state only.

On overload, investigate held permit duration and abandonment before raising
capacity. A permanently held permit is caller-owned lifecycle failure. The
package intentionally does not expire permits because expiration cannot stop
still-running work safely.
