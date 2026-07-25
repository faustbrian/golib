# PostgreSQL

Use `postgres.NewDate` for a finite non-null PostgreSQL `date`. The adapter
implements `database/sql.Scanner`, `driver.Valuer`, `pgtype.DateScanner`, and
`pgtype.DateValuer`. SQL `NULL` and infinity are errors.

Use `postgres.InfinityDate` only when the schema intentionally admits
`-infinity` or `infinity`. Those sentinels are never coerced to ordinary civil
dates. Native pgx text/binary codecs and a live PostgreSQL round trip are tested.

`make integration` uses `POSTGRES_URL` when supplied or starts a disposable
PostgreSQL 18 Docker container. CI runs the same tagged suite across supported
PostgreSQL versions.
