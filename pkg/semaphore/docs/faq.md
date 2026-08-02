# FAQ

## Is this a mutex?

No. It permits weighted concurrent ownership and does not identify one critical
section owner. Use `sync.Mutex` for mutual exclusion.

## Is capacity global across pods?

No. Capacity is per process and usually per pod. Use durable leases and fencing
for a global constraint.

## Why can a small waiter remain blocked when weight is available?

An older large waiter owns queue precedence. Bypassing it can starve large
requests indefinitely. Separate resource classes only when that policy is
explicit.

## Does canceling acquisition cancel admitted work?

No. Context controls waiting and is passed to convenience operations for
cooperative cancellation. A returned permit must always be released.

## Does Close revoke permits?

No. Close stops admission and rejects queued callers. Existing permits remain
valid until release; `Wait` observes their drain.

## Why are there no permit leases or finalizers?

Neither proves that protected work stopped. Automatic capacity return could
admit overlapping work and violate conservation at the real resource.

## Can an observer block or reenter?

Yes without corrupting accounting because callbacks run outside the lock.
Blocking delays the delivering caller; adapters should own any asynchronous
buffer and lifecycle explicitly.
