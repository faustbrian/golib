# Trust and propagation

Tenant metadata is accepted only after the application authenticates the
immediate peer and explicitly returns a trust decision. Header presence,
network location, a proxy-looking address, or a previously authenticated end
user is not sufficient. Authentication establishes who sent a request;
authorization separately decides whether that principal may act for a tenant.

At an internet-facing HTTP boundary, strip inbound tenant headers and derive a
tenant from authenticated application state. At an internal service boundary,
authenticate the calling workload, reject direct backend access, allowlist the
specific propagation route, and then call `Accept`. Never copy a forwarded
header through an untrusted hop. JSON-RPC metadata follows the same rule.

Injection refuses an existing field, preventing a caller-controlled value from
being silently preserved or overwritten. Extraction distinguishes missing,
duplicate, conflicting, oversized, malformed, and untrusted data. Transport
adapters must retain these distinctions rather than convert them to a default
tenant.

Queue and event consumers authenticate their broker/topic/subscription policy
before marking metadata trusted. Retries, dead-letter handling, and replay must
carry the original tenant field and re-run extraction; they must not infer a
tenant from payload fields or queue names.
