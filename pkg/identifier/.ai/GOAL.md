# Goal: Typed Identifier Generation And Validation

## Objective

Build `identifier` as a coherent, secure, typed identifier package for UUID,
ULID, TypeID, KSUID, NanoID, and explicitly justified distributed identifiers.
It MUST provide consistent parsing, formatting, generation, ordering,
serialization, entropy, clock, and validation contracts without pretending all
identifier families have identical guarantees.

## Core Scope

- UUID versions required by current standards, prioritizing v4 and v7.
- ULID with monotonic generation and explicit clock/entropy ownership.
- TypeID with prefix validation and canonical encoding.
- KSUID and NanoID where interoperability or compact random IDs justify them.
- Optional Snowflake-style IDs only with explicit node allocation, clock
  rollback, sequence overflow, and deployment requirements.
- Generic strongly typed IDs using Go generics without reflection or runtime
  registration.
- Parse, validate, compare, sort, inspect timestamp where defined, marshal text,
  JSON, binary, SQL, and PostgreSQL UUID values.
- Deterministic test generators and injected clocks/entropy.

## Security And Semantics

- Use `crypto/rand` by default for random identifiers.
- Reject biased alphabets, insufficient entropy, malformed prefixes, ambiguous
  encodings, non-canonical forms, and unsupported versions.
- Document predictability, sortability, timestamp leakage, collision bounds,
  privacy, database locality, and distributed-node requirements per family.
- Never call an identifier secret, authorization evidence, idempotency proof,
  or correlation context automatically.
- Do not expose mutable byte aliases or ambient global generators.

## Package Shape

- Root: common errors, generator contracts, typed-ID helpers, inspection.
- `uuid`, `ulid`, `typeid`, `ksuid`, `nanoid`, optional `snowflake`.
- `idtest`: deterministic sources, clocks, collision and format assertions.

## Verification And Documentation

Require meaningful 100% production coverage, official vectors, differential
tests, collision simulations, clock rollback/overflow tests, fuzzing, race
tests, mutation tests, SQL/JSON round trips, and fair benchmarks against
maintained identifier packages.

Document selection guidance, guarantees, leakage, database behavior,
serialization, migration from Laravel ULIDs/Cline Mint, security, performance,
FAQ, compatibility, and changelog. All local and CI gates follow ecosystem
standards.

## Acceptance Criteria

- Every algorithm's guarantees and failure modes are explicit.
- Postal ULID persistence and ordering can migrate without value changes.
- Typed IDs prevent domain identifier mixing without reflection.
- Randomness, clocks, node IDs, and monotonic state have explicit ownership.
- Meaningful 100% coverage and every blocking gate pass.
