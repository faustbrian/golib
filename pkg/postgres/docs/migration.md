# Migration from direct pgx and database/sql

## From direct pgxpool wiring

1. Keep existing SQL and generated queries unchanged.
2. Translate pool sizes, lifetimes, and hooks into `Config`; compare every old
   default because this module deliberately uses finite explicit values.
3. Replace pool construction with `New` and pass `pool.Raw()` to existing code.
4. Replace manual begin/defer rollback patterns with `RunTransaction` where its
   exactly-once callback contract fits.
5. Replace string matching with `Classify` and SQLSTATE predicates.
6. Add readiness, bounded shutdown, and observation adapters.
7. Run real database contention, cancellation, and failure tests before rollout.

## From database/sql

Generated and handwritten code must move to pgx/v5 types, including `pgx.Rows`,
`pgx.Row`, `pgconn.CommandTag`, codecs, batches, and copy APIs. Audit null,
timestamp, numeric, array, JSON, and custom type semantics. `database/sql`
connection lifetime and transaction behavior are similar concepts but not
identical contracts.

Roll out behind normal service canaries. Compare connection counts, acquisition
wait, error categories, transaction latency, and query results. Do not run two
large pools per replica longer than needed during migration.
