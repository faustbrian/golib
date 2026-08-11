# FAQ

## Is relevance portable?

No. Core queries are portable intent; analyzer and ranking behavior belongs to
the adapter, index definition, and application acceptance corpus.

## Is an accepted write immediately searchable?

Not necessarily. Visibility follows refresh policy. Document the application
read-your-write strategy.

## Can I retry a failed bulk request?

Retry individually classified, idempotent items within a bounded budget.
Reconcile unknown outcomes first.

## Why did my cursor stop working?

It may be expired, tampered with, bound to another query or tenant, exceed its
budget, reference an expired PIT, or target a changed index-definition
fingerprint.

## Does the fake match OpenSearch ranking or analyzers?

No. Its declared shared fidelity is intentionally limited to deterministic
match-all, boolean, exact-term, prefix, and exists queries; one
`search.DocumentIDSortField` sort; bounded offset pagination;
tenant/logical-index isolation; external-version
index/delete semantics; tombstones; and ordered bulk attribution. The fake also
implements update-existing and upsert for application tests, but those are
fake-only capabilities: the OpenSearch adapter rejects update-existing and the
shared real-adapter conformance suite does not claim either action as portable.
Exact-term matches an identical JSON scalar, and exact-term or prefix matches
any matching scalar inside a multi-valued array. This is the mapping-independent
subset exercised by the real-adapter conformance fixture.

It does not emulate mapping coercion, analyzer or normalizer behavior,
object-array flattening, nested mappings, full-text analysis, scoring, range or
geo behavior, projection, highlights, aggregations, suggestions, raw
extensions, PIT/cursor consistency, refresh visibility delays, shard failures,
transport ambiguity, backend response parsing, lifecycle/templates,
migration/cutover, or backend capacity. It applies accepted writes synchronously
and can only synthesize its document/tombstone capacity rejection. Capability
flags reject every omitted feature instead of approximating it. Run adapter
conformance and pinned real OpenSearch fixtures for all backend behavior and
failure semantics.
