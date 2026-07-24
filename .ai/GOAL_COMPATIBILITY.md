# Goal: Preserve Public Compatibility

## Mission

Define and enforce compatibility across independently versioned Go modules,
formal protocols, persistence behavior, and service integrations.

## Compatibility Surface

Inventory and govern:

- exported packages, types, functions, methods, fields, constants, variables,
  interfaces, generic constraints, and error types;
- accepted inputs, defaults, zero values, option order, ownership, and
  concurrency guarantees;
- serialized JSON, XML, SOAP, YAML, TOML, MessagePack, CSV, and tabular forms;
- JSON:API, JSON-RPC, OpenRPC, JSON Schema, HTTP, authentication, and
  authorization semantics;
- database migrations, SQL behavior, queue envelopes, outbox records,
  idempotency keys, webhook signatures, cache keys, and scheduler semantics;
- error wrapping, `errors.Is`/`errors.As`, redaction, and retry classification;
- metrics names, attribute semantics, log fields, trace propagation, and
  cardinality limits;
- configuration keys, environment variables, file formats, precedence, and
  secret refresh behavior;
- performance or resource-limit guarantees explicitly promised to users.

## Enforcement

- Maintain per-module API baselines for stable releases.
- Run maintained API compatibility tooling before release.
- Add behavioral contract and interoperability tests where symbol checks are
  insufficient.
- Compare generated schemas and protocol fixtures deterministically.
- Require changelog and migration documentation for every breaking change.
- Distinguish bug fixes that correct non-conformant behavior from optional
  feature changes, and version them honestly.
- Prevent accidental dependency-driven increases in the minimum Go version.
- Test the declared supported Go and platform matrix.

## Interface Discipline

Keep interfaces small and consumer-oriented. Adding a method to a public
interface is breaking. Avoid broad shared interfaces, exported implementation
details, global registries, and concrete dependency leakage that make future
compatibility unnecessarily expensive.

## Deprecation

Prefer additive migration paths and documented deprecation before removal.
Every deprecation MUST identify the replacement, migration, removal version,
and behavior during the transition. Do not preserve insecure or
specification-invalid behavior indefinitely solely for compatibility.

## Completion Criteria

This goal is complete when every stable module has a documented compatibility
surface, automated API and behavioral checks, accurate SemVer decisions, and
no undocumented breaking change in the proposed release set.
