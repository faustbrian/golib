# FAQ

## Does an empty schedule mean always open?

No. The zero schedule and inherited root days are closed.

## Is `00:00-00:00` full day?

No. Equal endpoints are invalid. Use `OpenAllDay`.

## Which day owns an overnight range?

The start date. The following date evaluates the spill before its own exception
operations.

## Does a local query resolve DST?

Yes. `IsOpenLocal` requires the same explicit gap/fold policy as `ResolveLocal`.
Use `RejectDST` to reject a local time that occurs zero or two times, or select
the documented fold/gap policy deliberately.

## Can metadata change availability?

No. It changes full equality/hash but not `SemanticallyEqual` or query results.

## Does open mean an order is eligible?

No. Eligibility, SLA, inventory, and authorization remain application policy.
