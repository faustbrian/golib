# Compatibility and capabilities

The module pins official client `opensearch-go/v4` `v4.7.3` and supports exact
OpenSearch versions `2.19.6` and `3.8.0`. Each release records those versions
in integration evidence; users must test their
exact server, plugins, analyzers, mappings, and ranking corpus.

The [specification decision register](specification-decisions.md) records every
observable REST interpretation and defensive adapter policy in the supported
matrix.

The adapter supports the core typed query family, filters, stable sorting,
source projection, terms and range aggregations, highlights, suggestions,
geo distance, external-version writes, bulk item outcomes, PIT/search-after
pagination, trusted node discovery, and index lifecycle operations. Capability
growth is explicit. Write capabilities are advertised only when a durable
`WriteGuard` is configured. It does not claim that relevance or analyzers are
portable.

This is the first production-readiness contract for the adapter; there is no
declared compatible earlier adapter release. The release gate overlaps the
current adapter process with the frozen `testdata/mixedappv1` application
wire-protocol executable against one real alias. Both processes perform
attributed external-version writes and ordered reads, and the current adapter
verifies the combined logical-index view. The fixture is content-pinned in this
module and deliberately uses only the documented OpenSearch wire contract; it
is not presented as a historical adapter release. Once an adapter release is
published, the matrix must add that immutable released binary before claiming
compatibility with it. Cursor and migration-checkpoint handoff between
application versions remains a cold-handoff boundary unless a release matrix
explicitly proves those exact versions.

The adapter accepts an explicitly constructed `RawExtensionQuery` only when it
is bound to `opensearch` and contains one bounded JSON object. Applications
must authorize construction of that node. The adapter never falls back to a
query string or accepts an extension bound to another backend.
