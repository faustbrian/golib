# Hardening Goal: XML Schema

## Objective

Prove XSD models, datatypes, compilation, reference resolution, validation,
identity constraints, serialization, and builders correct and secure.

## Required Audits

- Run every applicable official conformance case with zero unexplained skips.
- Exhaust namespaces, imports, includes, chameleon schemas, cycles, duplicates,
  derivation, substitution, wildcards, particles, facets, defaults, nil, and
  identity constraints.
- Verify every built-in datatype lexical/value space and boundary.
- Differential-test streaming and tree validation and independent reference
  implementations.
- Attack SSRF, file disclosure, entities, deep XML, schema recursion, particle
  explosion, XPath/identity amplification, regexes, and diagnostic growth.
- Prove all resolver, input, work, memory, depth, and output budgets.
- Race compiled schemas; fuzz parser/compiler/validator; mutation-test every
  normative constraint and security guard.

## Release Blockers

- Normative divergence, unexplained conformance skip, wrong datatype/facet,
  identity bypass, implicit I/O, resolver escape, unbounded work, race, panic,
  or invalid generated schema.

## Completion Criteria

- Official, differential, hostile, fuzz, race, mutation, leak, and benchmark
  suites pass with meaningful 100% coverage.

