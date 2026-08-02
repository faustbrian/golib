# Goal: Amazon MSK IAM Authentication Adapter

## Objective

Build `mskiam` as the optional AWS MSK IAM SASL/OAUTHBEARER token provider for
`kafka`. It MUST use AWS-supported signing and credential discovery, remain
AWS-specific, and never implement SigV4, Kafka SASL, or IAM authorization.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Validate canonical AWS region, bounded token timeout, provider ownership, and
  TLS-only use through the root Kafka security contract.
- Default to the AWS SDK v2 credential chain and retain its refreshing provider;
  support explicit caller-owned concurrency-safe providers.
- Generate a fresh signed token per authentication session using the supported
  AWS signer and bound token size, format, lifetime, and effective expiry.
- Refresh credentials too close to expiry once when the provider supports safe
  invalidation; fail closed otherwise.
- Honor context cancellation and outer Kafka credential deadlines.
- Return stable redacted categories for configuration, credential, signer,
  panic, timeout, cancellation, expiry, and malformed output.
- Never expose access keys, secret keys, session tokens, signed tokens,
  credential endpoints, provider diagnostics, or process-wide debug modes.

## Documentation And Completion

Document ECS task roles, EKS pod identity/web identity, IAM permissions, token
and credential lifetime, supported MSK variants, API, examples, adoption, FAQ,
compatibility, and security. CI MUST enforce race, fuzz, signer interop, API,
docs, benchmarks, exactly 100% statement coverage, and exactly 100% of viable
mutants killed by meaningful tests.
