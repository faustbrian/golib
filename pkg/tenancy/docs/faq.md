# FAQ

## Does a tenant ID authorize access?

No. Authentication and authorization decide whether a caller may use a scope.

## Why is `TenantID.String` redacted?

Formatting often reaches logs, traces, and errors. Use `Value` only at a
trusted transport, persistence, or namespace boundary.

## Can middleware infer a tenant from any header or payload field?

No. Extraction requires a configured field and an explicit trust decision.

## Can system work use an empty tenant ID?

No. It needs `SystemCapability` with administrative reason metadata.

## Does `WithSystem` bypass PostgreSQL RLS?

No. It installs an empty tenant setting. Database roles independently govern
cross-tenant access.

## May a scoped context be stored for later work?

No. Pass immutable scope into an owned bounded executor and derive a new
context for each operation.

## Are namespace hashes authorization tokens?

No. They prevent ambiguity and disclosure in keys; provider access controls
still enforce who can read or write those keys.
