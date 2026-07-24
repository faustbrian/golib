# Rule catalog

All rules are advisory by default. A repository may promote a rule through the
versioned `rules` map only after corpus evidence and migration work. The exact
diagnostic location is the violating import, declaration, call, field, or
statement. Suggested fixes are intentionally absent unless semantics can be
preserved mechanically.

Every suppression must use the exact rule ID on the line immediately before
the diagnostic and include a reason. Prefer configuration or remediation over
suppression.

## `api/backend-error-boundary`

Owner: `platform-architecture`. Severity: `error`. Default status: `advisory`.
golangci-lint `errorlint` remains authoritative for generic `errors.Is`,
`errors.As`, and wrapping syntax. This rule owns configured backend provenance
at exported API boundaries, so `errorlint` remains enabled.

Configure exact packages or trailing `/...` trees under
`backend_error_boundaries`. Configure exact backend functions or
`Type.Method` symbols and zero-based results under `backend_error_sources`.
Wrappers that preserve backend identity, such as `fmt.Errorf` with `%w`, belong
under `backend_error_passthroughs` with their result and input positions.

```go
func Load() error {
    return backend.Load() // bad: backend contract escapes
}

func Load() error {
    if err := backend.Load(); err != nil {
        return ErrUnavailable // good: stable package-owned classification
    }
    return nil
}
```

The analyzer uses SSA to follow configured results through local assignments,
branches, interface conversions, generic calls, methods, and configured
wrappers. It reports only exported functions and exported methods on exported
receiver types in configured boundary packages. Calls through dynamic
interfaces, heap-stored errors, and unlisted transformation helpers are not
guessed at. An unlisted helper therefore forms a reviewable classification
boundary. Add a preserving helper to passthrough policy; otherwise map the
failure to a stable public error. Suppress only an exact compatibility return
whose backend identity is intentionally public and documented.

## `api/forbidden-call`

Owner: `platform-architecture`. Staticcheck SA1019 remains authoritative for
documented Go deprecations; this rule owns organization-specific migrations.

Configure exact package functions or `Type.Method` symbols, a replacement, and
optional reviewed caller packages under `forbidden_apis`.

```go
legacy.NewClient() // bad: configured legacy API
adapter.NewClient() // good: configured replacement
```

The analyzer resolves aliases, dot imports, methods, and generic calls through
type information. Suppress only a reviewed call that cannot yet migrate.

## `api/interface-naming`

Owner: `platform-architecture`. Severity: `warning`. Default status:
`advisory`. Staticcheck ST1003 remains authoritative for Go initialism
spelling. This rule owns explicitly configured architectural-role prefixes and
suffixes, so ST1003 remains enabled.

Configure non-overlapping exact packages or trailing `/...` trees under
`interface_names`. Each policy requires an exported `required_prefix`, an
exported `required_suffix`, or both. `allowed_names` contains narrow exact
compatibility names.

```go
type Client interface { Call() } // bad when suffix Port is required
type ClientPort interface { Call() } // good configured role
type Number interface { ~int | ~int64 } // good: generic constraint
```

The analyzer resolves aliases and completed type sets through type information.
It reports only exported package-scope interfaces usable as runtime values;
unexported interfaces, concrete types, and constraint-only interfaces are
accepted. Rename the declaration to communicate its configured role. Reserve
an exact allowed name or suppression for a reviewed public compatibility
contract rather than weakening the package policy.

## `api/interface-placement`

Owner: `platform-architecture`. Severity: `warning`. Default status:
`advisory`. golangci-lint `ireturn` remains authoritative for return-site
interface policy, and `interfacebloat` remains authoritative for method-count
thresholds. This rule owns configured declaration placement, so both adjacent
linters can remain enabled.

Configure exact provider package paths or trailing `/...` package trees under
`interface_provider_packages`. Overlapping entries are rejected.

```go
// bad in a configured provider package
type Client interface { Call() }

// good in the consuming package
type client interface { Call() }

// good: a generic constraint cannot be used as a runtime value
type Number interface { ~int | ~int64 }
```

