# Adoption guide

## Static documents

Start with `parse.Decode` in strict unknown-field mode. Preserve the returned
source only when exact re-emission matters; use `MarshalCanonical` for hashes,
reviews, and generated artifacts. Run both meta-schema and semantic validation.

## Existing JSON-RPC services

Inventory runtime method names, parameter structure, notification-only methods,
custom errors, and authorization visibility. Construct those concepts
explicitly. Do not infer protocol behavior from a structurally valid document.

Expose discovery through the transport-neutral `discovery.Service`. Keep batch,
notification, error envelope, request ID, and transport behavior in the
existing JSON-RPC implementation.

## Dynamic visibility

Supply a `discovery.Filter` that returns a new owned document for the current
authorization context. An empty methods array is valid. Never cache a filtered
snapshot across security contexts unless the cache key is caller-owned and
proven complete.

Use `discovery.NewServiceWithOptions` for generated or tenant-dependent
documents and set `MaxOutputBytes` to the largest discovery response the
transport contract permits. The default constructor applies a finite 64 MiB
ceiling and bounded semantic diagnostics.

## Schema reuse and references

Keep core parsing offline. Configure a `reference.Resolver` only at the call
site that needs external resources. Prefer `MemoryStore` or a scoped `fs.FS`.
For HTTP, allow exact schemes and hosts and retain the default private-address,
redirect, compression, timeout, and byte protections.

## CI compatibility diffing

Parse a released baseline and candidate, resolve both with the same explicit
resource policy, and call `diff.CompareResolved`. Treat `Breaking` as blocking
and review `Conditional` changes rather than silently accepting them.
`Report.Compatible` returns false until those findings are resolved.

## Extensions

Create specification extensions with `NewExtensions`; exact names must start
with lowercase `x-`. Preserve unknown standard fields only during an explicit
forward-compatibility workflow and do not reinterpret them as known fields.
