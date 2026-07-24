# Goal: Audit and harden `jsonrpc`

## Mission

Perform an evidence-driven security, correctness, interoperability, and
operational audit of `jsonrpc`, then implement every justified hardening
change required for a production-grade JSON-RPC 2.0 v1 library.

Do not assume existing tests, documentation, coverage, or a previous release
prove correctness. Trace actual behavior from untrusted bytes through parsing,
validation, dispatch, middleware, handlers, response construction, transports,
and client correlation. Preserve compatibility unless a verified defect or
security issue requires a documented change.

## Authoritative inputs

- The official JSON-RPC 2.0 specification.
- Go's `encoding/json`, `net/http`, `context`, and concurrency contracts.
- The repository's `.ai/GOAL.md`, `AGENTS.md`, compatibility policy, public API,
  examples, conformance fixtures, and changelog.
- Published security guidance applicable to HTTP servers, clients, parsers,
  resource exhaustion, and dependency use.

Use primary sources. Link every protocol conclusion to the relevant
specification section. Distinguish normative requirements from defensive
policy and optional convenience behavior.

## Phase 1: Establish the baseline

1. Inventory every exported API, wire behavior, option, error, hook,
   middleware seam, HTTP behavior, client behavior, limit, goroutine, and
   dependency.
2. Map every JSON-RPC 2.0 requirement and error code to implementation code,
   tests, fixtures, and documentation.
3. Run the complete repository gate, race detector, fuzz targets, benchmarks,
   vulnerability scan, documentation checks, and workflow validation.
4. Record failures, flakes, skips, environmental blockers, and untested claims.
5. Produce a threat model covering malicious clients, malicious servers,
   buggy handlers, oversized batches, cancellation, slow transports, and
   concurrent registry or dispatcher use.

Do not change production behavior until the baseline and a failing regression
test demonstrate the issue.

## Protocol and parser audit

Prove behavior for at least:

- JSON syntax errors versus structurally invalid JSON-RPC documents;
- scalar, array, object, empty-batch, and mixed-batch top-level values;
- missing, wrong, duplicated, unknown, or malformed `jsonrpc`, `method`,
  `params`, `id`, `result`, and `error` members;
- string, number, null, absent, fractional, exponent, very large, duplicate,
  and otherwise ambiguous IDs without precision loss;
- notification detection and the requirement to emit no response, including
  handler failures, panics, and notifications inside batches;
- batch response membership, ordering policy, concurrency, partial failures,
  duplicates, and empty response behavior;
- exact standard error codes, safe custom codes, error data, and prevention of
  internal error or panic disclosure;
- params decoding for arrays, objects, null, omitted values, unknown fields,
  trailing data, and typed targets;
- client validation of version, IDs, result/error exclusivity, missing or
  duplicate batch responses, unsolicited IDs, and malformed server replies;
- deterministic marshaling wherever the public contract promises it.

Add official examples as fixtures and adversarial counterexamples for every
rule. Where useful, add differential tests against an independent conforming
implementation, but never treat another library as more authoritative than
the specification.

## Transport and runtime audit

Audit and harden:

- request and response body limits, including batches and decompression policy;
- media type parsing, parameters, method handling, status codes, and headers;
- URL validation, redirects, caller-supplied clients, connection reuse, and
  response-body cleanup;
- context propagation and cancellation before, during, and after handlers;
- middleware order, repeated middleware, nil dependencies, hooks, and panic
  containment;
- registry mutation and lookup under concurrency;
- batch parallelism, backpressure, goroutine bounds, ordering, and cleanup;
- slow readers, slow writers, partial writes, transport errors, and timeouts;
- sensitive values in errors, logs, hooks, or protocol error data.

No input-controlled path may cause an unbounded allocation, unbounded
goroutine fan-out, deadlock, data race, panic escape, or silent protocol
violation.

## API and compatibility audit

- Identify nil traps, unsafe zero values, confusing ownership, inconsistent
  options, and error contracts that cannot be inspected with `errors.Is` or
  `errors.As` where appropriate.
- Verify all exported symbols have accurate Go documentation and examples.
- Treat serialized behavior, ID handling, error mapping, hook order, and
  middleware order as compatibility-sensitive.
- Prefer additive fixes. If a breaking correction is unavoidable, document
  the exact prior behavior, risk, migration, and required semantic version.
- Keep the package transport-neutral and free from application-specific policy.
- Verify optional `authentication`, `authorization`, and `idempotency`
  integrations preserve JSON-RPC notification, batch, ID, and error semantics.

## Test and hardening requirements

- Write a failing regression test before every behavioral fix.
- Maintain meaningful 100% production statement coverage.
- Expand fuzzing for request decoding, response decoding, batches, dispatcher
  entry points, client correlation, and marshal/unmarshal round trips.
- Seed fuzzers with official examples, boundary IDs, deep JSON, duplicate
  members, invalid UTF-8, large batches, and previously failing inputs.
- Add race, cancellation, leak, panic, slow-I/O, and bounded-resource tests.
- Add benchmarks for hostile as well as representative single and batch calls;
  define budgets only from reproducible evidence.
- Run `go vet`, Staticcheck, the race detector, coverage, fuzz smoke targets,
  benchmarks, `govulncheck`, `actionlint`, and documentation validation.

## Required Deliverables

1. A hardening report listing each finding with severity, evidence,
   exploitability or impact, affected API, and disposition.
2. A JSON-RPC conformance matrix linking every normative rule to source, test,
   fixture, and documentation evidence.
3. Focused commits containing regression tests, fixes, and documentation.
4. Updated API, compatibility, security, troubleshooting, performance, and
   changelog documentation where findings affect them.
5. Updated fuzz corpora, benchmarks, and CI gates for every newly protected
   boundary.
6. A final release-readiness verdict with exact verification commands and
   results, remaining risks, and the semantic-version recommendation.

## Release Blockers

- Any protocol divergence, incorrect request/notification behavior, unsafe
  resource handling, compatibility regression, or security defect.
- Missing meaningful 100% coverage or a failing GitHub Actions gate.

## Completion Criteria

The work is complete only when:

- every normative JSON-RPC 2.0 rule has traceable passing evidence;
- every high- and medium-severity finding is fixed or explicitly rejected with
  evidence and documented rationale;
- all untrusted inputs and concurrent paths have enforced resource bounds;
- no known panic, race, deadlock, leak, response-correlation, or information
  disclosure gap remains;
- public behavior and intentional limits are accurately documented;
- the full local quality and hardening gate passes without skips; and
- the final report states what was actually verified without overstating
  compliance or production readiness.