The analyzer resolves exported aliases and embedded interfaces through type
information. It reports only package-scope interfaces whose completed type set
is a method set, meaning they can be used as runtime values. Unexported
interfaces, local declarations, concrete types, and constraint-only interfaces
are accepted. Move the smallest required interface to its consumer and accept
it at the use site. Suppress only an exact reviewed declaration whose provider
ownership is part of a stable public compatibility contract.

## `architecture/import-boundary`

Owner: `platform-architecture`. This suite is authoritative over overlapping
depguard package policy; disable duplicate depguard entries.
`gomodguard_v2` remains authoritative for direct `go.mod` module allowlists,
blocklists, replacements, and version constraints. Keep it enabled beside this
rule; module identity and package dependency direction are separate contracts.

Configure package `pattern`, `deny_imports`, and narrow `allow_imports` entries.
For organization-wide direction, assign non-overlapping package trees to
`layer` and `context`, then define explicit top-level `layers` and `contexts`
with `may_import` edges. For target-owned dependencies such as SQL drivers or
backend SDKs, configure each package tree and its approved adapter trees under
`backend_clients`.

```go
import "example.com/service/infrastructure/sql" // bad in domain
import "example.com/service/domain/ports" // good dependency direction
import "database/sql" // bad outside its configured adapter
```

Imports within the same layer or context are implicitly allowed. A direction
is enforced only when both packages are classified for that dimension;
unclassified external packages are not inferred. Configured layer and context
graphs must be acyclic; validation emits a stable cycle trace for conceptual
cycles that Go's package import-cycle check cannot observe. Explicit deny
policy wins at the same import location, and overlapping source policies or
classifications are configuration errors rather than precedence rules. Exact
imports and `...` package trees are supported. Restricted backend client trees
and their approved adapter trees must also be non-overlapping. The restriction
is checked at the import itself, so aliases, dot imports, generic APIs, and
intermediate wrapper packages cannot bypass it. Use an allowed adapter or
invert the dependency; suppress only the import declaration under review.

## `context/blocking-api-context`

Owner: `platform-runtime`.

List exported functions or `Type.Method` symbols in `blocking_functions` on an
exact package policy.

```go
func Load(id string) error // bad: configured blocking API
func Load(ctx context.Context, id string) error // good
```

Context-compatible parameter types are resolved semantically. Add and
propagate the caller context instead of creating a new root.

## `context/no-background`

Owner: `platform-runtime`.

`entrypoints` lists the exact composition-root packages allowed to create root
or detached contexts. Recognized `_test.go` files are also accepted boundaries.

```go
result, err := load(context.Background()) // bad below a root
detached := context.WithoutCancel(ctx) // bad: cancellation and deadline removed
result, err := load(ctx) // good propagation
```

`context.Background`, `context.TODO`, and `context.WithoutCancel` are resolved
through type information, including aliases and dot imports. `go vet`
`lostcancel` remains authoritative for dropped cancellation functions; it does
not report deliberate detachment with `WithoutCancel`.

## `context/no-stored-context`

Owner: `platform-runtime`.

`context_owners` lists exact reviewed packages that may retain a context.

```go
type Service struct { ctx context.Context } // bad hidden lifetime
func (Service) Load(ctx context.Context) {} // good operation scope
```

Embedded, aliased, and context-compatible field types are checked. Pass
context per operation unless the type has an explicit reviewed ownership
contract.

## `http/no-default-client`

Owner: `platform-runtime`. golangci-lint `noctx` remains authoritative for
contextless HTTP convenience calls; this rule owns direct shared globals.

```go
response, err := http.DefaultClient.Do(request) // bad shared ownership
response, err := client.Do(request) // good injected client
```

Both `http.DefaultClient` and `http.DefaultTransport` are resolved as typed
package variables. The exact standard-library ownership transfer
`http.DefaultTransport.(*http.Transport).Clone()` is accepted; other direct
uses retain shared process-global state. Construct and inject an explicit
client and cloned transport with reviewed timeout and shutdown policy.

## `http/client-timeout`

