# Hardening Goal: Typed API Query Contracts

## Objective

Prove that `api-query` cannot be used to bypass authorization or tenant
constraints, inject persistence syntax, forge cursors, destabilize pagination,
or consume unbounded resources under hostile query input and schema evolution.

## Required Audits

### Schema And Compilation Audit

- Exhaust unknown, duplicate, conflicting, inaccessible, deprecated, and
  revision-mismatched capabilities.
- Mutation-test every allowlist, mandatory predicate, authorization, and cost
  branch that could broaden a plan.
- Prove canonical plans are deterministic across maps, transports, and runs.
- Verify schemas and compiled plans remain immutable and race-safe.

### Filter And Persistence Audit

- Fuzz filter grammar, types, Unicode, nesting, large sets, null, empty, numeric
  edges, and malformed encodings.
- Prove no identifier or value reaches SQL outside allowlisted parameterized
  compilation.
- Verify mandatory tenant and authorization predicates survive every
  composition, simplification, adapter, and empty-filter path.
- Test expensive-query budgets against adversarial expression shapes.

### Cursor And Pagination Audit

- Fuzz malformed, oversized, expired, unknown-version, wrong-schema, tampered,
  replayed, and key-rotation cursors.
- Property-test forward/backward traversal with duplicates, inserts, deletes,
  nulls, ties, and boundary values.
- Prove stable total ordering and no duplicate/omitted rows under the documented
  consistency model.
- Ensure cursor diagnostics never expose signing keys or sensitive positions.

### Transport And Resource Audit

- Differential-test HTTP, JSON-RPC, OpenRPC, and JSON:API bridge plans.
- Bound bytes, depth, nodes, values, fields, includes, sorts, cursors, errors,
  allocations, and compilation time.
- Threat-model schema probing, field exfiltration, relationship traversal,
  injection, tenant escape, and denial of service.

## Required Deliverables

- Capability and plan semantic matrices.
- Threat model, query-cost budgets, cursor protocol, and hardening findings.
- Mutation, fuzz, race, cross-transport, and PostgreSQL injection evidence.
- Pagination stability corpus and benchmark baselines.
- Updated API, security, migration, cursor, SQLC, FAQ, and troubleshooting docs.

## Release Blockers

- Any undeclared capability, tenant/auth bypass, SQL injection, cursor forgery,
  unstable total order, sensitive data leak, race, panic, or unbounded query.
- Any silent semantic difference between transport adapters.
- Any pagination claim not supported by consistency documentation and tests.
- Missing meaningful 100% coverage, mutation evidence, or green blocking CI.

## Completion Criteria

- Schema, filter, cursor, transport, persistence, and evolution suites pass.
- Security predicates and resource budgets are mutation-resistant.
- Race, fuzz, vulnerability, compatibility, and performance gates pass.
- NilAway runs visibly as advisory without blocking findings.
- No release blocker remains and the changelog is current.
