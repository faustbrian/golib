# Threat model

## Assets

- Caller password bytes during synchronous execution.
- Encoded hashes, salts, and derived outputs.
- Hashing policy and resource budgets.
- Correct match/mismatch and upgrade decisions.
- Availability under attacker-controlled password/hash input.

## Trust boundaries

Password input, encoded database values, contexts, lookups, entropy failures,
observers, queue pressure, and shutdown timing may be malformed, unavailable,
or adversarial. The maintained Go cryptographic implementations and Go runtime
are trusted within their documented behavior.

## Controls

| Threat | Control |
| --- | --- |
| Custom/weak cryptography | Maintained `x/crypto` Argon2id and bcrypt only |
| Parameter bomb | Parse and bound time, memory, lanes, cost, salt, output first |
| Parser ambiguity | Canonical fixed-order grammar and strict base64 |
| CPU/memory exhaustion | Input bounds, admission concurrency, bounded queue |
| Queue retention | Context cancellation, queue ceiling, drainable shutdown |
| Hash downgrade | Monotonic rehash rules; never Argon2id to bcrypt |
| Stale concurrent upgrade | Expected/replacement database CAS |
| Entropy failure | Complete `crypto/rand` salt or classified failure |
| Diagnostic leakage | Redacted formatters and cause-free error strings |
| Metric cardinality/leakage | Bounded observation enums only |
| Observer failure | Panic isolation; observation cannot change result |
| Caller-buffer retention | Internal copy, no retained password field |

## Explicit non-protections

- Compromised process memory, debugger, kernel, runtime, or host.
- Guaranteed memory wiping or side-channel immunity.
- Application logs/traces of raw inputs before this package.
- Username enumeration, endpoint rate limiting, breach lookup, or strength UI.
- TLS, user lifecycle, sessions, MFA, recovery, or authorization.
- Database confidentiality after hash exfiltration and offline guessing.
- A malicious lookup, observer, or entropy implementation supplied by tests.

## Abuse cases

Hostile encodings cannot request work above policy ceilings. A flood of valid
maximum-cost hashes can occupy only `Concurrent` operations and `Queue` waiters;
additional calls fail explicitly. Applications still need network-level rate
limits, endpoint timeouts, and capacity planning.

Go does not expose a recoverable general-purpose allocation failure. A process
may terminate if its trusted policy and container limit are incompatible. The
package therefore validates generated lengths, rejects attacker-selected work
before primitive allocation, and bounds simultaneous default-policy work; it
does not claim to catch runtime out-of-memory termination. `make resource`
exercises that boundary with two 64 MiB slots and eight concurrent callers.

Malformed and missing-user paths may be faster than successful verification.
Use a dummy hash and uniform responses, but treat complete timing equalization
as an application/system concern.