Owner: `platform-runtime`. golangci-lint `noctx` remains authoritative for
outbound request context, while gosec G114 remains authoritative for HTTP
server timeouts. This rule owns explicit `http.Client` timeout construction.

List exact packages with reviewed unbounded or streaming ownership under
`http_timeout_exceptions`.

```go
client := &http.Client{} // bad: no explicit timeout policy
client := &http.Client{Timeout: 10 * time.Second} // good bounded client
```

The analyzer resolves `net/http.Client` through type information, including
aliases and dot imports. It reports keyed literals with missing or
statically non-positive `Timeout`, `new(http.Client)`, and explicit zero-value
client declarations. Non-constant timeout expressions are accepted as
explicit configuration. Unkeyed external struct literals remain `go vet`'s
authority. Construct the client atomically with a positive timeout; use an
exact package exception only for reviewed streaming semantics.

## `lifecycle/no-constructor-goroutine`

Owner: `platform-runtime`.

Configure exact package constructor functions or `Type.Method` symbols under
`constructors`.

```go
func New() *Worker { go run(); return &Worker{} } // bad hidden startup
func New() *Worker { return &Worker{} } // good explicit Start follows
```

The rule follows directly executed constructor flow, including immediate and
deferred function literals. It deliberately ignores stored or returned
callbacks because their execution is not established.

## `lifecycle/no-global-goroutine`

Owner: `platform-runtime`. Severity: `error`. Default status: `advisory`.

```go
var ready = func() bool { go serve(); return true }() // bad: hidden startup
var callback = func() { go serve() } // good: not executed by initialization
```

The analyzer follows only code proven to execute directly from package-level
variable initializers, including immediately invoked and deferred function
literals. Stored callbacks and calls through named helpers are not followed
because their execution cannot be established locally. Move startup behind an
explicit constructor or `Start` method with cancellation, waiting, and error
ownership. Suppress only an exact reviewed compatibility initializer.

## `lifecycle/cleanup-ownership`

Owner: `platform-runtime`. `bodyclose` remains authoritative for HTTP response
bodies, and `sqlclosecheck` remains authoritative for `database/sql`
resources. Do not duplicate those contracts here.

Configure exact package functions or `Type.Method` symbols under
`resource_constructors`, with the zero-based `cleanup_result` position and
optional reviewed caller packages.

```go
resource, _, err := resourceapi.Open() // bad: cleanup ownership is lost
resource, cleanup, err := resourceapi.Open() // good: caller owns cleanup
```

The analyzer resolves aliases, dot imports, methods, and generic calls through
type information. It reports only a blank cleanup tuple position or a call
whose results are wholly discarded. Returning or binding the cleanup result
preserves ownership; the rule does not guess whether later control flow calls
it. Bind and call, defer, or explicitly transfer cleanup to the resource owner.

## `lifecycle/transaction-rollback`

Owner: `platform-runtime`. Severity: `error`. Default status: `advisory`.
`sqlclosecheck` remains authoritative for rows, statements, named statements,
and pgx query results; it does not establish transaction rollback ownership.

Configure exact transaction functions or `Type.Method` symbols under
`transactions`, with the zero-based transaction `result` and exact
`rollback_method`.

```go
tx, err := db.BeginTx(ctx, nil)
if err != nil {
    return err
}
defer tx.Rollback() // good: established before transactional work

tx, err := db.BeginTx(ctx, nil) // bad: no immediate rollback owner
if err != nil {
    return err
}
return tx.Commit()
```

The analyzer resolves aliases, dot imports, generic functions, and methods
through type information. It accepts an exact deferred rollback immediately
after construction, or after one exact `err != nil` guard whose final statement
returns or branches out. An immediate return of the transaction transfers
ownership to the caller. Conditional defers, helper defers, work before the
defer, non-terminating guards, discarded transaction results, and commit-only
paths are diagnostics. This deliberately narrow structural contract avoids
guessing through arbitrary helpers or interprocedural ownership. Use an exact
suppression only when another reviewed abstraction establishes rollback at the
diagnostic location.

## `lifecycle/lock-across-call`

