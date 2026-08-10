# FAQ and migration

## Is verification authentication or authorization?

No. It proves that an allowed key produced a signature over the selected
components and that the profile accepted it. The application still maps the
key to an identity and authorizes the operation.

## Does this replace TLS?

No. TLS protects a connection and provides confidentiality. Message signatures
can authenticate selected data across hops, but expose all unsigned metadata
and content.

## Should forwarded headers be trusted?

Never automatically. Supply external scheme, authority, and raw request target
from a separately authenticated proxy boundary and require that context in the
profile.

## Content-Digest or Repr-Digest?

`Content-Digest` hashes the actual HTTP content bytes, including content coding.
`Repr-Digest` hashes selected representation data and may differ for ranges,
HEAD, state-changing responses, and `Content-Location`. The generic HTTP body
adapters compute only `Content-Digest`; applications compute `Repr-Digest` from
the representation bytes whose semantics they own.

## Headers or trailers?

Headers allow validation policy before streaming but require buffering when a
body-dependent value is not already known. Trailers allow streaming but can be
dropped by intermediaries and cannot protect eager content processing. Use
trailers only when every hop preserves them and the receiver waits for EOF.

## How are retries handled?

The buffered client adapter supplies `GetBody`; ordinary retry logic can create
a fresh attempt from the clone. The streaming trailer adapter is deliberately
one attempt. Recreate the request and body for each retry and use application
idempotency for non-idempotent operations.

## How do I migrate a Cavage draft implementation?

Treat it as a separate protocol. Do not feed draft `Authorization: Signature`
or `(request-target)` syntax into this parser. During migration, negotiate an
explicit adapter from the `compatibility` package at the application boundary,
emit RFC 9421 fields separately, and remove the legacy path after all peers
migrate. The compatibility adapter accepts a caller-supplied implementation; it
does not reinterpret the legacy format as RFC 9421. AWS Signature V4, OAuth 1.0,
and named vendor schemes use the same isolated boundary.

## Why are deprecated digest algorithms rejected for computation?

RFC 9530 prohibits deprecated algorithms in adversarial settings such as a
signed integrity field. The parser retains unknown or deprecated preference
keys for negotiation, while local computation and required verification are
restricted to active SHA-256 and SHA-512.
