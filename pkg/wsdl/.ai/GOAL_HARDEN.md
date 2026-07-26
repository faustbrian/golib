# Hardening Goal: WSDL Toolkit

## Objective

Prove WSDL models, namespaces, imports, bindings, operation patterns,
generation, composition, and XSD integration correct and secure.

## Required Audits

- Inventory every WSDL 1.1 and 2.0 normative rule, schema discrepancy,
  extension point, and erratum.
- Exhaust QName scope, namespace aliases, imports/includes, cycles, duplicate
  symbols, operation overloading, messages, faults, binding styles/uses,
  actions, headers, MIME parts, endpoints, and required extensions.
- Parse and validate representative real carrier WSDLs and differential-test
  reference tooling.
- Verify builder and parser semantic round trips and deterministic output.
- Attack entities, SSRF, files, redirects, import/schema bombs, recursive
  graphs, extensions, and diagnostics.
- Race immutable compiled descriptions; fuzz XML/models/builders; mutation-test
  every normative and security branch.

## Release Blockers

- Spec divergence, wrong binding/operation graph, namespace confusion, implicit
  I/O, resolver escape, invalid generated WSDL, XSD duplication, unbounded work,
  race, panic, or unexplained fixture failure.

## Completion Criteria

- Normative, real-world, differential, hostile, fuzz, race, mutation, and
  benchmark suites pass with meaningful 100% coverage.