Owner: `platform-runtime`. `go vet` `copylocks` remains authoritative for lock
copying, and Staticcheck SA2001 remains authoritative for empty critical
sections. This rule owns configured callback and I/O calls under held locks.

Configure exact package functions or `Type.Method` symbols under
`lock_sensitive_calls`, with optional reviewed caller packages.

```go
mutex.Lock()
backend.Call() // bad: configured I/O while the lock is held
mutex.Unlock()

state := copyState()
mutex.Unlock()
backend.Call(state) // good: external control runs after unlock
```

The analyzer resolves `sync.Mutex` and `sync.RWMutex` operations through type
information and performs forward CFG must-analysis. It reports only when the
same lock identity is held on every path reaching the configured call. Locks
acquired on only one branch, or released on any reaching branch, are accepted.
Deferred unlocks keep ownership active through the function. Deferred calls,
goroutine calls, nested function bodies, and lock expressions whose instance
identity cannot be established are not guessed at. Analysis fails
deterministically when one function exceeds 4,096 CFG blocks or 256 distinct
lock identities instead of exploring unbounded state. Split such a function
into explicit lifecycle operations. Otherwise, copy required state, unlock,
and invoke the external operation afterward.

## `lifecycle/no-init`

Owner: `platform-runtime`.

`init_packages` lists exact reviewed packages allowed to use package
initialization.

```go
func init() { register() } // bad hidden ordering
func NewRegistry() *Registry { return register() } // good explicit setup
```

Move work into an explicit constructor invoked by the composition root.

## `lifecycle/no-process-control`

Owner: `platform-runtime`.

`entrypoints` lists exact packages allowed to terminate or panic. Elsewhere,
the rule detects `panic`, `log.Fatal` variants, and `os.Exit` semantically.
Recognized `_test.go` files and generated test-main packages are accepted test
boundaries because they are not shipped library process control.

```go
log.Fatal(err) // bad in a library
return fmt.Errorf("load: %w", err) // good caller-owned failure
```

Return a classified error and let the approved process entrypoint select the
exit code after cleanup. Keep intentional panic fixtures in test files rather
than production helpers.

## `lifecycle/unbounded-goroutine-fanout`

Owner: `platform-runtime`. Severity: `warning`. Default status: `advisory`.
`go vet` `loopclosure` remains authoritative for loop-variable capture, and
Staticcheck SA2000 remains authoritative for `WaitGroup.Add` placement. This
rule owns organization-configured concurrency fan-out bounds, so both existing
checks remain enabled.

Configure exact packages or trailing `/...` trees and a `max_static` value from
1 through 1024 under `goroutine_fanout`.

```go
for _, item := range request.Items {
    go process(item) // bad: concurrency scales with request size
}

jobs := make(chan Item)
for range 8 {
    go worker(jobs) // good: fixed worker count
}
```

The analyzer multiplies nested loop bounds and recognizes constant integer
ranges, arrays, literal slices and maps, constant strings, and canonical
incrementing or decrementing counter loops. Runtime-sized slices, maps,
channels, counters, infinite loops, and static products above policy are
reported. It follows immediately invoked and deferred function literals but
does not guess through stored callbacks.

Two synchronization forms are accepted only with typed identity evidence: a
positive `sync.WaitGroup.Add` before the launch with `Done` in the closure and
`Wait` before the next iteration, or a statically sized buffered-channel token
sent before the launch and received by a deferred closure. The synchronization
bound must not exceed `max_static`. Other limiter abstractions require an exact
suppression until the rule has a typed configuration contract for them. Prefer
a fixed worker pool; suppress only a reviewed launch whose external limiter is
documented at that location.

## `observability/high-cardinality-label`

Owner: `platform-observability`. Severity: `warning`. Default status:
`advisory`. golangci-lint `promlinter` remains authoritative for Prometheus
naming and help conventions; this rule owns organization-configured typed
cardinality flows, so `promlinter` can remain enabled.

Configure exact named types under `metric_label_types`. Configure exact metric
functions or `Type.Method` symbols under `metric_label_sinks`, with zero-based
`arguments` and optional `variadic_from` positions.

