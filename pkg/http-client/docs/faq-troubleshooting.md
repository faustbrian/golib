# FAQ And Troubleshooting

## Why was my request not retried?

Retry is endpoint policy, not a default. Confirm method safety, replayable body,
maximum attempts, caller context, endpoint unsafe opt-in, and an applied
idempotency key. A key alone never makes arbitrary work safe.

## Why did a redirect lose credentials or trace headers?

The origin changed. Credentials, cookies, trace context, baggage, and custom
sensitive headers are stripped at trust boundaries. Add explicit policy for the
new destination instead of forwarding old authority implicitly.

## Why did decoding close the body?

`DecodeResponse`, `DecodeJSONResponse`, status rejection, drain, transfer, and
file helpers consume body ownership. A successful raw `Client.Do` response is
caller-owned. See `responses.md` for the full matrix.

## Why does my custom transport reject egress or TLS configuration?

Core cannot prove an opaque transport revalidates DNS, proxy, redirects, or TLS
identity. Use the package-owned standard transport or enforce equivalent policy
entirely in the custom transport.

## Why is a fixture unmatched?

Matching is ordered and canonical but strict. Check method case, origin, path,
sorted non-redacted query, selected headers, and raw-body or digest policy.
`Verify` reports interactions left unused. Persisted raw request bodies are
rejected.

## How do I diagnose a timeout?

Check caller deadline, resolved operation policy, admission maximum wait, retry
elapsed budget, DNS/connect/TLS/header phases, and body processing separately.
Use `errors.Is`/`errors.As`; safe rendered messages intentionally omit the
dependency's raw detail.

`RetryOptions.MaximumElapsed` prevents another delay or attempt from starting;
it does not interrupt an attempt already in progress. Use the client total
timeout or caller context as the hard operation deadline.

## Why was authentication rejected before transport?

Trusted credential origins require HTTPS and reject URL userinfo or malformed
ports. Use a real TLS test server when possible. `AllowInsecure` is an explicit
local-test escape hatch, not a production transport policy.

## Can I use this as a generic REST client?

It preserves ordinary HTTP composition but intentionally does not provide an
untyped fluent `Get`/`Post` product. Put typed methods, models, and vendor errors
in a concrete vendor package.
