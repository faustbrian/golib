# Operations

## Startup

Validate every configuration before accepting traffic. Construct static
authenticators immutably. Bound callback and discovery contexts. For remote JWT
keys, retain the returned `Remote` owner and fail startup if its initial fetch
fails. OIDC discovery also fails startup when metadata or its JWK URL is unsafe.

## Runtime

Monitor bounded `credential_kind`, `outcome`, `failure_kind`, and duration
attributes through `authlog` or `authotel`. A rise in rejected failures often
means expiration or rotation drift; unavailable failures mean a verifier,
network, issuer, or cache problem. Do not add subject, token, claim, key,
header, URL query, or cookie values to metrics or traces.

Set HTTP client timeouts and request deadlines appropriate to the service.
OIDC adds a 30-second client timeout only when the supplied client has none.
OIDC remote keys use conditional requests and bounded HTTP freshness. Configure
minimum and maximum refresh intervals and waiter capacity; overflow and
cancelled waiters fail unavailable instead of growing without bound. A failed
refresh observes a cooldown before another network attempt. JWT initialization
and refresh bounds remain explicit. Size limits should be
large enough for expected keys and claims but small enough to cap attacker
work.

## Rotation and outage

Overlap old and new static credentials. JWT refresh failures retain the
previous cached set. OIDC validates a cached known key during outage but
requires a successful synchronous fetch for an unknown key. A newly rotated
key may remain unknown until bounded freshness expires; coordinate issuer cache
headers and rollout timing. Alert on sustained
unavailable failures and on a rotation that never transitions to the new ID.

## Shutdown

Call `jwt.Remote.Close` with a bounded shutdown context. It cancels and joins
cache-owned goroutines, cancels and drains admitted refreshes, rejects new
operations after closing begins, and is repeatable. OIDC starts no background
goroutines.
The logging and telemetry adapters own no lifecycle; shut down their parent
`log` or `telemetry` runtime separately.