```go
type UserID string

counter.Label(string(userID)) // bad: direct conversion preserves evidence
counter.Label(Bucket(userID)) // good: explicit bounded category
counter.Label("anonymous") // good: statically bounded value
```

Aliases, dot imports, methods, generic calls, and pointer, slice, array, or map
containers retain configured type evidence. Direct type conversions are
followed to their source, preventing a `string(id)` bypass. Calls through
stored function values and arbitrary helper functions are not guessed at; an
explicit helper therefore represents a reviewable normalization boundary.
Configure only types whose corpus evidence establishes unsafe cardinality, and
replace them with bounded categories rather than suppressing the label. An
exact suppression is reserved for a reviewed sink whose backend applies a
provable bound.

## `observability/dynamic-label-name`

Owner: `platform-observability`. Severity: `warning`. Default status:
`advisory`. `promlinter` remains authoritative for static Prometheus naming and
help conventions; this rule owns typed attacker-controlled data reaching
configured label-name positions.

Configure exact named input types under `metric_label_name_types`. Configure
exact metric functions or `Type.Method` symbols under
`metric_label_name_sinks`, with zero-based `arguments` and optional
`variadic_from` positions.

```go
type MetricName string

counter.LabelName(string(requestName), "value") // bad: request controls schema
counter.LabelName(allowedName(requestName), "value") // good: fixed allowlist
counter.LabelName("method", "GET") // good: static schema
```

The analyzer uses the same typed evidence and direct-conversion tracking as
`observability/high-cardinality-label`. Calls through stored function values
and arbitrary mapping helpers are not guessed at; a helper is therefore the
explicit review boundary where an allowlist must be established. Configure
only types whose ingress contract proves attacker control. Keep the rule
advisory until the full corpus establishes that those type annotations are
complete and precise.

## `safety/no-mutable-global`

Owner: `platform-architecture`. This rule owns typed shared-state policy;
golangci-lint `gochecknoglobals` owns a blanket style prohibition. Disable
`gochecknoglobals` for packages governed by this rule to avoid duplicate
diagnostics.

Opt exact package paths or trailing `/...` package trees into the rule under
`mutable_globals`. Overlapping package policies are rejected. With no package
policy the analyzer is inactive, including in raw vettool execution.

```go
var clients = map[string]*Client{} // bad shared composite state
var defaultPort = 443 // accepted scalar value
var ErrClosed = errors.New("closed") // accepted error sentinel
```

The analyzer uses declared and inferred types. It reports package variables
whose effective type is a pointer, slice, map, channel, function, non-error
interface, struct, array, `unsafe.Pointer`, or a named or aliased form of one.
Constants, local variables, basic scalar globals, named scalar globals, and
the predeclared `error` interface are accepted. This deliberately narrower
contract is the semantic gap over a blanket no-globals rule; it does not guess
whether a composite value happens to remain unchanged at runtime. Construct
the state explicitly and inject its owner. Suppress only an exact reviewed
declaration, such as a concurrency-safe immutable cache, with an ownership
rationale.

## `security/no-unsafe`

Owner: `platform-security`. This suite owns the policy prohibition; gosec G103
may remain advisory for detailed unsafe usage.

```go
import "unsafe" // bad production safety bypass
import "encoding/binary" // good safe representation API
```

The rule also detects cgo imports and `go:linkname`. Isolate a genuinely
required bridge behind a narrow, reviewed suppression and migration issue.

## `security/sensitive-sink`

Owner: `platform-security`. gosec G101 remains authoritative for hardcoded
credentials; this rule owns typed values flowing into configured sinks.

Configure exact named `sensitive_types` and sink functions or methods with
zero-based `arguments`, optional `variadic_from`, and reviewed caller packages.

```go
slog.Any("token", token) // bad when Token and slog.Any are configured
slog.String("token", token.Redacted()) // good explicit representation
```

Pointers, arrays, slices, and maps containing configured types retain the
sensitive evidence. Explicit conversion to an unannotated type is not guessed
at; organizations should expose reviewed redaction APIs instead.
