# API reference

The checked API snapshot is `api/baseline.txt`; `make api-compat` detects any
change. Go package documentation remains the exhaustive symbol-level source.

## Root package

- `Validator[T]` and `ValidatorFunc[T]`: deterministic, side-effect-free
  validation. Function adapters contain panics and discard panic payloads.
- `Context`: immutable locale, operation, safe metadata, limits, and path.
- `Value[T]`: explicit `MissingState`, `NullState`, or `PresentState`.
- `Path` and `Segment`: fields, indexes, keys, generic items, and RFC 6901
  pointers.
- `Violation` and `Report`: ordered, deduplicated, bounded findings.
- `All`, `Any`, `Not`, `When`, and `Dependent`: typed composition.
- `AsyncValidator[T]`, `AsyncValidatorFunc[T]`, and `AsyncAll`: separate
  cancellation-aware I/O validation with bounded concurrency.
- `IsolateAsyncPanics`: containment for arbitrary async implementations;
  `AsyncAll` applies it automatically.
- `IsolatePanics`: containment for arbitrary `Validator[T]`
  implementations; function adapters are contained automatically.

`Report.HasErrors` includes blocking findings omitted after truncation.
`Report.Err` returns `*InvalidError`, unwraps to `ErrInvalid`, and works with
`errors.Is` and `errors.As`. `ErrLimitExceeded`, `ErrInvalidLimit`, and
`ErrValidatorPanic` are stable root sentinels. `ErrInvalidViolation` marks a
custom diagnostic rejected for invalid severity, code, parameters, UTF-8, or
control characters. `structplan.ErrInvalidPlan` classifies malformed typed
plan or cache construction.

## Subpackages

- `rules`: reusable typed validators. See the [rule catalog](rules.md).
- `structplan`: reflection-free typed plans and optional strict tag plans.
- `validationrpc`: JSON-RPC `-32602` invalid-params projection with severity,
  truncation, and report-level blocking state.
- `validationjsonapi`: JSON:API documents, error objects, source pointers,
  severity, truncation, and blocking state.
- `validationhttp`: RFC 9457-style problems and router-neutral hooks.
- `validationconfig`: the small `Validate() error` config contract.
- `validationservice`: cancellation-aware service hooks and chains.
- `validationobserve`: bounded labels without paths, values, or parameters.
- `validationtext`: bounded, panic-safe, control-free, HTML-escaped
  application message catalogs that cannot alter machine path or code.
- `validationtest`: fixtures, consumer assertions, conformance tables, and
  mutation helpers.

All exported APIs have Go documentation. The core has no global registry and
no package-owned mutable singleton.
