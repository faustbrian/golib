# Compatibility and capabilities

The module pins official client `opensearch-go/v4` `v4.7.3` and supports exact
OpenSearch versions `2.19.6` and `3.8.0`. Each release records those versions
in integration evidence; users must test their
exact server, plugins, analyzers, mappings, and ranking corpus.

The adapter supports the core typed query family, filters, stable sorting,
source projection, terms and range aggregations, highlights, suggestions,
geo distance, external-version writes, bulk item outcomes, PIT/search-after
pagination, trusted node discovery, and index lifecycle operations. Capability
growth is explicit. It does not claim that relevance or analyzers are portable.

The adapter accepts an explicitly constructed `RawExtensionQuery` only when it
is bound to `opensearch` and contains one bounded JSON object. Applications
must authorize construction of that node. The adapter never falls back to a
query string or accepts an extension bound to another backend.
