# Threat model and SSRF matrix

The attacker controls request bytes, signature headers, event IDs, endpoint
URLs, DNS answers, responses, and timing. The operator controls trusted keys,
clock, limits, replay store, and endpoint allow/deny policy.

| Threat | Control | Residual responsibility |
| --- | --- | --- |
| Forgery/body substitution | HMAC, exact-byte digest, constant-time compare | Protect and rotate secrets |
| Replay/race | Signed nonce plus atomic hashed event replay key, fail closed | Durable tenant-scoped store and TTL |
| Header/body DoS | Pre-decode count and byte bounds | Set deployment-specific limits |
| Timestamp abuse | Signed seconds and bounded trusted-clock skew | Synchronize the host clock |
| SSRF/rebinding | URL validation plus dial-time resolution validation | Configure explicit exceptional allow ranges |
| Redirect bypass | Secure client does not follow redirects | Do not replace its redirect policy |
| Proxy bypass | Secure transport disables environment proxies | Audit custom transports |
| Retry abuse | Bounded attempts/delay; idempotency required | Choose endpoint-safe idempotency semantics |
| Secret leakage | Typed external errors and fixed observation fields | Do not log requests or raw errors |

`SSRFPolicy` defaults to HTTPS and denies userinfo, fragments, noncanonical or
non-ASCII hosts, invalid ports, private, loopback, link-local, multicast,
unspecified, carrier-grade NAT, benchmarking, documentation, and other
reserved ranges. Every DNS answer must pass and answer count is bounded.
IPv4-mapped IPv6 is unmapped before policy checks. Explicit denied prefixes
win over explicit allowed prefixes.

Allowing HTTP or private prefixes is an operator exception intended for
controlled networks and tests. TLS certificate and hostname verification stay
owned by `net/http`. Application authorization and payload schema validation
remain out of scope.
