# FAQ

## Why is `Date` not `time.Time` at midnight?

Midnight requires a location and can be skipped. A civil date is not an instant.

## Does one day equal 24 hours?

No. Calendar addition changes the civil date. Exclusive instant day ranges can
be 23, 24, 25, or historically other elapsed lengths.

## Which month policy is the default?

None. Every month/year operation names a policy.

## Are holidays included?

No. See [holiday datasets](holiday-datasets.md).

## Can I parse natural language?

No. Convert user-facing input in a separate locale-aware presentation layer,
then construct a validated canonical value.

## Where are intervals, clocks, and schedules?

Use temporal, clock, and scheduler respectively.
