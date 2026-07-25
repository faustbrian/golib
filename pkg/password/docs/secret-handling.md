# Secret handling and side channels

Password byte slices remain caller-owned. Operations copy them before primitive
use, never retain them, and best-effort `clear` only the internal copy. Go may
copy memory, keep stack/register values, or optimize lifetimes; this package
does not claim guaranteed erasure.

Never convert passwords to strings for convenience, place them in contexts, or
include them in errors, logs, traces, metrics, panic values, test names, or
failure messages. Clear caller buffers only if the application owns them and
understands the same runtime limitations.

Argon2id derived outputs are compared with `crypto/subtle.ConstantTimeCompare`.
Bcrypt uses the maintained primitive's verification. Match/mismatch timing smoke
analysis catches obvious package regressions, but cannot prove side-channel
immunity. Parsing, malformed input, admission, user lookup, database latency,
and endpoint responses remain distinguishable.

Applications should use a valid dummy hash for absent users, return uniform
public authentication failures, rate-limit endpoints, and avoid exposing whether
a username exists. `passwordauth` requires dummy work but cannot equalize caller
lookup, network, database, or response behavior.

Encoded password hashes are not plaintext passwords, but they enable offline
guessing and are sensitive database material. `EncodedHash.String()` is an
explicit persistence escape hatch; diagnostic formatting is always redacted.

Observations intentionally omit password, salt, output, encoded hash, username,
subject, error cause, and attacker-controlled parameters. Keep metric labels to
the provided bounded enums.
