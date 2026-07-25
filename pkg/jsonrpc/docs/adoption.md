# Production adoption guide

Adopt the package at a protocol boundary, not by rewriting business logic at
the same time. The sequence below keeps wire compatibility observable and
rollback straightforward.

## 1. Inventory

For every existing method, record:

- exact method name;
- positional or named params and whether params may be omitted;
- result shape;
- error codes, messages, and public data;
- notification behavior;
- ID types accepted in real traffic;
- batch use and any ordering assumptions;
- transport status, headers, authentication, and body limits.

Capture fixtures from documented contracts, not confidential production
payloads. Flag any current behavior that diverges from JSON-RPC 2.0 rather than
silently preserving it as the new default.

## 2. Build a compatibility harness

Run the same fixtures against the current implementation and a dispatcher
configured with the new handlers. Compare decoded JSON values, error codes, ID
types, response omission for notifications, and mixed-batch membership. Avoid
byte comparison when object key order is irrelevant.

## 3. Map application errors

Create one table from internal errors to public RPC errors. Expected domain
failures may return `*jsonrpc.Error`; unexpected failures should remain ordinary
errors and pass through a safe `WithErrorMapper`. Verify logs retain causes and
wire responses do not.

## 4. Attach operational controls

Add bounded-cardinality metrics, tracing, structured logging, authentication,
authorization, request limits, and deadlines. Verify notification failures are
observable even though no response exists. Confirm raw params and credentials
are excluded from default logs.

## 5. Shadow

Where the existing transport permits it, mirror sanitized requests to the new
dispatcher without using its result. Compare response classes and timing. Do
not execute mutating methods twice; use recorded fixtures or read-only methods
for those paths.

## 6. Rollout

Route a small, identifiable fraction of requests through the new boundary.
Watch method-level error codes, invalid request rates, latency, panic recovery,
body-limit rejection, and missing batch responses. Increase gradually after a
representative traffic window.

## 7. Rollback

Keep the prior handler path deployable until the rollout window closes. A
rollback must switch the boundary without changing request IDs or retrying
notifications. Document whether callers might retry ordinary requests after a
transport failure and ensure methods are appropriately idempotent.

## Readiness checklist

- Every method has valid, invalid, and error fixtures.
- Notifications are proven to emit no response.
- Empty, mixed, invalid-member, and notification-only batches are covered.
- Client and server body limits fit expected payloads.
- Context cancellation reaches downstream I/O.
- Public error data contains no internal or personal information.
- Dashboards and alerts use bounded labels.
- Rollout and Rollback owners know the switch mechanism.
