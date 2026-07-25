# Guides

## Typed structs

Typed plans do not use reflection:

```go
type User struct { Name string }
builder := structplan.New[User](validation.DefaultLimits())
_ = structplan.Add(builder, "name", func(user User) string { return user.Name },
	rules.RuneLength(2, 80))
plan, err := builder.Compile()
```

`Compile` snapshots the builder. Later additions do not mutate earlier plans.
Names must be unique and `MaxStructFields` applies.
Empty or oversized names and nil accessors, validators, builders, or caches
are rejected at construction. Accessor and validator panics become
field-local `validator_panic` findings without retaining panic payloads.

## Optional tags

```go
type User struct {
	Name  string `validate:"required,min=2,max=80"`
	Email string `validate:"required,email"`
}
plan, err := structplan.CompileTags[User](limits)
```

The grammar is comma-separated `required`, `email`, `min=N`, and `max=N`, or
the complete tag `-`. Compilation rejects empty entries, duplicate rules,
unknown rules, invalid non-negative integers, tagged inaccessible fields,
cycles, excessive depth, long tags, and too many fields. Untagged exported
structs and pointers are nested. Nil pointers make `required` fail and skip
non-required rules; non-nil tagged pointers validate their underlying value.
Dynamic interface values use their concrete value. Aliases use their
underlying kind. Arrays, maps, and slices use length and honor
`MaxCollectionSize`; integers, unsigned integers, and floats use numeric
bounds. Plans are
immutable. `Cache` is instance-owned, bounded, concurrency-safe, and clearable.

## Composition and cross-field rules

Use `All` for independent constraints and `Dependent` when later checks are
meaningful only after a prerequisite. `FieldsEqual`, `RequiredWhen`, and
`ExcludedWhen` receive typed accessors; field names are used only as output
paths. This prevents reflective lookup from reading the wrong field.

## Async and I/O validation

Implement `AsyncValidator[T]` for database, network, or queue checks. Call
`AsyncAll(ctx, validationContext, value, validators...)`; it runs no more than
`MaxCustomConcurrency` validators concurrently, stops scheduling after
cancellation, and merges completed results in declaration order. Validators
already running must honor `context.Context`. Never hide I/O in `Validator[T]`.

## Errors

```go
if err := report.Err(); errors.Is(err, validation.ErrInvalid) {
	var invalid *validation.InvalidError
	if errors.As(err, &invalid) { /* inspect invalid.Report() */ }
}
```

Default formatting exposes only path, code, counts, and truncation. Causes and
parameters are available only through explicit accessors.

## Localization

Implement `validationtext.Catalog`. `Messages` passes locale, stable code, and
a defensive copy of safe parameters to the application catalog. Missing
messages remain empty. Translation cannot replace codes or paths.

## JSON-RPC, JSON:API, and HTTP

- `validationrpc.InvalidParams(report)` returns code `-32602` with ordered data,
  truncation, and report-level blocking state.
- `validationjsonapi.Errors(report)` returns a document with error severity,
  source pointers, truncation, and report-level blocking state.
- `validationhttp.FromReport(report)` returns a problem document;
  `WriteProblem` writes it with `application/problem+json`.

These packages do not bind requests, choose routes, or own transport control
flow. `validationhttp.Hook[T]` is an optional router integration seam.

## Config and service boundaries

`validationconfig.CheckValue` implements the small `Validate() error` contract
used by configuration loaders. `validationservice.Hook[T]` and `Chain` provide
cancellation-aware boundary validation without depending on service types.

## Custom validators and observation

Adapt a function with `ValidatorFunc[T]`. Package-owned validators do not
panic. If an application validator is not trusted, wrap it explicitly with
`IsolatePanics`; the panic payload is discarded. `validationobserve.Report`
emits code, severity, and operation only—never paths, parameters, causes, or
values—so it can back log or telemetry adapters safely.
