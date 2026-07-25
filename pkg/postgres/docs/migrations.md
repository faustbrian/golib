# migrations integration

`postgres` does not run migrations during service startup. Schema changes
have different locking, permissions, rollout, and failure semantics from
ordinary application traffic and should be executed as an explicit deployment
step or Kubernetes Job.

Use the executable [`migrations` example](../examples/migrations/) with its
`database/sql` PostgreSQL adapter and the same secret source as the service.
It constructs the owned `postgres` pool, exposes it through pgx's standard
library bridge, and applies embedded migrations from a dedicated process.
Give the migration identity only the DDL permissions it needs. Application
roles should normally have narrower DML permissions.

Recommended deployment order:

1. run backward-compatible expand migrations;
2. deploy code that can use old and new schema forms;
3. backfill with bounded batches and observable progress;
4. switch reads/writes;
5. run contract migrations only after old code cannot return.

The [Kubernetes migration Job](../examples/kubernetes/migration-job.yaml) runs
that dedicated command with a secret reference, deadline, and no automatic retry.
Tune retry policy to the migration tool's advisory-lock and idempotency
contracts.
