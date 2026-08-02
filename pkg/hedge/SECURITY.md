# Security policy

Report vulnerabilities privately to the repository owner. Do not include
credentials, production payloads, request bodies, URLs, or raw customer errors.

Unsafe replay can duplicate side effects. Use hedging only after reviewing
every downstream hop's idempotency and authorization contract. Endpoint
diversity must preserve tenant routing, data residency, consistency, and
credentials. Bound amplification, labels, deadlines, cleanup, and shutdown.
