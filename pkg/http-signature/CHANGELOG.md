# Changelog

All notable changes to this module are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- Prove escaped quoted-string scanning before repairing a following integral
  RFC 8941 Decimal, killing the previously surviving conditional mutant.

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Added

- Add an auditable RFC 8941, RFC 9421, and RFC 9530 specification decision
  register with explicit compatibility and security consequences.
- Add isolated equivalent request sign-and-verify benchmarks against pinned
  `yaronf/httpsign` and `dadrus/httpsig`, with separate correctness proof,
  repeated samples, environment capture, and documented policy-cost
  differences.
- Convert Structured Fields dependency panics on hostile extension syntax into
  typed parse failures across signature, digest, and canonicalization entry
  points.
- Combine multiple Structured Field lines with the RFC-mandated comma and space
  before parsing, reject leading horizontal tabs outside RFC 8941
  optional-whitespace positions, and retain the required fractional zero when
  canonicalizing integral Decimal values.
- Add isolated compatibility `RoundTripper` and verification middleware seams
  for Cavage drafts, AWS SigV4, OAuth 1.0, and explicitly named vendor schemes;
  outbound callbacks cannot replace request identity or RFC 9421 signature
  fields, and inbound callback mutations cannot reach later RFC verification.
- Add streaming response digest/signature trailers with canonical application
  declarations, bounded authenticated late-trailer support, fail-closed
  protocol constraints, and client-side verification that waits for EOF before
  releasing response content.
- Make buffered response signing inherit outer ordinary headers with normal
  handler replacement semantics and reject handler-managed transfer or trailer
  framing, protocol switching, and successful `CONNECT` before signing.
- Fail streaming adapters on zero-progress body readers and reject protocol
  switching where digest/signature trailers cannot complete.
- Preserve application trailer values populated at EOF across buffered request
  digest generation and verification without losing caller-declared framing.
- Prevent streaming request downgrade by forcing supported HTTP/1 attempts to
  chunk even empty bodies, preserving and authenticating only predeclared EOF
  trailers, rejecting early responses and `CONNECT`, and refusing profiles that
  cover transport-dependent connection or framing fields.
- Clear handler-injected protected response trailers on every late streaming
  failure, omit mutable TLS state from verification callback snapshots, and
  report signed buffered-response short, invalid, or failed writes once through
  a redacted late diagnostic path.
- Add deterministic RFC 9530 SHA-256 and SHA-512 integrity-field generation,
  strict byte-sequence parsing, immutable ordered values, and policy-selected
  constant-time verification.
- Add ordered RFC 9530 integrity-preference parsing and serialization with
  strict integer weights, duplicate rejection, and unknown-algorithm retention.
- Add explicit and default-bounded parser resource limits across signature,
  digest, and negotiation fields.
- Bound complete signature-base canonicalization by default and permit a
  stricter per-message ceiling.
- Bind transport-owned Host, content-length, transfer-encoding, trailer, and
  connection components to deterministic `net/http` wire state, including
  method- and status-sensitive zero-length request and response emission,
  exact transfer-coding spelling and order, and response body-probe ambiguity,
  with an explicit received-versus-`Response.Write` transport mode that rejects
  ambiguous zero-value contexts, unavailable inbound trailer identity, and
  stale outbound header aliases.
- Reject noncanonical or multi-line Cookie coverage and require binary wrapping
  for multiple Set-Cookie field lines to prevent transformation collisions.
- Add strict ordered parsing for `Signature-Input` and `Signature`, rejecting
  duplicate labels, wrong Structured Fields member types, duplicate covered
  component identifiers, and RFC 9651-only values outside RFC 8941.
- Preserve RFC 8941 Item parameters on `Signature`, `Content-Digest`, and
  `Repr-Digest` members instead of rejecting legal extensions.
- Add ordered `Accept-Signature` parsing and serialization with distinct
  Boolean creation and expiration request semantics.
- Add request signature-base construction for registered derived components,
  header and trailer selection, field combination, explicit external request
  context, request-response component binding, and Structured Fields modes.
- Add all active IANA signature algorithms with exact RFC encodings, strict key
  compatibility, Go-managed cryptographically secure RSA-PSS and ECDSA
  randomness, cancellation, and secret-safe typed verification failures;
  retain the former caller-random inputs as ignored compatibility parameters so
  weak or blocking readers cannot affect signing.
- Add explicit verification profiles with mandatory coverage, parameter,
  algorithm, time, tag, key-resolution, cache-freshness, and nonce policy.
- Reject zero-length verification-key validity windows even when configured
  clock skew would otherwise make the instant appear acceptable.
- Add explicit signing profiles and context-bounded rotating-key providers that
  create deterministic matching field pairs without mutating messages or
  consuming bodies.
- Add profile-level mandatory trusted external request context that fails
  before signing-provider or verification-resolver access.
- Reject trusted external-origin contexts whose scheme or authority
  contradicts an absolute-form request target.
- Preserve double-slash origin-form paths and reject request-target fragments,
  user information, opaque targets, and absolute targets without authority.
- Reconstruct authority-form and asterisk-form target URIs with an empty
  path/query while retaining the normalized `/` value for `@path`, and reject
  special forms used with the wrong method.
- Normalize authority ports numerically so leading-zero default ports are
  omitted and other ports have a stable decimal representation.
- Reject authority values containing query, fragment, or opaque URI data.
- Add explicit outbound signing round-tripper and inbound verification
  middleware adapters with caller-owned label selection, trusted external
  request context, failure mapping, existing-field policy, and body ownership.
- Add bounded fail-closed response-signing middleware and a response-verifying
  round-tripper with request-response binding, authenticated digest coverage,
  callback isolation, transparent-decompression rejection, and replayable
  verified bodies; buffered digest and trailer paths reject 101 and successful
  `CONNECT` before reading opaque protocol or tunnel bytes.
- Match body suppression for `HEAD`, 204, 205, and 304 when signing responses,
  including rejecting RFC-forbidden 205 handler content and digesting only
  content actually emitted.
- Add explicit bounded Content-Digest round-tripper and verification
  middleware with caller-body closure, replayable clones, and no partial
  delegation after size or digest failure.
- Add a bounded streaming request adapter that computes Content-Digest while
  the transport reads, then signs the declared digest trailer at EOF without
  falsely advertising replayability.
- Add eager inbound trailer verification that waits for EOF, checks the
  bounded content digest, authenticates a profile-required trailer component,
  and only then delegates a replayable body.
- Fail buffered body processing closed when releasing caller-owned body
  resources fails, without exposing the underlying close error.
- Add an atomic bounded process-local replay store and a context-aware durable
  replay adapter contract with fail-closed capacity and backend semantics.
- Pin the RFC texts, errata records, and relevant IANA registries with explicit
  decisions for every currently listed erratum.

### Changed

- **Breaking:** `ResponseSigningMiddlewareConfig.ReportError` is now required.
  Callers must provide a concurrency-safe callback that records redacted late
  output failures; `MapError` remains responsible only for failures that can be
  mapped before response commitment.
