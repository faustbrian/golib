# Migration and compatibility

## From raw maps or generated structs

Parse existing JSON in preserving mode, inspect unknown fields, then move to
strict mode. Replace narrowed schema structs with `jsonschema.Schema` values.
Use constructors to distinguish absent optional fields from explicit defaults.

## From implicit reference loading

Remove global URL and filesystem loaders. Create a resolver per policy scope,
select an explicit store, and pass a context and absolute base at each resolve
operation. Expect previously hidden I/O to become visible errors.

## Between OpenRPC patch versions

All canonical `1.4.x` patch values share the 1.4 feature set. Preserve the
declared patch spelling. Other minor and major lines require a new supported
implementation and do not fall back to 1.4 behavior.

## Public API evolution

Before v1, incompatible Go API changes remain possible but must preserve
document semantics and be recorded in `CHANGELOG.md`. After v1, release checks
must compare exported APIs and document migrations before an incompatible major
release.
