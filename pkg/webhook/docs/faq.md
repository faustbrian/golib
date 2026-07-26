# FAQ

## Does this provide exactly-once webhooks?

No. It provides authenticated messages, atomic replay rejection, and
idempotency-aware retry primitives. Application effects still require their
own transaction and idempotency design.

## Can I decode JSON before verification?

No. Verify the exact received bytes first, then decode `VerifiedBody`.

## Why are redirects disabled?

A redirect can change the destination after validation and is a common SSRF
bypass. Resolve a new approved endpoint explicitly instead.

## Why did delivery attempt only once?

Retries require an idempotency key. Queue and outbox adapters deliberately use
one HTTP attempt because the durable layer owns retries.

## Which providers are supported?

None by preset in v1. Only the documented generic scheme is claimed.

## May I log a verification error?

Use `Observation` categories. Do not log raw headers, payloads, URLs, event
IDs, keys, signatures, or detailed errors from untrusted input.
