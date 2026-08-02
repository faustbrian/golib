# Goal: Immutable AWS Secrets Manager Version Store

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `awssecretsmanager` as the optional AWS adapter that creates or confirms
immutable, explicitly versioned binary secrets for migrations and durable
external references. It MUST NOT become a general read API, rotation engine,
staging-label manager, IAM manager, or application configuration loader.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Accept bounded binary values, stable names, caller-derived 32-64 byte
  idempotency/version tokens, unique non-AWS-managed stages, and an explicit
  KMS key reference.
- Create a missing secret or append an immutable version to an existing secret.
- Confirm exact retries by version-pinned read and constant-time byte comparison;
  reject token reuse with different material.
- Never move `AWSCURRENT`, `AWSPREVIOUS`, `AWSPENDING`, or another shared label.
- Return only stable ARN/version references and stable redacted errors.
- Copy and best-effort zero temporary payloads while defining caller ownership.
- Honor cancellation and AWS SDK retry policy without adding nested retries or
  claiming certainty after ambiguous remote outcomes.
- Use the caller's AWS SDK configuration and rotating credential provider.

## Documentation And Completion

Document AWS semantics, IAM/KMS permissions, idempotency derivation, conflict
and ambiguity recovery, API, examples, adoption, migration usage, FAQ,
security, compatibility, and cost/rate limits. CI MUST enforce strict checks,
race, fuzz, AWS contract tests, security scans, API compatibility, benchmarks,
docs, exactly 100% statement coverage, and exactly 100% of viable mutants
killed by meaningful tests.
