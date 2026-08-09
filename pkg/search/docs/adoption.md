# Track and Location adoption

## Track

Project one document per tracking entity with the tracking ID as stable ID and
the source sequence or revision as external version. Persist an outbox event in
the same transaction as the tracking change. Typical requests combine full text
over reference/carrier fields, exact tenant and status filters, timestamp
ranges, highlights, status facets, and a stable `updated_at`, `_id` sort.

## Location

Project one document per location with explicit localized name fields, country
and type keywords, nested address values, and a geo point. Map the request
locale through an application-owned allowlist of analyzers. Typical requests
combine localized full text, country/type filters, geo distance, name
highlights, type facets, and suggestions.

Both applications must re-authorize returned IDs against current source data,
define projection-lag behavior, expose bounded cursor traversal, and own ranking
tests against the deployed OpenSearch version and mappings.
