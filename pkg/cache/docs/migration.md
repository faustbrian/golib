# Migration guide

## From map or ad-hoc memory caches

Define a typed key encoder and value type, choose explicit byte/entry bounds,
and replace `(value, ok)` handling with `Result.State`. Preserve source errors
instead of treating every false result as a miss.

## From direct Redis/Valkey JSON

Keep native client creation in the application. Introduce a versioned key space
and codec, then deploy readers before writers if old and new formats must
coexist. Prefer a new key-space version for an incompatible cutover so no key
scan is required.

## From singleflight wrappers

Replace unbounded `singleflight.Group` use with `GetOrLoad`. Set measured
`MaxConcurrent` and `MaxWaitersPerKey` values, make loaders honor their supplied
context, and call `Close` during shutdown.

## Release upgrades

Read every version in `CHANGELOG.md`. For changes to keys, codecs, TTLs, error
semantics, interfaces, or adapters, follow the compatibility note and deploy in
the stated order. Run your backend through `cachetest.RunBackendConformance`
with a deterministic outage hook after upgrading.
