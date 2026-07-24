# Troubleshooting

## A required zero value passes or fails unexpectedly

Check whether decoding used `Value[T]`. `Required` rejects present empty values;
`Present` accepts them; `ZeroValue` tests Go-zero independently. Review the
[presence table](semantics.md).

## JSON:API points to the wrong field

Build the path from typed segments and use `JSONPointer`, not `Path.String`.
The projection does this automatically. Map keys containing `/` and `~` are
escaped.

## A tag plan fails at startup

Unknown and duplicate rules are intentionally rejected. Supported grammar is
`required`, `email`, `min=N`, `max=N`, or `-`. Recursive types, tagged
unexported fields, invalid bounds, and resource-limit violations also fail.

## A report says `path_limit`

The original location exceeded `MaxPathLength` and was discarded so an
attacker-controlled path cannot grow diagnostics. Raise the limit only after
reviewing protocol and logging budgets.

## A report says `string_limit`

The input exceeded `MaxStringLength` before a parser, matcher, comparison,
sort, or hash operation. Lower protocol limits are safe; raise the limit only
after reviewing memory and runtime budgets. The rejected value is never copied
into the finding.

## A report says `invalid_violation` or `validator_panic`

`invalid_violation` means a custom diagnostic supplied an unsafe severity,
code, parameter set, UTF-8 sequence, or control character. Validate extension
output at its source. `validator_panic` means custom validation panicked; the
panic payload is intentionally unavailable to prevent secret disclosure.

## A translated message is empty or escaped

Catalog output is omitted when it panics, exceeds `MaxStringLength`, contains
invalid UTF-8, or includes control characters. Accepted output is HTML-escaped.
Machine code, path, severity, order, and blocking state are never translated.

## A projected document is truncated

Inspect both truncation and blocking state. `Report.HasErrors`, HTTP status,
JSON-RPC `data.has_errors`, and JSON:API `meta.has_errors` remain fail-closed
even when the retained finding is only a warning.

## Async validation stops early

Inspect `context.Context` cancellation/deadline and
`MaxCustomConcurrency`. Already-running validators must honor cancellation.

## Coverage is 99.9%

Run `go tool cover -func=coverage.out` after generating a profile, locate the
specific branch, and add a defect-detecting boundary test. The gate requires
exactly 100.0%.

## Mutation or fuzz checks fail

Mutation output names the surviving semantic category. Fuzz output writes a
reproducible corpus case; rerun the exact command printed by Go, distinguish a
test-oracle error from a package defect, and retain useful minimized inputs.
