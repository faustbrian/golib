# Laravel And PHP Migration

Golib replaces the narrow service infrastructure used by Track, Postal, and
Location; it does not reproduce Laravel's container, model binding, sessions,
views, or implicit global helpers.

## Concept Map

| Laravel or PHP concern | Go boundary | Migration note |
| --- | --- | --- |
| HTTP kernel and service providers | `service`, `router`, `http-middleware` | Construct dependencies explicitly in the composition root. |
| Controllers and RPC methods | application use case plus thin transport adapter | Keep request objects and protocol models out of application logic. |
| Sanctum/basic/bearer/API keys | `authentication` and focused adapters | Authentication establishes identity; authorization remains separate. |
| Gates, policies, RBAC/ACL/ABAC | `authorization` | Preserve action and resource semantics, not framework method names. |
| Eloquent and query builder | pgx/sqlc through application-owned persistence seams | Preserve schema and query behavior before optimizing. |
| Laravel migrations | `migrations` | Baseline existing history; do not replay already applied Laravel migrations. |
| Cache | `cache` with Redis or Valkey adapter | Make TTL, stale behavior, serialization, and failure policy explicit. |
| Horizon and queues | `queue`, `queueservice`, `queue-control-plane` | Queue mechanics, worker lifecycle, and operational control are separate owners. |
| Scheduler | `scheduler` plus `lease` | Singleton and overlap guarantees require fenced distributed ownership. |
| HTTP client | `http-client` plus focused resilience packages | Preserve pagination, retry, throttling, and error contracts per vendor. |
| Filesystem/Flysystem | `filesystem` | Select local, SFTP, or object-storage adapters explicitly. |
| Monolog | standard `log/slog` plus `log` handlers | Pass `*slog.Logger`; avoid a package-global logger. |
| Carbon | `clock`, `temporal`, `calendar`, `opening-hours` | Use the smallest semantic owner rather than one universal date object. |
| Validator | `validation` | Keep transport decoding separate from domain validation. |
| `.env` and config | `config` | Local files are development inputs; deployed secrets remain platform-owned. |

## Staged Replacement

1. Freeze request, response, queue, and database fixtures from the PHP service.
2. Characterize model casts, mutators, raw writes, timestamps, and transaction
   boundaries before replacing persistence.
3. Introduce Go reads against the existing schema and compare results.
4. Introduce idempotent Go writes behind a controlled routing boundary.
5. Shadow imports, jobs, and provider calls before moving ownership.
6. Compare latency, allocations, database load, and error behavior on the same
   hardware and payloads.
7. Move traffic gradually with an explicit rollback that does not require
   reversing a schema migration.

For existing Laravel migration tables, create a distinct Golib migration
history and baseline the currently deployed schema. New forward-only changes
start after that baseline. Do not rename or reinterpret Laravel history merely
to satisfy a migration tool.

See the [service recipe](../recipes/service.md),
[durability recipe](../recipes/durable-worker.md), and
[operations](../operations/index.md).

Return to the [migration index](index.md).
