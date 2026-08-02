# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Immutable validated rule configuration with stable precedence, bounded
  composition, deterministic nth/every/sequence/seeded schedules, typed
  metadata predicates, snapshots, and generation-safe reset.
- Explicit error, latency, cancellation, deadline, bounded panic, byte, partial
  IO, network, reset, half-close, and interruption faults.
- Generic execution, HTTP transport/body, reader/writer, connection, dialer,
  listener, filesystem, sleeper, and timer-factory adapters with documented
  ownership and partial-result semantics.
- Bounded attribution events and a fail-closed runtime experiment gate with
  authorization, allowlist, expiry, evaluation budget, audit, and terminal
  emergency disable.
- Deterministic golden, exact statement coverage, race/stress, fuzz, adapter
  contract, leak, example, and benchmark evidence.
- Isolated Failsafe-Go/goresilience comparison benchmarks and retry/circuit
  breaker campaign integrations without downstream production dependencies.
- Adoption, API, adapter, operations, security, Kubernetes, infrastructure
  comparison, extension, and FAQ documentation.
