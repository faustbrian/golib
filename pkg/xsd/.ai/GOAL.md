# Goal: XML Schema Definition Implementation

## Objective

Build `xsd` as a complete, secure, specification-faithful XML Schema package
for parsing, compiling, resolving, validating, and generating XSD documents.
It is the schema foundation for `wsdl` and SOAP integrations, not an
extension of generic XML decoding.

## Specification Scope

- Full XML Schema 1.0 support for stable release.
- XML Schema 1.1 support only when its complete additional assertions,
  alternatives, open content, and datatype changes are implemented and tested.
- Pin W3C specifications, schemas for schemas, datatype specs, errata, examples,
  and conformance suites with provenance.
- Maintain versioned requirement matrices and a specification decision register.

## Core Capabilities

- Complete schema document model including target namespaces, imports,
  includes, redefine/override where applicable, annotations, defaults, and form
  policies.
- Built-in and user-defined simple/complex types, derivation, restriction,
  extension, unions, lists, substitution groups, abstract/final/block behavior.
- Elements, attributes, groups, attribute groups, wildcards, particles,
  sequences, choices, all, occurrence constraints, nillability, and defaults.
- Datatype lexical/value spaces and every facet, including precision-sensitive
  decimal/integer behavior through `math` where appropriate.
- Identity constraints: unique, key, and keyref with correct XPath subset.
- Compile schemas into immutable, concurrency-safe validation plans.
- Validate streaming or tree XML with stable structured diagnostics and source
  locations.
- Deterministic serialization and programmatic builders without generating
  invalid schemas silently.

## Resolution And Security

- Injected resolver for internal, relative, file, catalog, and remote resources.
- Remote and file loading disabled by default.
- Correct base URI, namespace, chameleon include, cycle, duplicate component,
  and resource identity behavior.
- Bound bytes, depth, schemas, components, references, particles, XPath work,
  identity tables, diagnostics, and retained source.
- Forbid external entity expansion and implicit DTD/network behavior.
- Defend against SSRF, file disclosure, redirect abuse, schema bombs, regex
  complexity, recursive types, and identity-constraint amplification.

## Package Shape

- Root: document model, errors, limits, version, diagnostics.
- `compile`: immutable schema sets and component resolution.
- `validate`: instance validation.
- `datatype`: XML Schema datatypes and facets.
- `resolve`: explicit resource/catalog interfaces.
- `builder`: valid-by-construction helpers where possible.
- `xsdtest`: official fixtures and reusable assertions.

## Verification And Documentation

Require meaningful 100% production coverage, official W3C conformance suites,
datatype vectors, XML parser differentials, import/include/cycle matrices,
stream/tree parity, fuzzing, race, mutation, cancellation, leak, complexity,
and correctness-gated benchmarks against maintained XSD implementations or
reference engines.

Document support, conformance, models, compilation, validation, datatypes,
resolution, catalogs, security, limits, builders, WSDL integration,
performance, migration, cookbook, FAQ, compatibility, and changelog. CI/local
gates follow ecosystem standards.

## Acceptance Criteria

- Every claimed XSD feature has normative and executable evidence.
- Schemas and XML instances are processed without implicit external I/O.
- Recursive schemas work while hostile graphs remain bounded.
- `wsdl` can consume compiled schema sets without duplicating XSD logic.
- Meaningful 100% coverage and every blocking gate pass.
