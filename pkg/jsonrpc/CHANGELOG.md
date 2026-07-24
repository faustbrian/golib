# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Expose JSON-RPC specification verification as an explicit conformance gate.
- Added the `GO-SAFETY-1` ownership, concurrency, race, fuzz, resource, and
  benchmark standard with an executable `make safety` gate.
- Moved AI planning and hardening briefs into `.ai/` and clarified the
  separate purposes of project and third-party notice files.

### Added

- `Registry.RegisterSystem` for trusted protocol integrations such as
  OpenRPC's reserved `rpc.discover` method.
- A standardized OSS repository skeleton covering policy, documentation,
  legal notices, Go tooling, pinned CI, security, and release automation.

## [1.0.0] - 2026-07-14

### Added

- Evidence-driven audit and hardening goal covering JSON-RPC conformance,
  hostile inputs, transports, concurrency, compatibility, and release readiness.
- Living hardening report, threat model, and normative JSON-RPC conformance
  matrix with explicit evidence and open-risk tracking.
- Dispatcher payload and batch-member options with safe defaults of four MiB
  and 1,024 members, plus an inspectable request-limit protocol error.
- A transport-neutral four-MiB client reply parsing limit with an additive
  option and inspectable oversized-response sentinel.
- Transport-neutral JSON-RPC 2.0 request, notification, response, and batch
  processing.
- Canonical public module path at `github.com/faustbrian/golib/pkg/jsonrpc`.
- Concurrency-safe server registry, middleware, request context, safe error
  mapping, and panic containment.
- Plain `net/http` handler with media-type and body-size enforcement.
- Typed client calls, notifications, mixed batches, strict response validation,
  custom ID generation, and custom transport support.
- Bounded HTTP client transport with headers and caller-provided HTTP clients.
- Official-spec conformance fixtures, meaningful full coverage, race tests,
  fuzz targets, and single/batch benchmarks.
- CI, static analysis, security scanning, dependency updates, benchmark/fuzz
  automation, and semantic-version tag releases.
- Guarded patch, minor, and major Makefile release commands that create local
  annotated tags without pushing them.
- Quickstart, architecture, API, cookbook, adoption, middleware,
  troubleshooting, FAQ, compatibility, release, and community documentation.
- Shared repository instructions for Claude Code through the canonical
  `AGENTS.md` rules.
- Generated `llms.txt` index and `llms-full.txt` bundle sourced from the
  canonical Markdown documentation.
- Canonical JSON-RPC 2.0 section links on protocol types, dispatch behavior,
  error objects, and conformance fixtures.

### Fixed

- Bound fuzz-smoke concurrency to avoid deadline flakes on high-core hosts.
- Reject duplicate members in request, response, and error envelopes instead
  of inheriting `encoding/json`'s last-member-wins behavior. This defensive
  interoperability policy prevents ambiguous peers from interpreting the same
  protocol object differently.
- Reject duplicate generated IDs within a client batch before transport I/O so
  every non-notification response remains unambiguously correlatable.
- Reject case variants of reserved request, response, and error-object members
  instead of inheriting `encoding/json`'s case-insensitive struct matching.
- Reject invalid UTF-8 throughout protocol envelopes and classify invalid
  server input as a parse error instead of silently replacing malformed bytes.
- Reject duplicate top-level names in `DecodeParams` named-parameter objects
  before Go's JSON decoder can collapse them.
- Reject oversized transport-neutral payloads and batches before parsing or
  handler execution can amplify their CPU, allocation, or downstream cost.
- Normalize arbitrarily long numeric-ID exponents with linear decimal-string
  arithmetic instead of allocation-heavy arbitrary-precision integers.
- Reject oversized replies from every client transport before JSON parsing,
  not only when the built-in HTTP transport enforces its body limit.
- Make the exported `Registry` zero value safe for concurrent registration and
  lookup instead of panicking on its first registration.
- Ignore nil functional options consistently and return HTTP request
  construction errors, including nil-context misuse, without network I/O.
- Expand continuous fuzzing across response and error decoding, ID round
  trips, and single and batch client correlation.
- Store the complete official JSON-RPC example corpus as stable conformance
  fixtures consumed by automated tests.
- Add hostile-boundary benchmarks for maximum dispatcher payloads and
  oversized generic client replies.
- Document every exported Go declaration and clarify safe custom error-code
  selection outside the JSON-RPC reserved range.
- Stop the default HTTP transport from following redirects and potentially
  forwarding caller-configured credentials to another origin.
- Reject trailing JSON values passed directly to `ID.UnmarshalJSON`.
- Keep `StringID` correlation equal to its actual JSON encoding when a Go
  string contains invalid UTF-8.
- Seed fuzzing with the checked-in specification corpus, deep JSON, and large
  batches in addition to malformed and boundary values.
- Add race, cancellation, chunked-reader, and response-body cleanup regression
  coverage for runtime ownership contracts.
- Preserve the nil-context transport regression test under both direct
  Staticcheck and golangci-lint without weakening either analyzer.

[Keep a Changelog]: https://keepachangelog.com/en/1.1.0/
[Semantic Versioning]: https://semver.org/
[Unreleased]: https://github.com/faustbrian/golib/pkg/jsonrpc/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/faustbrian/golib/pkg/jsonrpc/releases/tag/v1.0.0
