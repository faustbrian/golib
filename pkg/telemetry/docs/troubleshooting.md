# Troubleshooting

## No telemetry arrives

1. Confirm the signal is enabled and `Init` succeeded.
2. Inspect the explicit protocol, endpoint, URL path, TLS, and headers.
3. For gRPC use `host:4317`; for HTTP use `host:4318` plus signal paths.
4. Check Collector receivers, pipelines, refusal counters, and backend export.
5. Call `ForceFlush` from a bounded diagnostic or shutdown path and inspect the
   complete joined error.

Do not infer Collector health from application readiness.

## TLS failures

Verify the mounted CA is PEM, `ServerName` matches the certificate, and client
certificate/key are configured together. `InsecureSkipVerify` weakens identity
verification and should be a temporary, explicit compatibility setting.

## Authentication failures

Exporter headers are copied during construction. Confirm the configured key,
scheme, token scope, Secret mount, and Collector authenticator. Do not print
header values while debugging.

## High memory or dropped spans

Inspect request rate, sampling ratio, spans per operation, queue size, batch
timeout, Collector availability, and backend throttling. Do not make queues or
retry elapsed time unbounded. Reduce volume or restore downstream capacity.

## Unexpected metric series

Inspect views and attribute allow-lists. Search for identifiers, raw routes,
errors, hosts, queue names, or baggage. Lower the cardinality limit to contain
impact, then remove the uncontrolled dimension; hashing it is not a fix.

## Broken traces

Confirm `traceparent` reaches the service, the handler uses the runtime
propagator, and parent-based sampling is enabled. Proxies must preserve W3C
headers. Oversized or malformed headers are intentionally ignored.

## Baggage missing

Baggage is intentionally absent unless enabled, allow-listed, within bounds,
and extracted through a trusted boundary. For HTTP, set `TrustedInbound` only
on authenticated internal handlers. Public handlers always drop baggage.

## Shutdown takes too long

Use a fresh bounded context derived with `context.WithoutCancel`; an already
cancelled signal context aborts immediately. Verify timeout and retry horizons,
then inspect each joined error. Shutdown is idempotent; repeated calls do not
restart export.

## Duplicate initialization

Only one runtime may register process globals. Reuse it, set
`RegisterGlobal = false` for an isolated provider, or correct ownership. The
rejected runtime cleans up partial providers.
