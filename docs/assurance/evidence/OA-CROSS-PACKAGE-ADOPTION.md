# Cross-Package Adoption Evidence

Observed at `2026-08-13T02:00:11Z` on `darwin/arm64` with Go `1.26.5`.

## Executed Proof

The non-production adoption fixture exercised the public composition APIs used
to construct representative Track, Postal, and Location service roles:

- API, JSON-RPC, worker, scheduler, migration, online-migration, and activation
  roles selected only their declared facilities and kept business and
  management HTTP boundaries distinct;
- public telemetry, PostgreSQL, Kafka, cache, queue, scheduler, migrations,
  configuration, correlation, and service adapters composed without a service
  locator, global registry, or hidden business package;
- representative Track, Postal, and Location requests crossed the canonical
  HTTP lifecycle and returned caller-owned correlation and request identities;
- API, RPC, worker, scheduler, and one-shot roles shared bounded retry budgets,
  caller-owned deadlines, and explicit named resilience policies;
- dependency failure and overload retained truthful readiness, bounded retries
  and fleet amplification, rejected locally shed work before it reached the
  dependency, and preserved mixed policy revisions during rollout;
- draining closed new admission, released blocked waiters, bounded shutdown,
  and retained an honest incomplete-drain result for uncooperative work; and
- the retained generic bootstrap for each service stayed within its explicit
  adoption budget.

The complete fixture passed normal and race-detector execution. Its structural
adoption-budget check also passed. The cold task-owned Go caches were removed
after the bounded run.

## Claim Boundary

This proves bounded local composition consistency for the represented service
roles and modules. It does not prove actual Track, Postal, or Location business
logic; durable database or broker behavior; tenant propagation; idempotency;
redaction; telemetry export; unknown external outcomes; Linux or container
execution; managed dependencies; or real ECS rolling deployment. It also does
not establish consistency for every releasable module. `OA-CROSS-PACKAGE-
CONSISTENCY` therefore remains pending.
