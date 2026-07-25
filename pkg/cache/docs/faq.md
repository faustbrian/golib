# FAQ

## Why is a miss not an error?

Absence is an expected cache state. `Result.State == Miss` stays distinct from
backend, decode, and loader failures.

## Why is `ErrMiss` exported?

It supports integrations that need an error-form miss, but the semantic read
API uses `Result.State`. Do not expect `Get` to return `ErrMiss`.

## Is loading deduplicated across services?

No. Flights are process-local. The module intentionally does not expose a
distributed lock as cache functionality.

## Does `Cache.Close` close Redis or Valkey?

No. The application owns supplied backends and native clients. Close the cache,
then the backend/client according to application lifecycle.

## Can I log backend keys?

Avoid it. They are hashed but remain stable high-cardinality identifiers.
Bundled observers do not expose them.

## Can stale-while-revalidate and stale-if-error be enabled together?

No. Their caller-visible precedence is ambiguous, so construction rejects the
combination.

## Why are memory entries bounded twice?

Entry count prevents metadata explosion; retained-byte limits prevent a small
number of large records from exhausting memory.

## Should I cache errors?

No. Return loader errors. Negative caching is only for authoritative absence.
