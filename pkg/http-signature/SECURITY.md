# Security policy

## Reporting

Report suspected vulnerabilities with GitHub's private security-advisory
workflow. Use generated test keys and sanitized messages. Do not include live
keys, credentials, nonces, signature bases, message bodies, or resolver output.

## Boundary

This module authenticates selected HTTP message components under an explicit
application profile. Operators still own TLS, caller authentication,
authorization, trusted proxy configuration, key generation and storage,
rotation and revocation freshness, durable replay coordination, audit
redaction, deployment time synchronization, and incident response.

A cryptographically valid signature is not proof that the signer is authorized
for the requested operation. Digest fields alone do not prevent malicious
tampering unless their values and relevant representation metadata are
authenticated.

## Supported use

- Require active algorithms and keys of the exact corresponding type.
- Require creation and expiration times unless a reviewed protocol supplies an
  equivalent freshness mechanism.
- Require atomic durable nonce consumption when replays can cross processes.
- Require trusted external request context behind any origin-rewriting proxy.
- Bound field syntax, body buffering, key resolution, replay TTL, and cache
  freshness.
- Treat trailer loss, resolver uncertainty, replay-backend uncertainty, and
  body-limit failures as verification failure.

Legacy Cavage signatures, AWS Signature V4, OAuth 1.0 signatures, and vendor
formats are not accepted by the RFC 9421 parsers. The `compatibility` package
only isolates caller-supplied implementations; its diagnostic callback receives
the original external error and therefore belongs behind application redaction.
