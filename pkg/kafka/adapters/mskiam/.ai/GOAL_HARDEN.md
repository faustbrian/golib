# Goal Harden: Amazon MSK IAM Authentication Adapter

## Mission

Prove token generation remains secure and available through credential
rotation, clock skew, concurrency, AWS failure, and workload replacement.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Model credential retrieval, cache invalidation, signer call, validation,
  return, expiry, cancellation, and failure transitions.
- Inject expired/near-expiry credentials, clock skew, malformed tokens,
  oversized values, signer/provider panic, timeout, cancellation, throttling,
  access denial, endpoint failure, and credential rotation.
- Race token requests through shared caches and custom providers; prove refresh
  does not stampede and returned expiries never exceed credential lifetime.
- Test ECS task-role, EKS pod-identity/web-identity, environment, profile, and
  explicit provider paths without embedding static credentials in fixtures.
- Run real MSK Provisioned and Serverless interoperability before claiming
  support; retain explicit unverified labels otherwise.
- Search errors, traces, metrics, panic output, fixtures, and test failures for
  credential/token disclosure using generated canaries.
- Audit timers, contexts, goroutines, provider ownership, process-wide signer
  settings, and behavior after Kubernetes credential rotation.
- Benchmark generation and contention separately from external retrieval.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests with no unresolved secret, expiry, race,
lifecycle, or MSK interoperability finding.
