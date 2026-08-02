# Security policy

Report suspected vulnerabilities privately through the repository security
contact. Do not include credentials, request bodies, raw headers, database
values, tenant identifiers, or production experiment tokens in an issue,
fixture, event, log, or reproduction.

## Activation boundary

The zero injector is disabled. The package does not read environment variables,
configuration files, flags, network endpoints, or global registries. Tests
construct an `Injector` explicitly. Runtime experiment wiring must use the
fail-closed `Runtime` gate described in [docs/safety.md](docs/safety.md).

## Supported reports

Security reports should identify unauthorized activation, stale authorization,
expiry bypass, budget bypass, allowlist widening, unsafe event data, unbounded
latency or allocation, resource leaks, adapter ownership violations,
nondeterministic selection, or a way to reactivate a disabled runtime gate.
