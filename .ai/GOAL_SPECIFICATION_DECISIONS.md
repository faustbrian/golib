# Goal: Make Specification Decisions Explicit And Auditable

## Mission

Establish one mandatory process for identifying, resolving, testing, and
maintaining ambiguities, errata, undefined behavior, implementation-defined
behavior, recommendations, extensions, and defensive policies across every
specification-backed package in `golib`.

Tests and conformance percentages are not substitutes for a decision record.
Two implementations can pass the same fixtures while making incompatible
choices in areas the fixtures do not cover. Every material choice MUST be
discoverable without reading implementation source or reconstructing intent
from tests.

## Initial Scope

This goal applies at minimum to:

- `json-schema`;
- `jsonapi`;
- `jsonrpc`;
- `openapi`;
- `openrpc`;
- standards-heavy portions of `authentication`, `http-client`,
  `http-middleware`, `router`, `wire`, and `webhook`.

Each package MUST identify any additional RFC, registry, profile, extension,
recommendation, or protocol whose ambiguity can affect observable behavior.

## Required Decision Register

Every specification-backed package MUST maintain
`docs/specification-decisions.md`. Each entry MUST have a stable identifier and
record:

- affected specification, exact version, section, and authoritative URL;
- the exact issue stated without silently choosing an interpretation;
- whether the issue is ambiguity, contradiction, omission, erratum,
  implementation-defined behavior, optional behavior, or interoperability
  policy;
- all credible interpretations and known peer behavior;
- the selected behavior and normative rationale;
- whether the choice is normative, recommended, defensive, extension-specific,
  transport-specific, or application policy;
- security, resource, compatibility, and wire-format consequences;
- executable tests, official fixtures, fuzz seeds, and interoperability cases;
- affected public APIs and documentation;
- upstream issue, erratum, or standards discussion where one exists;
- the condition that would require reconsidering the decision.

Unknown behavior MUST remain visibly unresolved. It MUST NOT be converted into
an undocumented default merely to complete implementation.

## Normative Source Discipline

- Pin every specification version and record provenance and integrity data.
- Treat normative specification text as authoritative over informative schema,
  examples, generated models, blog posts, and competitor behavior.
- Track errata, registries, recommendations, profiles, and official extensions
  separately from the base specification.
- Preserve BCP 14 requirement strength exactly.
- Do not infer a MUST from a common implementation or majority behavior.
- Do not call defensive behavior specification-required unless the source says
  so.
- Record version-specific differences instead of merging incompatible dialects
  into one approximation.

## Interoperability Discipline

- Select maintained peers with materially overlapping behavior.
- Build differential fixtures for every registered decision where practical.
- Compare semantic results, diagnostics, wire output, and failure behavior.
- Treat peer disagreement as evidence to investigate, not a vote.
- Minimize every disagreement and classify it as local defect, peer defect,
  fixture defect, harness defect, specification ambiguity, or deliberate
  policy difference.
- Keep competitor dependencies outside production modules.
- Never weaken normative behavior merely to match a popular implementation.

## Initial Decision Inventories

`jsonrpc` MUST cover at least notification-shaped invalid members, explicit
null IDs, numeric ID precision, empty batches, invalid batch members, response
ordering, parse-error versus invalid-request classification, duplicate object
members, notification-only HTTP responses, content types, and HTTP status
mapping.

`jsonapi` MUST cover at least unspecified members, extension and profile
namespaces, query parameter families, relationship linkage, included-resource
uniqueness, sparse fields, pagination links, Atomic Operations ordering and
rollback, recommendation status, and conflicts between base, extension,
profile, and recommendation text.

`json-schema` MUST cover at least dialect and vocabulary selection, unknown
keywords, format assertion, content keywords, annotation collection,
unevaluated behavior, dynamic references, reference siblings, duplicate JSON
members, numeric precision, regular-expression semantics, URI normalization,
remote loading, output formats, and optional test-suite behavior.

`openrpc` MUST cover at least reference siblings and cycles, method and
content descriptor uniqueness, example validation, schema dialect selection,
runtime discovery, extension members, canonicalization, composition conflicts,
and differences between specification prose, meta-schema, and examples.

`openapi` MUST cover at least undefined and implementation-defined behavior,
OpenAPI 2.0/3.0/3.1/3.2 dialect differences, Schema Object dialect rules,
Reference Object siblings, path templating, parameter serialization, security
requirement composition, server URL expansion, callbacks, webhooks, extension
members, external resources, reference cycles, YAML/JSON equivalence, and
conflicts between specification prose and published schemas.

## Automation

Provide a root check that:

- discovers specification-backed modules;
- requires their decision register and conformance matrix;
- validates unique stable decision identifiers and required fields;
- rejects broken authoritative links where deterministic checking is possible;
- verifies every resolved entry links to executable evidence;
- detects stale specification pins and newly published errata without silently
  changing behavior;
- reports unresolved decisions visibly without treating them as passing;
- prevents release when a normative contradiction or unreviewed observable
  behavior remains.

## Documentation And Change Control

- Link the decision register from README, conformance, compatibility, and
  contribution documentation.
- Record observable decision changes in the module changelog.
- Treat a changed wire-level or validation decision as a compatibility review,
  even when the old behavior was undocumented.
- Require a dedicated review section for specification decisions in pull
  requests that change parsing, validation, serialization, resolution, or
  protocol behavior.
- Preserve superseded decisions with links to replacements instead of erasing
  history.

## Completion Criteria

- Every in-scope package has a complete decision register and conformance map.
- Every known ambiguity or policy choice has an owner, status, rationale, and
  executable evidence.
- Official specifications, fixtures, errata, extensions, and recommendations
  are pinned and traceable.
- Differential results are reproducible and disagreements are classified.
- Root CI detects missing, stale, contradictory, or untested decisions.
- No package claims full compliance while relying on an undocumented material
  interpretation.
