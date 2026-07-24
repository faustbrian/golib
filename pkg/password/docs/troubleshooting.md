# Troubleshooting

## `ErrMalformedHash`

Check exact prefix, separator count, fixed Argon parameter order, canonical
decimal fields, unpadded base64, decoded lengths, and bcrypt length/alphabet.
Treat malformed stored hashes as data/operational failures, not mismatches.

## `ErrResourceRejected`

The password, encoding, Argon fields, bcrypt cost, salt, or output exceeds the
policy. Do not raise limits based only on attacker input. Inventory legitimate
stored parameters, benchmark them, and change policy deliberately.

## `ErrAdmission`

The bounded queue is full. Reduce arrival rate, shorten endpoint timeouts,
increase capacity only after CPU/memory measurement, or scale pods. Never add an
unbounded retry loop.

## `ErrClosed`

Lifecycle shutdown has begun. Stop routing new requests and wait for drain.
Admission cannot be reopened; construct the next service lifecycle explicitly.

## `ErrEntropy`

Production `crypto/rand.Reader` could not fill a salt. Fail closed and investigate
the host/runtime. Never fall back to deterministic, timestamp, math/rand, or
reused salts.

## Laravel hash does not verify

Confirm the literal candidate encoding, database collation/whitespace, `$2y$`
prefix, PHP options, and that no layer transformed `+`, `/`, or `$`. Reproduce
with synthetic credentials; never paste production passwords into tests.

## Memory spikes or OOM

Multiply Argon memory by active concurrency, include application/GC baseline,
and add headroom. Lower admission concurrency before weakening the hash policy.

## Upgrade CAS affects zero rows

A concurrent writer changed the hash. Do not overwrite it. Treat the login as a
successful verification and let a later login re-evaluate the current value.
