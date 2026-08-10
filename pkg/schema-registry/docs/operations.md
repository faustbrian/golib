# Outages and incident operation

Choose outage behavior per lookup; there is no automatic fail-open. For normal
production decode, use fail-closed or a documented stale window. For startup or
incident isolation, load a release-bound bundle and use cache-only resolution.
Do not retry registration from every producer instance: use the client's
subject/fingerprint single-flight locally and deployment coordination across
instances.

Provider adapters apply one total deadline, bounded concurrency, bounded
responses, and bounded retries. Confluent retries transport failures,
throttling, and server errors within the same deadline. Glue delegates retry
policy to the configured AWS SDK client and does not multiply it.

During an incident record provider scope, operation, safe error class, cache
state, freshness age, and bundle provenance. Never record schema bodies or
credentials. Unknown registration outcome requires resolution by exact content
or provider identity before retrying.
