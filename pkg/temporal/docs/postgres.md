# PostgreSQL

`postgres` maps instant periods to `tstzrange`, date periods to `daterange`, and
normalized sets to corresponding multiranges.

PostgreSQL timestamps are microsecond precision. Instant values with non-zero
sub-microsecond data are rejected; they are never rounded silently. Unbounded
and PostgreSQL-empty ranges are rejected because core periods are bounded and
carry their own empty representation. SQL NULL uses `InstantRange` or
`DateRange` wrappers and is distinct from an empty range.

PostgreSQL canonicalizes discrete dateranges to closed-open. Conversion back to
`dateperiod` retains represented dates, so structural bounds may differ while
`SetEqual` remains true. Maximum-date exclusive-end overflow is rejected.

Run the disposable integration suite with:

```sh
TEMPORAL_POSTGRES_DSN='postgres://temporal:temporal@localhost/temporal_test' \
  go test -tags=integration ./postgres
```
