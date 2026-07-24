# Adapters

HTTP uses `X-Correlation-ID`, `X-Request-ID`, and `X-Causation-ID`. The adapter
finds case-insensitive duplicates, sanitizes request headers to the accepted
values, mirrors them to the response, and stores immutable context values.

JSON-RPC operates on an explicit `Metadata` map of raw JSON values so a strict
envelope decoder can preserve duplicate members. It does not add a `meta`
member or otherwise rewrite protocol envelopes.

Queue and scheduler adapters use application-owned string maps. The webhook
adapter names the HTTP send/receive boundary. The request ID bridge accepts a
bound lookup function; pass a closure around
`requestid.FromContext(ctx, requestid.Request)` after the middleware source is
explicitly trusted.
