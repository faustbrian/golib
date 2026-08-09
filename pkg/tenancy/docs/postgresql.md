# PostgreSQL and RLS

Use `Predicate` when the application owns explicit SQL predicates. Use
`Manager.WithTenant` when a transaction-local custom setting and RLS are part
of the isolation model. Both may be used together as defense in depth.

`Manager` leases one connection, clears stale state, starts a transaction,
installs `app.tenant_id` with transaction-local `set_config`, reads it back,
and passes only that transaction to the operation. Connection acquisition and
internal transaction calls receive the same tenant-scoped context. The manager
reads the setting again after the callback and before commit, so a callback
that returns with changed scope causes the complete transaction to roll back.
The callback is a trusted persistence boundary: it must not mutate, reset, or
temporarily replace the configured setting. A callback that changes the tenant,
performs work, and restores the original value before returning can bypass the
final readback. Prevent direct setting mutation through application-owned SQL
interfaces and review; the manager cannot make an unrestricted `*sql.Tx`
incapable of issuing PostgreSQL configuration statements. It rolls back every
failure path and clears the same connection under a bounded cleanup context. A
failed reset discards the physical connection.

Apply `NewRLSPlan` statements in migrations. Run application queries as a
non-owner role without `BYPASSRLS`; table owners and privileged roles can bypass
policies. Use both `ENABLE ROW LEVEL SECURITY` and `FORCE ROW LEVEL SECURITY`.
The generated plan pairs a scoped permissive grant with a restrictive policy.
PostgreSQL requires at least one permissive policy before restrictive policies
can admit rows; the restrictive half remains an AND constraint when other
permissive policies apply. Both policies use the same `USING` and `WITH CHECK`
expressions, which treat an absent or empty setting as matching no tenant.
Apply `CreateGrant` before `Create`. On rollback, drop `DropGrant` before `Drop`
so the restrictive constraint is never removed while a broader grant remains.

Prepared statements must be created and executed through the callback
transaction. Do not retain `*sql.Tx`, `*sql.Conn`, or tenant context after the
callback. `WithSystem` records explicit system intent and clears the tenant
setting; it does not bypass RLS. Administrative SQL that genuinely spans all
tenants needs a separately authorized database role and audit path.

The live integration test requires `POSTGRES_URL` for a PostgreSQL principal
that can create temporary roles and tables. It creates a distinct non-owner,
non-`BYPASSRLS` application login and proves absent/system-scope denial,
restrictive-policy composition, cross-tenant reads and mutations, prepared-plan
reuse across tenants, rollback, cancellation, stale-session cleanup, backend
termination and replacement, concurrent pool reuse, and idle-scope reset.
