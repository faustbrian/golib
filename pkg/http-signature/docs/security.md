# Security model and profile guidance

## Profile decisions

Every deployment profile must define:

- allowed signature algorithms and exact key types;
- mandatory covered components, including method, target authority/path or
  target URI, digest fields, and relevant representation metadata;
- creation, expiration, clock skew, and maximum age;
- nonce presence, uniqueness scope, atomic consumption, and durable failure
  semantics;
- key identifier interpretation, bounded resolution, rotation overlap,
  revocation freshness, and unknown-backend behavior;
- whether trusted external request context is mandatory;
- signature label selection, acceptable tags, error-to-problem mapping, and
  safe audit categories.

There is deliberately no universal default profile. Profiles should cover the
smallest complete semantic statement required by their protocol. Covering a
body digest without `Content-Type`, `Content-Encoding`, and other relevant
representation metadata can leave denial-of-service and interpretation gaps.

## Keys and algorithms

The `alg` parameter is untrusted. Verification resolves an application-allowed
algorithm, binds it to the resolved key, and rejects disagreement. Resolvers
must return a bounded freshness interval and validity interval and must never
include identifiers or backend details in errors. Remote resolution runs under
the profile deadline and has no package-owned network path.

HMAC secrets are copied into an opaque value. The package does not claim secure
zeroization in garbage-collected Go memory. Do not log or serialize key values.

## Replay

`MemoryReplayStore` is linearizable only inside one process. Its capacity and
TTL are explicit and capacity exhaustion fails closed. Multi-instance services
need a durable adapter whose consume operation is atomic for `(keyid, nonce)`.
Timeouts and unknown commits must be treated as unknown outcomes, never as
success. Do not blindly retry a protected side effect after an unknown result.

## Failure disclosure

`VerificationError` exposes a stable category but omits identifiers, nonces,
signature bytes, bases, resolver errors, and message content. Applications
should map categories to coarse HTTP problems and log only category,
correlation, tenant, and policy identifiers. Authentication and authorization
failures should remain indistinguishable where account enumeration is a risk.

Streaming response signing has a distinct failure boundary: final signing can
fail after bytes are emitted. `TrailerResponseSigningMiddleware` reports this
through its required callback and clears protected trailer values so the
message remains incomplete. Buffered `ResponseSigningMiddleware` also requires
a late `ReportError` callback: a short, invalid, or failed write after signed
headers commit is reported once as `ErrBodyRead` without another write or
status attempt. Clients must wait for complete authenticated content and fail
closed; servers must not claim that reporting can retract or authenticate
already-emitted bytes.

## Transformations

Signatures survive only transformations accounted for by canonicalization and
the selected components. Field combination, Structured Fields serialization,
default-port removal, query escaping, content coding, trailer removal, and
proxy target reconstruction can change observable inputs. Test the actual
proxy and HTTP-version path used in production.

The package derives deterministic transport-owned fields from `net/http`
state, not stale `Header` aliases. It deliberately rejects inbound `Trailer`
declaration coverage because Go discards its field-line order. Profiles must
set an explicit User-Agent before covering it and must use an ASCII wire Host;
the package does not guess Go's downstream default User-Agent or IDNA mapping.
Use one semicolon-space-canonical Cookie field value, and use `bs` when
covering multiple Set-Cookie field lines.

Buffered response integrations never interpret bytes after a 101 or successful
`CONNECT` as HTTP content. Signing rejects those responses before commitment;
digest and trailer verification rejects and closes them before reading the
upgraded connection or tunnel.

RFC 9110 forbids server-generated content on 205 responses. Buffered response
signing rejects handler body writes and authenticates empty content; streaming
response signing rejects 205 before commitment because its mandatory trailers
cannot be emitted on a bodyless response.
