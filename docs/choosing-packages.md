# Choosing Packages

Choose the narrowest module that owns the contract you need. Do not adopt an
adapter, persistence backend, or service runtime merely because another Golib
module can integrate with it.

## Decision Guide

| Requirement | Start with | Add only when needed | Avoid when |
| --- | --- | --- | --- |
| Plain HTTP routing | `router` | `http-middleware`, `service` | `net/http` alone already provides the complete lifecycle |
| Operation-oriented RPC | `jsonrpc` | `openrpc`, `service` | Resources and generic HTTP semantics are the public contract |
| Resource-oriented API | `jsonapi` | `api-query`, `service` | Most operations are commands that map poorly to resources |
| Conventional HTTP API | `router` plus `openapi` | `http-middleware`, `service` | JSON-RPC or JSON:API is already the normative contract |
| Outbound vendor calls | `http-client` | resilience modules, cache, telemetry | A small direct `net/http` client is sufficient and policy-free |
| Durable jobs | `queue` | `queue-control-plane`, idempotency, telemetry | Work is synchronous or a database transaction is the real boundary |
| Transactional event publication | `outbox` | queue or broker adapter | The event does not originate with a database transaction |
| Duplicate suppression | `idempotency` | PostgreSQL or Valkey adapter | A caller-provided unique constraint fully owns the invariant |
| Scheduled singleton work | `scheduler` | `lease`, queue | Plain process-local cron is sufficient |
| Database access | `postgres` | `migrations`, outbox, sqlc-generated code | Direct pgx already expresses the complete boundary |

## Boundary Rules

- `service` owns process lifecycle and composition, not business rules.
- `router` owns request dispatch, not dependency injection or model binding.
- `http-middleware` owns transport policy, not authorization decisions.
- `authentication` establishes identity; `authorization` decides permitted
  actions. Neither should absorb application policy data by convenience.
- `queue` owns durable delivery semantics; `queue-control-plane` owns operator
  visibility and control.
- `postgres`, `cache`, and provider adapters expose infrastructure contracts;
  application packages should depend on the narrow interfaces they consume.
- Resilience policies must wrap the actual failure boundary and preserve
  cancellation, idempotency, and retry safety.

For protocol choices, use [API protocols](api-protocols.md). For complete
compositions, use [recommended stacks](recommended-stacks.md). The older
[package selection](package-selection.md) page contains the family taxonomy and
additional tradeoffs.

Return to the [documentation index](index.md).
