# Threat model

## Scope and assets

The protected assets are schedule definitions, physical occurrence identity,
lease ownership and fencing order, queue envelopes, idempotency records, and
operator recovery authority. The scheduler coordinates decisions; it does not
protect arbitrary downstream side effects unless those systems validate the
fencing token or occurrence key.

Trust boundaries are:

- application code entering through definitions, conditions, hooks, observers,
  and executors;
- scheduler replicas communicating through PostgreSQL or Valkey;
- queue submission and idempotency completion in separate durable systems;
- HTTP and CLI inspection or recovery surfaces;
- Kubernetes termination, rollout, identity, network, and secret management;
- the host Go runtime and IANA time-zone database.

## Safety invariants

- One active lease key has at most one current owner and fencing token.
- Every takeover receives a strictly larger fencing token.
- A stale owner cannot heartbeat, release, or recover the current lease.
- Matching rollout revisions use the same coordination and occurrence keys.
- A timeout, malformed response, or outage never proves ownership.
- Missed-run work, callback goroutines, executor goroutines, history, payloads,
  registry size, and backend waits remain within documented budgets.
- Civil-time calculation is deterministic for the same definition, instant,
  Go runtime, and IANA data.
- The public contract never claims exactly-once execution.

## Threats and controls

| Threat or fault | Control | Residual exposure |
|---|---|---|
| two replicas dispatch one boundary | atomic occurrence lease and stable physical occurrence key | duplicate remains after lease expiry or external crash window |
| stale task writes after takeover | monotonic fencing token carried to executor and queue | downstream systems must enforce the fence |
| lease deletion by old owner | owner-and-token compare on release and recovery | operator can still force unsafe recovery after isolation failure |
| local clock skew | PostgreSQL and Valkey server time | an unhealthy backend clock affects every client |
| DST or calendar anomaly | explicit IANA zone, physical instants, 400-year corpus | different runtime zone databases can disagree during rollout |
| backend outage or latency | per-operation context, fail-closed errors, same-key retry | timeout can have an unknown committed outcome |
| backend key eviction | Valkey 9 startup check requires `noeviction`; fence counter is separate | administrative deletion can destroy history |
| callback or executor never returns | deadlines, tracked goroutines, fixed capacities, drain deadline | Go cannot forcibly terminate the code |
| panic in application callback | panic containment | hook or observer result is best-effort |
| oversized definitions or recovery input | exported byte, count, scan, and body limits | application-owned queue payload expansion needs its own limit |
| queue redelivery or scheduler crash | occurrence idempotency key and optional durable wrapper | queue and idempotency completion are not one transaction |
| split rollout identity | stable coordination ID and rollout matrix | renamed tasks or changed parameters deliberately diverge |
| unauthorized recovery | exact fencing token plus external authentication and network policy | core HTTP package does not implement application identity |
| secret disclosure | no credential fields in definitions or envelopes; Kubernetes Secrets | application metadata can still leak data if misused |
| shell injection | core has no shell executor | an application-added process adapter owns isolation and escaping |

## Operational assumptions

Every replica must have a unique owner value and use the same backend,
namespace, schedule definitions, Go runtime, and IANA data during steady state.
PostgreSQL or Valkey high availability must expose one authoritative data set;
split-brain storage invalidates lease safety. Valkey must be version 9 or newer
with `maxmemory-policy noeviction`. Recovery endpoints must be authenticated,
authorized, rate-limited, and audited by the hosting application.

Long-running or externally visible business work should be dispatched to a
durable queue. Workers must deduplicate occurrence keys. Any store accepting a
fencing token must reject a token lower than the largest already applied for
the protected resource.

## Release review

Reviewers should trace every new state mutation through cancellation, timeout,
retry, stale ownership, crash before commit, crash after commit, and rollout
coexistence. New adapters must pass the shared conformance suite plus live
outage, latency, reconnect, clock-authority, and retained-fence cases. Any
change to identity, cron calculation, lease scripts, migration SQL, or shutdown
requires an explicit matrix update in the hardening report.
