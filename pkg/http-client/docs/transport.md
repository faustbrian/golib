# Transport, Timeouts, Proxies, And Redirects

`New(Config{})` builds a standard `http.Client` with finite policy. The default
interactive profile bounds the logical operation at 30 seconds. Its owned
`http.Transport` uses a 10-second connect timeout, 30-second TCP keepalive,
10-second TLS handshake timeout, 15-second response-header timeout, one-second
expect-continue timeout, 90-second idle timeout, finite connection pools, a
1 MiB response-header limit, TLS 1.2 minimum, and HTTP/2 negotiation.

Use `PolicyProfileBatchV1`, `PolicyProfileStreamingV1`, or
`PolicyProfileWebhookDeliveryV1` for another documented workload baseline.
`Config.Policy` overrides a profile and `WithPolicyOverrides` applies a final
request-local override. `Client.InspectPolicy` exposes values and provenance.

## Ownership and reuse

The package-owned transport is owned and closed by the client. A custom
`Config.Transport` is borrowed unless `TransportOwnership` is
`TransportOwned`. `CloseIdleConnections` is always safe, but `Close` only
drains an owned transport. `Close` also cancels pending operations and closes
still-registered response bodies; callers should still close every response as
soon as its operation finishes.

Share one client and transport across concurrent calls. Do not construct a
transport per request. Transport pooling and `Pool` execution concurrency are
separate controls: pool concurrency limits application work while transport
limits govern sockets per host.

## Proxy and DNS behavior

The default standard transport honors `ProxyFromEnvironment`. With egress
policy enabled, proxy targets and their resolved addresses are checked by the
same scheme, host, port, CIDR, and address-class rules as direct targets. DNS
answers are validated before dialing numeric addresses. A custom transport is
incompatible with `Config.Egress` and `Config.TLS` because these guarantees
cannot be proven across an opaque dial path.

## Redirects

Standard `net/http` redirect mechanics remain visible. Operation middleware
runs once while attempt middleware runs for every redirect hop. Authentication,
cookies, trace context, baggage, and configured sensitive headers are stripped
when the origin changes. Egress policy revalidates every destination. A
non-replayable body cannot be automatically replayed.

## Timeout diagnosis

Distinguish the logical operation timeout from DNS, connect, TLS, response
header, middleware admission, retry elapsed, body, and caller deadlines.
Preserve causes with `errors.Is` and `errors.As`; rendered `TransportError`
messages deliberately omit query, userinfo, and dependency error text.

Tune only from measurement. A larger total timeout cannot repair a smaller
response-header bound, and more connections cannot repair a downstream rate
limit. Long-lived streaming calls should use the streaming profile plus an
explicit request deadline appropriate for the vendor contract.
