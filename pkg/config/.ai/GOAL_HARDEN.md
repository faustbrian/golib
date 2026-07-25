# Hardening Goal: Typed Layered Configuration

## Objective

Prove that `config` resolves one deterministic, immutable, validated, and
secret-safe snapshot under hostile files, environments, filesystem behavior,
source conflicts, parser differences, and optional remote-source failure.

## Required Audits

### Source And Precedence Audit

- Enumerate every source-order combination and prove the documented winner.
- Test missing optional/required sources, unreadable files, partial reads,
  cancellation, duplicate source names, equal priorities, and repeated loads.
- Prove a failed later layer cannot expose a partially merged snapshot.
- Verify programmatic defaults, files, dotenv, process environment, and explicit
  overrides preserve absent, empty, zero, null, and default distinctions.
- Ensure loading never mutates caller defaults, process environment, source maps,
  or previously returned snapshots.

### Merge And Decode Audit

- Exhaustively test scalar, object, map, slice, null, delete, replacement, and
  type-conflict semantics across every format.
- Reject unknown fields, duplicate keys, case-fold collisions, tag collisions,
  overflow, underflow, invalid duration/size/URL, unsupported destination kinds,
  and ambiguous embedded fields.
- Fuzz reflection paths, custom text unmarshaling, pointers, interfaces, maps,
  recursive types, panicking hooks, and malformed tags.
- Prove custom decoders cannot bypass size, depth, cancellation, or redaction
  contracts.

### Format And Interoperability Audit

- Compare equivalent JSON, YAML, and TOML documents and document intentional
  type-system differences.
- Test YAML aliases, merge keys, tags, duplicate mappings, scalar coercion, and
  expansion bombs.
- Test TOML dotted keys, arrays of tables, duplicate definitions, times, and
  numeric boundaries.
- Test dotenv quoting, escaping, comments, multiline values, `export`, duplicate
  variables, interpolation, cycles, and platform line endings.
- Bound bytes, nesting, keys, aliases, strings, collections, and parse time.

### Discovery And Filesystem Audit

- Test explicit and upward search, stop directories, roots, home/config
  directories, symlinks, loops, traversal, permissions, races, and path casing.
- Prove production defaults never discover a parent or home file implicitly.
- Test file replacement and truncation during read without accepting mixed data.
- Verify discovered-path diagnostics reveal no secret content or unsafe path
  detail beyond policy.

### Environment And Interpolation Audit

- Test operating-system case rules, invalid names, empty values, Unicode,
  duplicates, prefix collisions, nested separators, and very large environments.
- Verify process environment always wins over dotenv under the default plan.
- Fuzz interpolation syntax, defaults, escaping, recursion, cycles, expansion
  growth, missing variables, and secret references.
- Ensure interpolation provenance identifies sources without exposing values.

### Secrets And Diagnostics Audit

- Mark secrets through tags, wrappers, source sensitivity, nested structures,
  custom errors, and validation errors.
- Assert secrets never appear in formatting, marshaling, comparisons, diffs,
  provenance, traces, metrics, logs, panic recovery, or fuzz failures.
- Test copies, zeroization limitations, snapshots, and error wrapping honestly;
  MUST NOT claim Go memory can guarantee physical secret erasure.
- Verify file permission checks and insecure opt-outs are explicit.

### Concurrency And Lifecycle Audit

- Race-test parallel plans, loads, decoders, validators, custom hooks, snapshots,
  and repeated failures.
- Prove immutable snapshots cannot be changed through retained maps, slices,
  pointers, or provenance structures.
- Verify every I/O operation respects context cancellation and deadlines.
- Ensure no goroutine, file descriptor, timer, buffer, or remote client leaks.

### Infisical Adapter Audit

If the optional native adapter is implemented:

- Verify it remains separately imported and no SDK dependency enters core.
- Test Kubernetes identity, selected machine identities, token refresh, expiry,
  cancellation, retry, cache, stale fallback, API outage, TLS, and shutdown.
- Prove required-secret failure is fail-closed and stale values are never used
  beyond the explicit policy.
- Test project, environment, path, import, recursive, organization, and duplicate
  secret behavior with strict bounds.
- Ensure the adapter never writes secrets or attaches them to process environment.
- Compare native behavior with documented Operator/CSI-delivered file and
  environment workflows without claiming equivalence.

## Required Deliverables

- Complete source-precedence and merge truth tables.
- Cross-format conformance and intentional-difference matrix.
- Filesystem, environment, interpolation, and hostile-parser fuzz corpora.
- Secret-leak test harness covering all formatting and diagnostic surfaces.
- Race, cancellation, immutable-snapshot, and resource-ownership evidence.
- Optional Infisical compatibility and failure-mode matrix if implemented.
- Threat model, findings report, performance budgets, and updated API,
  operations, security, FAQ, and `CHANGELOG.md` documentation.

## Release Blockers

- Non-deterministic precedence or merge behavior.
- Partial snapshot returned after any source, decode, merge, or validation error.
- Secret disclosure through any supported diagnostic or integration surface.
- Implicit parent/home discovery in production defaults.
- Unbounded parsing, interpolation, discovery, recursion, allocation, or remote
  retry/cache behavior.
- Mutable returned snapshots, data race, panic, leaked resource, or ignored
  cancellation.
- Native Infisical dependency in core or production requirement for Kubernetes
  applications.
- Missing meaningful 100% coverage or a failing GitHub Actions gate.

## Completion Criteria

- Precedence, merge, format, discovery, environment, validation, and secret suites
  pass with deterministic evidence.
- Cross-platform, race, fuzz, vulnerability, compatibility, and performance gates
  pass.
- Optional Infisical behavior, if present, is isolated and fully failure-tested.
- All high and medium findings are fixed or rejected with documented proof.
- No release blocker remains and `CHANGELOG.md` is current.
