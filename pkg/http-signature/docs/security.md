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
through its required callback and leaves the declared trailers absent. Clients
must wait for EOF and fail closed; servers must not claim that reporting can
retract or authenticate already-emitted bytes.

## Transformations

Signatures survive only transformations accounted for by canonicalization and
the selected components. Field combination, Structured Fields serialization,
default-port removal, query escaping, content coding, trailer removal, and
proxy target reconstruction can change observable inputs. Test the actual
proxy and HTTP-version path used in production.
