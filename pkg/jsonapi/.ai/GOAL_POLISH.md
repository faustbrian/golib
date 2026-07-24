# Goal: Make JSON:API Specification Decisions Explicit

## Objective

Add the missing decision register and interoperability evidence without
rewriting the original JSON:API goal history.

## Required Work

- Implement `docs/specification-decisions.md` under the root specification
  decision contract.
- Cover base-spec ambiguities, official extensions, official profiles,
  recommendations, Atomic Operations, query parameter families, relationship
  linkage, included-resource uniqueness, sparse fields, sorting, filtering,
  pagination, errors, and extension/profile conflicts.
- Distinguish normative base requirements, extension requirements, profile
  semantics, recommendations, defensive policies, and convenience APIs.
- Record alternatives, chosen behavior, rationale, wire impact, security and
  resource impact, compatibility impact, tests, fixtures, and revisit triggers.
- Differential-test maintained peers only where capabilities overlap and never
  treat majority behavior as normative authority.
- Link decisions from conformance, compatibility, adoption, extension,
  recommendation, FAQ, and changelog documentation.

## Completion Criteria

- Every known material interpretation is documented and tested.
- No full-compliance claim relies on an unresolved or hidden choice.
- Interoperability differences are classified rather than silently normalized.

