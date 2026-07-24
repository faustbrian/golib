# HTTP authentication

`authhttp.NewExtractor` evaluates all explicitly configured sources in order
and requires exactly one credential. Duplicate Authorization fields, repeated
query parameters or cookies, partial API keys, and credentials present in more
than one source are rejected.

Header sources are `BasicAuthorization`, `BearerAuthorization`, and
`APIKeyHeader`. Query and cookie sources exist only through explicit bearer or
API-key constructors. The extractor never reads the request body.

`BearerQuery` and `APIKeyQuery` are deprecated for new designs. Query secrets
can be copied into browser history, referrers, reverse-proxy logs, and access
logs before this package receives the request. If a legacy protocol forces
their use, require TLS, suppress query logging end to end, use short-lived
credentials, and apply `Cache-Control: no-store` to requests and `private` to
successful responses as required by RFC 6750. Prefer headers.

`NewMiddleware` authenticates, stores the principal under a private context
key, and passes the original `http.ResponseWriter`, preserving optional
interfaces. It returns 401 for credential failures and 503 for unavailable or
unclassified failures. Configure fallback challenges with `WithChallenges`.
Challenges are sorted and quoted safely.

The middleware does not authorize. See `authhttp.ExampleNewMiddleware`.
