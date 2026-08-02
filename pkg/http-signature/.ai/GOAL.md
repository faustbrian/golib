# Goal: HTTP Message Signatures

## Objective

Build `http-signature` as a complete, interoperable, security-reviewed Go
implementation of HTTP Message Signatures and related digest fields for
`net/http` clients, servers, middleware, and proxies.

The implementation MUST target RFC 9421 and RFC 9530 without undocumented
divergences. At implementation time, pin the RFC text, verified errata, IANA
registries, official examples, and every normative requirement.

Authoritative starting points:

- https://www.rfc-editor.org/rfc/rfc9421
- https://www.rfc-editor.org/rfc/rfc9530
- https://www.rfc-editor.org/errata/rfc9421
- https://www.rfc-editor.org/errata/rfc9530

## Specification Commitment

Create a conformance matrix mapping every RFC 9421 and RFC 9530 MUST, MUST NOT,
SHALL, REQUIRED, SHOULD, and MAY to implementation, tests, and documentation.
Normative prose outranks examples. Ambiguities and errata MUST receive explicit
decisions rather than accidental behavior.

Support MUST include:

- covered HTTP fields and every registered derived component;
- component parameters and canonical signature-base construction;
- `Signature-Input`, `Signature`, and `Accept-Signature` parsing/serialization;
- multiple labels and signatures without map-order nondeterminism;
- request signatures, response signatures, and request-response binding;
- creation, expiration, nonce, algorithm, key ID, and tag parameters;
- Content-Digest and Repr-Digest generation and verification;
- trailers and supported structured-field behavior;
- application profiles defining mandatory coverage and policy.

## Architecture

Separate syntax, canonicalization, algorithms, key resolution, profile policy,
signing, verification, replay protection, and `net/http` integration. The core
MUST NOT perform hidden network access, choose permissive defaults, own key
storage, or treat a cryptographically valid signature as authorization.

Use explicit signer and verifier contracts with algorithm/key-type binding.
Profiles MUST define allowed algorithms, required covered components,
acceptable times, nonce policy, key resolution, external request context, and
error mapping as required by RFC 9421's application model.

## HTTP Semantics

Correctly handle field combination, Structured Fields, header/trailer
selection, raw versus reconstructed target information, proxies, HTTP/1.1,
HTTP/2, HTTP/3 semantics visible through `net/http`, multiple field values,
query parameters, authority, ports, escaping, and transformations permitted by
HTTP. The verifier MUST be given explicit trusted external-origin context when
the application is behind a proxy.

Body signing MUST compose with RFC 9530 digest fields and streaming. Define
buffering, maximum bytes, trailers, replayability, partial reads, compression,
content coding, retries, and body ownership. No helper may silently consume or
replace a caller body incorrectly.

## Algorithms And Keys

Implement only algorithms justified by the specification, interoperability,
and current security guidance. Every algorithm MUST have test vectors and
strict key compatibility. Support key IDs, rotation, bounded key resolvers,
cache freshness, cancellation, revocation, and remote-resolution failure
without embedding a generic PKI or secret manager.

Legacy Cavage draft signatures, AWS Signature V4, OAuth 1.0 signatures, and
vendor-specific schemes MUST be separate explicit compatibility adapters, not
accepted by the RFC 9421 parser.

## Replay And Failure Semantics

Provide explicit nonce/replay contracts and bounded in-memory plus durable
adapter seams. Define clock skew, expiration, nonce uniqueness, atomic consume,
key lookup, malformed versus unverifiable input, unsupported algorithms,
insufficient coverage, digest mismatch, and unknown backend outcomes.

Verification errors MUST be typed and safe enough for application-specific
HTTP problem mapping without disclosing keys, signature bases, credentials, or
sensitive message content.

## Integration

Provide explicit middleware and round-tripper adapters for `net/http`,
`http-client`, `http-middleware`, router, service, clock, secret-store, audit,
correlation, tenancy, telemetry, and capability profiles. Middleware ordering
relative to body limits, decompression, authentication, authorization, retries,
redirects, caches, and telemetry MUST be documented and executable.

## Verification

Use RFC examples, verified errata, generated vectors, independent
implementations, malformed Structured Fields, transformed messages, proxy
fixtures, and streaming bodies. Fuzz parsers and canonicalization. Run race,
leak, stress, fault-injection, timing-sensitive review, and bounded-resource
tests. Exact 100% statement coverage and 100% viable mutation kills are
REQUIRED.

## Documentation And Delivery

Document the RFC model, secure application profiles, required coverage,
algorithms, key rotation, replay prevention, proxies, body digests, middleware
ordering, failure mapping, migration from legacy schemes, FAQ, and complete
client/server examples. Add manifests, CI, benchmarks, changelog, notices, and
clean-consumer proof.

## Non-Goals

- replacing TLS, authentication, authorization, or capabilities;
- automatically trusting forwarded headers;
- inventing algorithms or a universal signing profile;
- silently accepting legacy draft formats as RFC 9421.
