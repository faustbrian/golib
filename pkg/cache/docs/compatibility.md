# Compatibility and SemVer policy

The module requires Go 1.25 or newer. CI tests the minimum supported Go 1.25
line and the current stable Go release.

Before `v1.0.0`, minor releases may change public APIs with changelog and
migration notes. Starting with `v1`, SemVer applies to:

- exported interfaces, types, functions, methods, constants, and errors;
- `errors.Is` identities and hit/miss/stale semantics;
- key prefix, hashing, encoding, and version behavior;
- TTL, stale, sliding, negative, jitter, loading, and shutdown behavior;
- built-in codec and wire-envelope compatibility;
- backend conditional, expiration, size, and conformance behavior;
- portable deadlines and their wall-clock interpretation;
- metric names, units, and label sets.

Adding a method to an exported interface is breaking. Changing a miss into an
error (or an error into a miss), changing key output, accepting previously
rejected ambiguous policy, or changing stored bytes incompatibly requires a
major release unless gated behind a new explicit API/version.

Supported backend integration versions for the initial release are Redis 7.2,
7.4, and 8.0, and Valkey 9.0. Older server versions may work but are not covered
by the release matrix.

The supported topology is standalone with optional password authentication and
verified TLS. Cluster, Sentinel, automatic failover, redirects, and replica
reads are outside the initial compatibility promise even when the supplied
native client exposes them. Adding a tested topology expands the compatibility
matrix and requires explicit changelog and operations guidance.
