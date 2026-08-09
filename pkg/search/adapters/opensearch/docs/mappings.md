# Mappings and analyzer ownership

Applications own document mappings, settings, analyzers, language fields, and
ranking. Store their canonical fingerprint with the physical index target and
bind it into cursors. A changed fingerprint invalidates traversal rather than
mixing results across schemas.

Locale-to-analyzer configuration is an explicit allowlist. Unknown locales do
not become analyzer names. Model exact identifiers and filterable fields as
keywords, full-text fields with intentional analyzers, dates as timestamps,
coordinates as geo points, and nested arrays as nested mappings when element
relationships matter.

Test mappings and relevance against pinned real OpenSearch fixtures. The core
fake is not an analyzer or ranking oracle.
