# FAQ

## Why not Go regexp?

Go `regexp` intentionally implements RE2 semantics. ECMAScript includes
backreferences, lookaround, UTF-16 indexing, different escapes, and different
replacement behavior, so translation would be a semantic approximation.

## Is matching linear time?

No. Full ECMAScript semantics require backtracking. Explicit budgets and
context cancellation bound execution.

## Does `Match` search the input?

No. It attempts exactly at `StartUTF16`. Use `Find` for an unanchored search.
The JSON Schema wrapper uses `Find` because schemas are not implicitly
anchored.

## Are programs safe to share?

Yes. `Program` is immutable. `Session` contains mutable `lastIndex` state and
must not be shared without synchronization.

## Why are spans in UTF-16?

ECMAScript strings and regular-expression indices are defined in UTF-16 code
units. Byte and rune mappings are included for Go callers.

## Which edition is used?

Exactly ECMAScript 2025. Future syntax is rejected until deliberately added.
