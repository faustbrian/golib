# Policy Profiles

Policy profiles give workloads named, versioned, finite defaults without
hiding the value selected for an operation. The built-in identifiers are:

- `interactive/v1` for latency-sensitive request/response work
- `batch/v1` for bounded bulk processing
- `streaming/v1` for long uploads and downloads
- `webhook-delivery/v1` for short, concurrent delivery attempts

The zero `Config.Profile` selects `interactive/v1`. A profile resolves finite
operation timeout, retry attempts and elapsed time, request-pool concurrency
and elapsed time, transport connections, limiter wait, breaker open time,
cache body size, general body size, and shutdown time.

Profiles do not silently install retry, limiter, breaker, or cache middleware.
Their resolved values are explicit inputs for those independently configured
components. The operation timeout and an internally owned transport's
connection limit are applied by `Client` directly.

## Overrides and precedence

`PolicyOverrides` uses pointer fields so omission is distinct from an explicit
value. Every supplied duration, count, and byte limit must be positive and
bounded. Client overrides apply through `Config.Policy`:

```go
timeout := 20 * time.Second
connections := 64
client, err := httpclient.New(httpclient.Config{
	Profile: httpclient.PolicyProfileWebhookDeliveryV1,
	Policy: httpclient.PolicyOverrides{
		OperationTimeout:            &timeout,
		TransportMaximumConnections: &connections,
	},
})
```

Request overrides are immutable context snapshots:

```go
bodyLimit := int64(2 << 20)
ctx, err := httpclient.WithPolicyOverrides(ctx, httpclient.PolicyOverrides{
	BodyMaximumBytes: &bodyLimit,
})
request = request.WithContext(ctx)
```

Resolution always uses this precedence:

1. named profile defaults
2. client overrides
3. request overrides

The legacy `Config.Timeout` field is treated as a client override and takes
precedence over `Config.Policy.OperationTimeout` when both are supplied.

## Inspection and provenance

`Client.InspectPolicy(request)` resolves a request without executing it.
`ResolvedPolicy.Values()` returns a value copy, while `Provenance` identifies
`profile`, `client`, or `request` for each `PolicyField`.

During execution, operation and attempt middleware can call
`ResolvedPolicyFromContext`. Both stages see the same immutable resolved
snapshot, including provenance. This keeps logs and metrics inspectable while
leaving raw credentials, tenants, account identifiers, paths, and queries out
of policy metadata.
