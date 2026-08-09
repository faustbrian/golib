# PostgreSQL and RLS

Use `Predicate` when the application owns explicit SQL predicates. Use
`Manager.WithTenant` when a transaction-local custom setting and RLS are part
of the isolation model. Both may be used together as defense in depth.

`Manager` leases one connection, clears stale state, starts a transaction,
installs `app.tenant_id` with transaction-local `set_config`, reads it back,
and passes only that transaction to the operation. It rolls back every failure
path and clears the same connection under a bounded cleanup context. A failed
reset discards the physical connection.

Apply `NewRLSPlan` statements in migrations. Run application queries as a
non-owner role without `BYPASSRLS`; table owners and privileged roles can bypass
policies. Use both `ENABLE ROW LEVEL SECURITY` and `FORCE ROW LEVEL SECURITY`.
The generated `USING` and `WITH CHECK` expressions treat an absent or empty
setting as matching no tenant.

Prepared statements must be created and executed through the callback
transaction. Do not retain `*sql.Tx`, `*sql.Conn`, or tenant context after the
callback. `WithSystem` records explicit system intent and clears the tenant
setting; it does not bypass RLS. Administrative SQL that genuinely spans all
tenants needs a separately authorized database role and audit path.

The live integration test requires `POSTGRES_URL` for a PostgreSQL principal
that can create temporary roles and tables. It proves forced RLS reads and
writes, prepared statements, rollback, and reuse of a one-connection pool.
