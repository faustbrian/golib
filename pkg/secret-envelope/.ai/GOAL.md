# Goal: Bounded Secret Envelope Encryption

## Objective

Build `secret-envelope` as a production cryptographic envelope for bounded
application-owned secret payloads. It MUST use fresh one-use AES-256-GCM data
keys behind an explicit wrapping provider and provide a separate verify-only
signature boundary without becoming a secret store, rotation engine, IAM
manager, or authorization system.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Generate a fresh 256-bit data key and 96-bit cryptographic nonce per payload.
- Authenticate immutable bounded encryption context in local AEAD and provider
  wrapping; document that context is identifying metadata, not secret data.
- Define a stable, versioned, canonical, bounded binary envelope with strict
  parsing, unknown-version rejection, and migration hooks.
- Own and copy caller bytes deliberately; best-effort zero plaintext data keys
  and temporary plaintext while documenting Go/runtime zeroization limits.
- Redact text, JSON, structured logging, errors, panic paths, and diagnostics.
- Support context-aware key providers and explicit unknown-outcome, throttling,
  cancellation, unavailable-provider, malformed-key, and integrity errors.
- Keep asymmetric verification algorithm-explicit, message-bounded, signing-
  free, and separate from authorization decisions.
- Supply an optional AWS KMS adapter through narrow interfaces.

## Documentation And Completion

Document threat model, API, persistence format, context design, key ownership,
AWS KMS operation, rotation/migration, examples, adoption, FAQ, compatibility,
security reporting, and recovery. CI MUST require strict static checks, race,
fuzz, crypto vectors, interoperability, security scans, API compatibility,
benchmarks, docs, exactly 100% statement coverage, and exactly 100% of viable
mutants killed by meaningful tests.
