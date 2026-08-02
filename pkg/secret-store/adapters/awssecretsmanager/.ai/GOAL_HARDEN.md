# Goal Harden: Immutable AWS Secrets Manager Version Store

## Mission

Prove immutable version creation and reconciliation under concurrency, AWS
ambiguity, throttling, credential rotation, and hostile inputs.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Model missing name, existing name, exact retry, token collision, stage
  collision, create/put/read ambiguity, cancellation, and terminal failure.
- Inject AWS throttling, access denial, network loss before and after acceptance,
  timeout, malformed responses, KMS failure, eventual visibility, credential
  expiry/rotation, region mismatch, and SDK retry exhaustion.
- Race concurrent identical and conflicting writers across goroutines and
  simulated Kubernetes replicas; prove deterministic reconciliation.
- Fuzz names, stages, tokens, binary values, ARN/version responses, and provider
  diagnostics under strict size and allocation bounds.
- Assert no secret bytes, tokens, credential material, endpoint details, or
  arbitrary AWS diagnostics appear in errors, logs, traces, metrics, or tests.
- Audit copies and zeroization attempts, caller/provider ownership, response
  retention, context cancellation, and absence of leaked goroutines.
- Run real AWS or faithful contract tests for exact retry and conflict semantics;
  record unverified emulator differences explicitly.
- Benchmark local overhead separately from AWS latency and document quotas so
  large migrations use bounded concurrency and backoff.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests and no unresolved security, ambiguity,
race, fuzz, or interoperability finding.
