# Troubleshooting

## `openrpc: unsupported specification version`

Use a canonical `1.4.x` version. Prerelease, build metadata, older, and future
feature lines are intentionally rejected.

## `reference resolution failed`

Check that external access is enabled only where intended, the absolute base is
correct, the store contains the exact document URI, and scheme/host policies
allow it. The safe error omits the URI; inspect caller-owned configuration.

## Meta-schema passes but semantic validation fails

Structural JSON Schema cannot express every uniqueness, ordering, link target,
runtime-expression, and reference rule. Review stable semantic diagnostic codes
and JSON Pointers.

## Canonical output differs from source

Canonical output sorts keys and removes insignificant whitespace. Use
`Result.PreservingJSON` for exact accepted source bytes.

## `make coverage` fails

The gate requires meaningful 100.0% production statement coverage. Use
`make coverage-report` to inspect the current value and add behavior-focused
tests; do not exclude ordinary production files.

## `rpc.discover` cannot be registered

Use `jsonrpc.RegisterDiscovery` with `jsonrpc.Registry.RegisterSystem`.
Ordinary `Registry.Register` intentionally continues to reject every `rpc.*`
method so application code cannot impersonate protocol extensions.
