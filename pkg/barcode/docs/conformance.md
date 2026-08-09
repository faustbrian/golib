# Conformance

The module claims conformance only to the exact software profiles and editions
reported by `barcode.CapabilityFor`. It does not claim physical print quality,
scanner certification, regulatory certification, or compatibility with
replacement standards editions that have not been reviewed.

The [decision register](specification-decisions.md) records material
interpretations and limitations. The [source manifest](../specification/manifest.json)
pins standards identities, replacement editions, fixture digests, and
independent implementation revisions. The
[normative](../specification/normative.tsv) and
[evidence](../specification/evidence.tsv) matrices map every supported format
requirement to executable evidence.

`make conformance` validates those inventories, the stable decision set,
deterministic render fixtures, control and boundary behavior, and reciprocal
independent reader and writer interoperability. Restricted ISO and AIM texts
are not redistributed, so the gate cannot honestly claim to rerun proprietary
official test suites that are not available under a compatible license.

Self-round trips, visually plausible images, aggregate coverage, and agreement
with one peer are insufficient. A format is advertised only when its complete
documented software profile has reciprocal evidence and no unreported
limitation.
