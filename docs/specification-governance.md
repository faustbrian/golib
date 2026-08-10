# Specification Governance

Specification conformance requires explicit decisions wherever normative text,
errata, registries, schemas, examples, extensions, recommendations, or peer
implementations permit materially different observable behavior. Passing an
official fixture suite does not replace this record.

## Applicability

Every public library or adapter with a non-empty `specifications` or
`provenance` field in `modules.json` must provide all of the following. A
provenance manifest without specification metadata is itself a catalog error.

- `docs/specification-decisions.md`;
- at least one cataloged conformance corpus;
- at least one machine-readable provenance manifest;
- a mandatory conformance gate;
- executable evidence for every resolved decision.

Harness and fixture modules inherit the decision contract of the production
module they verify. They do not maintain independent registers unless they
expose their own public protocol behavior.

Specification-backed modules must declare themselves in the catalog. Omitting
specification metadata to avoid this policy is a repository defect.

## Decision Format

Each decision uses a stable, sequential identifier:

```text
## PACKAGE-DEC-001: Short descriptive title
```

The body must explicitly record:

- status and owner;
- exact source version, section, and authoritative URL;
- classification;
- issue;
- credible interpretations;
- known peer behavior;
- selected behavior;
- security and resource consequences;
- compatibility and wire consequences;
- executable evidence;
- affected public surface;
- upstream issue, erratum, or discussion;
- reconsideration condition.

These requirements must appear in the entry's table or bold field labels.
Mentioning a field name incidentally in narrative text does not satisfy the
decision format. Consequence fields may be combined, but their body must still
address security, resources, compatibility, and wire behavior explicitly.

The accepted statuses are `resolved`, `unresolved`, and `superseded`.
Each entry must declare exactly one of them in its status field; status words
elsewhere in the entry do not satisfy that contract. Unresolved decisions block
the specification gate. A superseded decision must remain in the register and
identify a different existing decision in the same register as its replacement;
history must not be erased.

Every register must end with one `## Unresolved decisions` or
`## Unresolved and excluded behavior` inventory. The inventory must explicitly
state that no known decision remains unresolved. A known open question must
instead receive a stable decision identifier, owner, and `unresolved` status so
the gate blocks it visibly; the terminal inventory must not be used to hide it.

Executable evidence names exact Go `Test`, `Fuzz`, or `Benchmark` functions.
A trailing `*` may identify a tested function family when at least one matching
function exists. Prose such as "covered by parser tests" is not attributable
evidence.

## Provenance

TSV provenance manifests contain `id`, `version`, `url`, `sha256`, and
`status` columns. Every source row must have a unique identifier, an exact
version or immutable revision, an HTTP(S) source URL, a complete SHA-256
digest, and status `pinned` or `snapshot`.

JSON manifests may model inherited repositories, revisions, licenses, and
vendored files hierarchically. They must remain valid JSON and include
authoritative source URLs and source-integrity digests. Module conformance
scripts verify every source and vendored artifact represented by the manifest;
the root structural check does not silently fetch or update normative inputs.

An upstream revision, changed digest, new erratum, or registry update is a
review trigger, not authority to change runtime behavior automatically. Update
the manifest, classify the difference, update or add decisions, add executable
evidence, and record compatibility impact before changing behavior.

## Validation

Run the repository structural check with:

```bash
make specification-decisions
```

The check discovers applicable modules from current source and catalog policy,
then fails for missing registers, corpora, provenance, conformance gates,
stable identifiers, required fields, local links, statuses, replacement links,
executable evidence, or an explicit closed unresolved-decision inventory.
Local Markdown links with fragments must resolve to an existing generated
heading anchor; checking only that the target file exists is insufficient.
Package conformance gates verify pinned remote bytes, official fixtures, errata
review ceilings, and interoperability behavior.

The repository and package checks are complementary. Neither may be replaced
by a passing peer comparison, generated schema validation, documentation
review, or aggregate conformance percentage.

## Change Review

Any pull request changing parsing, validation, serialization, resolution,
canonicalization, transport, or protocol behavior must complete the
Specification Decisions section in the pull request description. It must list
affected decision identifiers, source or errata changes, observable behavior,
compatibility consequences, and executable evidence. State `Not applicable`
only when the change cannot affect a specification-backed behavior.

New ambiguity remains unresolved until the register identifies an owner and a
review condition. It must not be converted into an undocumented default to
make implementation or CI pass.
