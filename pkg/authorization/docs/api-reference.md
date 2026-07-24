# API and package reference

The Go API is the source of truth. Run `go doc` on a package for every exported
field and method; the checked-in API baseline detects incompatible changes.

## Core package

`github.com/faustbrian/golib/pkg/authorization` defines the shared contract:

- `Request`, `Subject`, `Resource`, and `Environment` carry typed inputs.
- `Value` constructors create closed string, bool, integer, float, time, IP,
  null, and string-set attributes. Accessors never coerce types.
- `Evaluator` is the I/O-free model boundary. `Authorizer` is the application
  decision boundary.
- `Decision` returns `Outcome`, `Reason`, bounded matched IDs, revision, and
  bounded trace. Truncation flags make incomplete explanations explicit. Only
  `Allow` is an authorization grant.
- `PolicyDefinition` and `NewSnapshot` create an immutable revision. Definitions
  can carry priority, activation windows, and non-sensitive metadata; snapshot
  revisions must be positive.
- `NewEngine`, `Decide`, `DecideBatch`, and `ReplaceSnapshot` provide coherent,
  bounded evaluation and optimistic atomic activation.
- `Combine` implements deny-overrides, allow-overrides, first-applicable, and
  priority-order semantics independently of the engine.
- `NewInstrumented` decorates an authorizer with failure-isolated decision
  events.

Errors are stable sentinels or typed wrappers. `ValidationError` identifies the
invalid field. `PolicyEvaluationError` identifies the failed policy, redacts
the underlying error text from its message, and supports `errors.Is` and
`errors.As`. Invalid requests, context cancellation, evaluator errors, invalid
outcomes, and evaluator panics all fail closed.

## Policy models

- `acl` indexes explicit subject or group grants and denies and can list a
  bounded set of concrete resource IDs.
- `rbac` validates roles, tenant-local inheritance, permissions, assignments,
  and assignment administration through `Manager`.
- `abac` provides a closed condition tree for equality, ordering, membership,
  sets, strings, CIDR, missing, null, and boolean composition.

Each model has `New`, `Evaluate`, `EvaluateBatch`, `Limits`, strict bounded
version-one documents, `EncodeDocument`, `DecodeDocument`, and a portable-policy
`Decoder`. Model evaluators return `NotApplicable`; the root engine supplies
default deny.

## Policy lifecycle

- `policy` validates and encodes the versioned manifest envelope, compiles
  registered model documents, diffs snapshots, dry-runs candidates, and
  synchronizes a repository into an engine. `Synchronizer.Decide` enforces a
  configurable maximum age for authoritative repository verification.
- `postgres` implements `policy.Repository` with one atomic manifest row and
  optimistic revisions. It exports SQL and `migrations` forms of the schema.
- `valkey` publishes a monotonic durable revision and uses pub/sub only as a
  wakeup hint.
- `authcache` configures an explicit typed `cache` manifest cache with exact
  revision loading. It is never a source of truth.

## Application adapters

- `authn` maps a structurally compatible authenticated principal into a typed
  subject without coupling the core engine to authentication middleware.
- `authhttp` is the canonical `net/http` package. `httpauth` remains the
  underlying standard-library implementation.
- `authrpc` supplies native `jsonrpc` fail-closed middleware.
- `authlog` emits bounded `log/slog` records and accepts `log` loggers.
- `authotel` records bounded OpenTelemetry metrics and spans and accepts
  providers managed by `telemetry`.
- `authorizationtest` supplies deterministic builders, evaluator fixtures,
  assertions, canonical decision JSON, and an authorizer conformance suite.

## Stability

Exported Go symbols follow semantic versioning. The portable manifest format
and each model document version are separate compatibility contracts. See
[compatibility and governance](compatibility.md) before changing either.
