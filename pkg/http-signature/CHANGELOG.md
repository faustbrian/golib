# Changelog

All notable changes to this module are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Add an isolated equivalent request sign-and-verify benchmark against pinned
  `yaronf/httpsign`, with separate correctness proof and documented policy-cost
  differences.
- Convert Structured Fields dependency panics on hostile extension syntax into
  typed parse failures across signature, digest, and canonicalization entry
  points.
- Add isolated compatibility `RoundTripper` and verification middleware seams
  for Cavage drafts, AWS SigV4, OAuth 1.0, and explicitly named vendor schemes;
  these seams never expand RFC 9421 parser acceptance.
- Add streaming response digest/signature trailers and bounded client-side
  verification that waits for EOF before releasing response content.
- Fail streaming adapters on zero-progress body readers and reject protocol
  switching where digest/signature trailers cannot complete.
- Add deterministic RFC 9530 SHA-256 and SHA-512 integrity-field generation,
  strict byte-sequence parsing, immutable ordered values, and policy-selected
  constant-time verification.
- Add ordered RFC 9530 integrity-preference parsing and serialization with
  strict integer weights, duplicate rejection, and unknown-algorithm retention.
- Add explicit and default-bounded parser resource limits across signature,
  digest, and negotiation fields.
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
  compatibility, caller-owned randomness, cancellation, and secret-safe typed
  verification failures.
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
  round-tripper with request-response binding and untouched response bodies.
- Match `net/http` body suppression for `HEAD` and bodyless status codes when
  signing responses, including digesting only content actually emitted.
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
