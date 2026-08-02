# Goal Harden: Bounded Secret Envelope Encryption

## Mission

Audit cryptography, persistence, provider failure, memory ownership, and secret
redaction under hostile input and operational failure.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Threat-model nonce reuse, context substitution, ciphertext malleation,
  cross-record replay, key confusion, parser bombs, provider compromise, and
  disclosure through every diagnostic path.
- Use known-answer and negative AEAD vectors; fuzz every envelope/context parser
  for truncation, noncanonical values, overflow, allocation limits, versions,
  duplicate fields, and trailing data.
- Prove nonce uniqueness under concurrency and injected entropy failure; never
  fall back to deterministic or weak randomness.
- Inject KMS throttling, denial, malformed keys, wrong context, cancellation,
  timeout, credential rotation, partial responses, and unknown outcomes without
  unsafe hidden retries.
- Audit all byte aliases, copies, temporary buffers, zeroization attempts,
  serialization methods, errors, logs, traces, metrics, examples, and fixtures.
- Race concurrent encrypt/decrypt/verify calls and hostile provider callbacks;
  prove no panic, deadlock, goroutine leak, or cross-request state.
- Test format migration, unknown versions, key rotation procedures, and
  Kubernetes termination during provider operations without corrupt output.
- Benchmark bounded payload sizes, parse rejection, provider-call overhead
  seams, allocations, and memory retention.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests, external cryptographic review of material
design changes, and no unresolved security or lifecycle finding.
