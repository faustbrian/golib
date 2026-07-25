# Specification decisions

This register records choices that affect conformance claims. Decisions are
append-only once released; a superseding entry points to the earlier decision.

## XSD-DEC-001: Stable scope is XML Schema 1.0 Second Edition

- Status: accepted
- Date: 2026-07-19
- Sources: XSD Structures and Datatypes, 28 October 2004, plus the Second
  Edition errata snapshot in `manifest.tsv`

The stable release target is XML Schema 1.0 Second Edition. XML Schema 1.1
syntax is rejected or treated as foreign markup until assertions,
alternatives, open content, and its datatype changes have complete executable
evidence. No partial 1.1 conformance is advertised.

## XSD-DEC-002: Parsing never resolves external resources

- Status: accepted
- Date: 2026-07-19
- Sources: XSD Structures 4.3; project security requirements

The document parser consumes only caller-supplied bytes. Includes and imports
remain resource references in the model. Compilation may resolve them only
through an injected resolver with explicit limits; the default resolver denies
file and network access.

## XSD-DEC-003: Conformance claims require matrix evidence

- Status: accepted
- Date: 2026-07-19
- Sources: XSTS framework; project acceptance criteria

A feature is supported only when the versioned requirement row names both its
normative source and executable evidence. A green unit suite cannot replace
the official test-suite result for a broad conformance claim.

## XSD-DEC-004: XML Schema 1.0 support matrix is complete

- Status: accepted
- Date: 2026-07-19
- Sources: XSD Structures and Datatypes; XSTS 2007-06-20; requirement matrix

Every XML Schema 1.0 support and release-quality row now has executable
evidence, and every accepted XSTS expectation passes. A discovered semantic
gap or regression reopens the affected row; it must not be hidden by narrowing
the prose claim or weakening the gate.
