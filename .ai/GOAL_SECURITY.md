# Goal: Secure the Go Libraries Ecosystem

## Mission

Establish and verify a coherent security model across every module in
`golib`. Security work MUST cover product behavior, untrusted inputs,
concurrency, resource exhaustion, secrets, dependencies, build automation,
and disclosure handling rather than relying only on vulnerability scanners.

## Threat Modeling

Create a root threat model and package-specific models for security-sensitive
modules. Identify:

- trust boundaries and attacker-controlled inputs;
- assets, credentials, tokens, personal data, and operational metadata;
- network, filesystem, database, queue, cache, parser, resolver, and plugin
  boundaries;
- authentication and authorization decisions;
- replay, duplication, ordering, and idempotency risks;
- SSRF, path traversal, injection, request smuggling, decompression bombs,
  reference bombs, regex denial of service, and parser differentials;
- races, deadlocks, goroutine leaks, stale caches, and lifecycle failures;
- data exposure through logs, traces, metrics, errors, examples, fixtures,
  panic recovery, and CI artifacts;
- dependency, action, release, and maintainer-compromise scenarios.

Every accepted risk MUST have an owner, rationale, mitigation, and review
condition.

## Secure Design Requirements

- Safe defaults MUST require no implicit network, filesystem, process, or
  environment access.
- All input-controlled work MUST be bounded by explicit size, depth, count,
  concurrency, timeout, retry, and memory policies.
- Context cancellation MUST reach every blocking operation.
- Credentials and payloads MUST be redacted from errors and observability by
  default.
- Cryptographic operations MUST use maintained standard or x/crypto
  primitives and forbid custom cryptography.
- Authentication MUST prevent timing leaks where credential comparison is
  security-sensitive.
- Authorization MUST fail closed and keep policy decisions explicit.
- URL, proxy, redirect, DNS, and remote-resource behavior MUST be caller
  controlled and SSRF-aware.
- File paths and archive entries MUST resist traversal and symlink escapes.
- SQL and migration APIs MUST use parameters and explicit transaction
  ownership.
- Queue, scheduler, webhook, outbox, and idempotency APIs MUST define replay,
  duplicate, poison-message, dead-letter, and retry safety.
- Global mutable registries and hidden background goroutines are forbidden.
- Panic containment MUST not hide corruption or expose sensitive values.

## Security Testing

Require, where applicable:

- hostile-input and malformed-protocol tests;
- fuzzing seeded with real specifications, fixtures, and regressions;
- race, cancellation, leak, timeout, and fault-injection tests;
- resource exhaustion and algorithmic complexity tests;
- SSRF, redirect, proxy, path traversal, injection, and redaction tests;
- authentication bypass and authorization fail-closed tests;
- replay, duplication, ordering, retry, and partial-failure tests;
- differential parser and protocol tests;
- dependency vulnerability and license checks;
- secret scans across source, history additions, fixtures, docs, and artifacts.

Security regressions MUST receive focused tests before fixes.

## Tooling And CI

Configure strict local and CI execution for `govulncheck`, `gosec`, Go vet,
Staticcheck, the owned `analysis` security policies, dependency review,
secret scanning, workflow analysis, and license checks. NilAway remains
visible warning-only until its signal quality supports enforcement.

Tool suppressions MUST be narrow, documented, reviewed, and prevented from
silently increasing. Scanner success MUST NOT replace manual design review.

## Vulnerability Management

- Publish one clear private reporting process.
- Define severity, acknowledgement, remediation, embargo, advisory, and
  coordinated-release procedures.
- Identify affected modules and versions precisely.
- Support security fixes without unrelated package releases.
- Produce advisories, changelog entries, upgrade instructions, and regression
  tests for confirmed vulnerabilities.
- Never include exploit-enabling secrets or private reporter data in public
  artifacts.

## Required Evidence

Produce threat models, security matrices, exact scanner results, hostile-input
test evidence, unresolved-risk records, and a per-module release verdict.

## Completion Criteria

Security work is complete only when no known critical or high finding remains,
all medium findings are fixed or explicitly accepted with evidence, all
security gates pass, every untrusted boundary is bounded and tested, and
public security claims match the evidence.
