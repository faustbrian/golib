# Goal: Build And Polish The OpenAPI Specification Package

## Objective

Apply additive follow-up work to the serious full-spec open-source package
defined by `GOAL.md` and `GOAL_HARDEN.md`. Execute this prompt after those
foundational contracts without rewriting their historical requirements.

## Specification Scope

- Support OpenAPI 3.2.x, 3.1.x, and 3.0.x completely.
- Support OpenAPI/Swagger 2.0 as an explicitly separated legacy dialect.
- Pin exact specification revisions, official schemas, dialect meta-schemas,
  examples, registries, and errata with provenance and integrity metadata.
- Keep Arazzo and OpenAPI Overlay as separately scoped specifications.
- Treat normative specification prose as authoritative over informative
  schemas, examples, generated models, and peers.

## Required Capabilities

- Complete version-aware typed document models without a lossy superset.
- Bounded JSON and YAML parsing with duplicate-key, alias, number, UTF-8,
  trailing-input, and semantic-equivalence policies.
- Deterministic serialization and semantic round trips.
- Complete normative validation for every object, field, relationship,
  conditional rule, path template, parameter, operation, response, callback,
  webhook, link, security scheme, security requirement, server, component,
  encoding, example, extension, and Schema Object rule.
- Correct OpenAPI Schema Object dialect integration with `json-schema`.
- Internal, relative, file, and remote reference resolution through injected,
  disabled-by-default, bounded resolvers.
- Legal recursion, cycle detection, Reference Object sibling behavior, URI and
  JSON Pointer semantics, and explicit resource identity.
- Deterministic traversal, composition with conflict policies, provenance, and
  semantic diff primitives.
- Structured stable diagnostics distinguishing malformed input, invalid
  documents, unresolved references, unsupported dialects, limits, and internal
  failures.

## Boundaries

- Do not become an HTTP router, server framework, client runtime, controller
  binder, dependency-injection container, annotation scanner, or business
  policy engine.
- Keep code generation optional and outside core parsing and validation.
- Do not perform hidden remote loading, global registration, reflection-driven
  application discovery, or background work.
- Keep optional YAML, network, generator, router, telemetry, and competitor
  dependencies isolated where practical.

## Specification Decisions

- Implement `docs/specification-decisions.md` under the root decision contract.
- Cover undefined and implementation-defined behavior, version differences,
  Schema Object dialects, Reference Object siblings, path templating, parameter
  serialization, security requirement composition, server expansion,
  callbacks, webhooks, extensions, external resources, reference cycles,
  JSON/YAML equivalence, and prose/schema disagreements.
- Maintain versioned normative and conformance matrices linking every
  requirement to implementation, tests, and documentation.
- Keep unresolved choices visible and prevent unsupported compliance claims.

## Security And Resource Safety

- Defend against SSRF, file disclosure, credential forwarding, redirect abuse,
  DNS rebinding assumptions, YAML alias bombs, reference bombs, recursive
  schemas, regex denial of service, deep nesting, huge values, and diagnostic
  amplification.
- Bound bytes, depth, resources, aliases, redirects, references, diagnostics,
  schemas, operations, callbacks, and all other input-controlled growth.
- Honor cancellation throughout parsing, loading, validation, composition, and
  diff work.
- Do not use unsafe, cgo, `go:linkname`, hidden globals, implicit caches, or
  unowned goroutines.

## Comparative Benchmarks

- Compare getkin/kin-openapi, pb33f/libopenapi, applicable openapi packages,
  and direct JSON/YAML parsing baselines.
- Separate OpenAPI 2.0, 3.0, 3.1, and 3.2 tracks.
- Separate load, parse, model, validate, resolve, serialize, compose, diff,
  cold, warm, valid, invalid, local-reference, and remote-reference behavior.
- Match dialect, document, schema policy, references, diagnostics, limits, and
  outputs before ranking.
- Disqualify unsupported or incorrect candidates from ranked tracks without
  hiding diagnostic results.

## Quality And Documentation

- Require meaningful 100% production statement coverage plus mutation evidence
  for normative and security decisions.
- Add official fixture, differential, fuzz, race, leak, cancellation,
  complexity, JSON/YAML, resolver, and interoperability suites.
- Provide README, support matrix, quick start, complete API docs, conformance,
  decisions, parsing, validation, references, security, limits, extensions,
  Schema Object, composition, diff, migration, interoperability, benchmark,
  compatibility, FAQ, troubleshooting, cookbook, contribution, release, and
  changelog documentation.
- Provide strict local and CI gates matching the ecosystem standard.

## Completion Criteria

- Every claimed dialect and normative requirement has executable evidence.
- Official fixtures pass with zero unexplained skips.
- JSON and YAML are semantically equivalent under documented policy.
- References are explicit, bounded, cancellation-aware, and secure.
- Specification choices and competitor comparisons are auditable and fair.
- Meaningful 100% coverage and every blocking quality gate pass.
