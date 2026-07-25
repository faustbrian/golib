# FAQ

## Is this a request validator or DTO framework?

No. Decode elsewhere, preserve presence when needed, then call typed
validators. The package owns no router, binder, serializer, ORM, or container.

## Why does `Required` take `Value[T]`?

Plain Go values cannot distinguish omitted, explicit null, and present zero.
`Value[T]` makes the caller's model explicit.

## Why are there byte and rune length rules?

Protocol limits are often bytes; user-visible character policies often need
Unicode code points. Neither is silently substituted for the other.

## Does Email prove deliverability or URL perform network access?

No. They are bounded syntax checks. External verification is async application
work.

## Why are map keys sorted?

Go map iteration is randomized. Sorting produces stable reports, fixtures, and
transport payloads.

## Can messages live in core?

Stable codes and parameters live in core. Applications supply catalogs because
prose, locale policy, and escaping belong at presentation boundaries.

## Can a custom validator crash validation or expose its panic value?

Function adapters contain panics and return `validator_panic` without the
payload. `AsyncAll` also isolates arbitrary async implementations. Wrap an
arbitrary synchronous interface implementation with `IsolatePanics` when
calling it directly. Panic containment does not make caller-owned state
mutation safe; custom code still requires ownership and review.

## Why did my custom violation become `invalid_violation`?

Machine codes, severity, and metadata are security-sensitive protocol data.
Invalid UTF-8, controls, oversized fields, excessive parameters, or an unknown
severity are replaced by one blocking diagnostic so malformed extension data
cannot bypass validation or inject unsafe output.

## Why no generated struct path?

Typed accessors already avoid reflection. Tag plans compile once. A generated
path would require conformance and maintenance without current benchmark proof.
