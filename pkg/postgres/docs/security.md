# Security and threat model

## Assets

- PostgreSQL credentials, client keys, certificates, DSNs, and network routes
- SQL arguments and rows, including tenant and personal data
- database availability, connection budget, locks, and transaction integrity
- schema names and server diagnostics

## Threats and controls

| Threat | Control | Application responsibility |
| --- | --- | --- |
| DSN disclosure | validation/startup strings omit DSNs; fuzz regression | do not log input config or unwrapped errors blindly |
| TLS downgrade or impersonation | typed all-host TLS override; TLS guide | verified roots, names, secret rotation, network policy |
| pool exhaustion | finite maximum and acquisition timeout; stats | concurrency limits, capacity budget, leak prevention |
| cancellation loss | caller contexts propagated; uncanceled rollback only | set request/job and statement deadlines |
| partial transaction cleanup | one commit or rollback path; joined errors | avoid external side effects or own retry/idempotency |
| telemetry data leak | bounded event schema; no SQL/arguments/raw errors | safe exporters, allow-listed query names |
| cardinality attack | fixed operation/outcome/kind/state values | never add tenant/input labels in adapters |
| malformed DSN crash | panic containment plus fuzz corpus | treat config as untrusted deployment input |
| unsafe runtime feature | GO-SAFETY-1 scan forbids unsafe/cgo/linkname | review dependencies and vulnerability reports |

`ErrorInfo.Detail` and `Hint` intentionally preserve native diagnostic data for
authorized application policy, but are not safe log fields. `Config.Configure`
is a trusted extension boundary and can weaken TLS, remove timeouts, install
unsafe tracers, or add hooks; review it as production code.

Typed TLS overrides copy certificate pools, protocol slices, and certificate
bytes. Callback functions, private keys, session caches, randomness, clocks,
and writers remain application-owned and must be safe for concurrent use.

Native hook and tracer panics are not converted into ordinary database errors.
Treat hooks as trusted process code, keep them bounded, and return errors for
expected connection rejection. This package installs no query tracer itself,
so duplicate tracer installation is controlled entirely by the application's
single `Config.Configure` composition point.

No package can make an arbitrary transaction closure safe to retry. Network
calls and emitted messages may escape PostgreSQL rollback. The module exposes
classification only and leaves execution policy to the application.
