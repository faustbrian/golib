# PostgreSQL or search

Prefer PostgreSQL when the query is relational, results must reflect the same
transaction as a write, the corpus is modest, and built-in full-text ranking
and indexes meet the product contract. This avoids projection lag and a second
operational system.

Prefer a search engine when the contract needs language analyzers, typo or
prefix behavior, relevance tuning, highlighting, facets, suggestions, geo
queries, large independent read scale, or point-in-time cursor traversal.

Do not use this package as a relational abstraction or primary datastore.
Identifiers in search results must resolve safely against authorization and
current source-of-truth state.
