# Hardening Goal: Complete OpenRPC Specification Foundation

## Objective

Prove that `openrpc` is completely specification-conformant, lossless,
deterministic, bounded, secure, interoperable, concurrency-safe, and panic-free
across every OpenRPC object, normative requirement, JSON Schema, reference,
runtime expression, discovery flow, parser input, resolver, composition, diff,
and optional generation path.

## Required Audits

### Specification Conformance Audit

- Reconcile every normative statement and object field against implementation,
  tests, documentation, and the pinned official specification/meta-schema.
- Exhaust required, optional, defaulted, nullable, deprecated, reference,
  extension, patterned-field, and uniqueness behavior.
- Verify supported version ranges and unknown future-version rejection.
- Classify and close every discrepancy; no undocumented conformance gap may
  remain.

### Parser And Model Audit

- Fuzz duplicate keys, invalid UTF-8, numbers, nesting, huge strings, unknown
  fields, extensions, trailing input, malformed objects, and resource edges.
- Prove strict and preserving modes differ only where documented.
- Property-test deterministic canonical output and lossless preserving round
  trips for all accepted documents.
- Prove caller data, builders, documents, schemas, and extensions have safe
  immutable ownership.

### Schema, Reference, And Expression Audit

- Exhaust Draft 7 boolean/object schemas, composition, recursion, formats,
  annotations, and every OpenRPC schema placement.
- Test internal/external references, JSON Pointer escaping, URI bases, relative
  paths, fragments, cycles, aliases, bundling, and dereferencing.
- Exhaust every runtime-expression and link evaluation form with absent,
  malformed, and adversarial contexts.
- Mutation-test every validation, reference, pointer, expression, and schema
  decision.

### Discovery, Composition, And Diff Audit

- Prove discovery method behavior for static, generated, filtered, empty, and
  revisioned documents through `jsonrpc` integration.
- Property-test registries, overlays, merges, component renames, reference
  rewrites, filtering, and deterministic composition.
- Exhaust semantic compatibility classification across methods, parameter
  structures, results, errors, schemas, links, servers, and components.
- Verify generators and CLI operations fail on semantic narrowing rather than
  silently emitting incomplete output.

### Resolver And Security Audit

- Attack SSRF, local file disclosure, unsafe schemes, redirects, DNS changes,
  decompression bombs, credential-bearing URLs, cycles, depth, fan-out, and
  cancellation races.
- Enforce bytes, tokens, nesting, methods, schemas, references, documents,
  diagnostics, output, expression, generation, and diff budgets.
- Verify core parse/validate paths perform no hidden I/O.
- Prove errors, diagnostics, logs, and hooks never disclose documents, fetched
  bodies, credentials, or high-cardinality sensitive values.

### Concurrency And Interoperability Audit

- Race/stress concurrent parsing, validation, serialization, resolution,
  deduplication, discovery, filtering, diffing, cancellation, and shutdown.
- Leak-test resolver, registry, hook, generator, and cancellation failure paths.
- Differential-test official examples and diverse ecosystem documents against
  the official meta-schema and independent implementations.
- Prove no hidden goroutine, mutable global registry, unbounded cache, deadlock,
  unsafe, cgo, or `go:linkname` remains.

## Required Deliverables

- Complete normative requirement and object-field conformance matrices.
- Meta-schema, ecosystem interoperability, and supported-version reports.
- Resolver threat model and enforced resource-budget table.
- Fuzz, mutation, race, leak, security, compatibility-diff, and benchmark
  evidence.
- Updated API, adoption, migration, security, performance, specification, FAQ,
  and troubleshooting documentation.

## Release Blockers

- Any missing or divergent normative requirement, object, field, extension,
  reference, schema, expression, link, or discovery behavior.
- Any semantic loss, unstable output, incorrect diagnostic, hidden I/O, SSRF,
  file disclosure, credential exposure, race, deadlock, leak, panic, or
  unbounded work.
- Any generator or diff path that silently narrows or misclassifies an API.
- Missing meaningful 100% coverage, mutation evidence, or green blocking CI.

## Completion Criteria

- Specification, parser, model, schema, reference, expression, discovery,
  composition, diff, and interoperability suites pass.
- Every normative statement is linked to executable evidence and documentation.
- Resolver and parser paths survive enforced limits and adversarial security
  tests.
- Race, fuzz, mutation, leak, vulnerability, compatibility, and performance
  gates pass.
- NilAway runs visibly as advisory without blocking findings.
- No release blocker remains and the changelog is current.
