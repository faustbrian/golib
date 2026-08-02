# Goal: Authentication OpenTelemetry Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `authotel` as the optional OpenTelemetry adapter for `authentication`.
It MUST turn completed authentication events into bounded, payload-free traces
and metrics without changing authentication outcomes or adding telemetry to the
core module.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Implement the `authentication.Instrumenter` contract with providers supplied
  explicitly by the caller.
- Emit stable attempt, duration, credential-kind, outcome, and failure-kind
  observations with a documented semantic-convention and version policy.
- Keep attribute cardinality finite and defined by closed authentication enums.
- Never record credentials, tokens, claims, subjects, issuers, API keys, error
  text, panic values, endpoints, or caller-controlled identity strings.
- Define exactly-once completion, duplicate completion, canceled context,
  observer failure, and concurrent-use behavior.
- Start no goroutines and make disabled/no-op telemetry cheap.
- Keep authentication independent from exporter health, SDK queueing, sampling,
  and shutdown.

## Public Contract And Documentation

Document construction, provider ownership, instruments and attributes,
cardinality, privacy, failure isolation, lifecycle, examples, adoption, FAQ,
compatibility, and migration policy. Preserve a small API with no global
provider lookup and no authorization decisions.

## Completion Gates

CI MUST enforce formatting, vet, static analysis, race tests, fuzz smoke,
security and dependency checks, API compatibility, docs checks, benchmarks,
exactly 100% statement coverage, and exactly 100% of viable mutants killed by
meaningful behavioral tests. Coverage-only assertions and equivalent-mutant
games do not satisfy this goal.
