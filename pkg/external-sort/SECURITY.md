# Security policy

Report vulnerabilities privately to the repository owner. Reports must not
contain production keys, records, paths, or customer data.

## Security properties

Temporary records are encrypted independently with AES-256-GCM. Each nonce
combines a random-seeded, synchronized 64-bit process-wide store
domain with a monotonic 32-bit per-store record counter. Concurrent stores
therefore have disjoint nonce domains even when deterministic dataset entropy
repeats, and records cannot reuse a nonce within `MaximumRecords`; sequence
exhaustion fails closed. Each store also receives a random identity, and both
the identity and nonce domain are authenticated.
Authenticated data binds that identity, the format version, chunk number,
record ordinal, and fixed record size. Corrupt, truncated, reordered,
duplicated, cross-linked, or cross-store substituted records fail closed.

The caller owns key derivation and must provide a unique 32-byte storage key
for each independent sensitive dataset. Module-generated errors never contain
keys, records, or temporary paths; caller callback and context errors are
returned unchanged and remain the caller's responsibility.

## Operational requirements

The configured parent must be a trusted local directory with no group or other
permissions. Each store revalidates and binds that directory to a rooted handle
so parent-path rename or replacement cannot redirect storage or cleanup. Place
it on encrypted ephemeral storage, call `Close` on every path, and use the
non-following, descriptor-relative stale-directory policy in
[the operations guide](docs/operations.md) after crashes. The module does not
defend against a privileged host administrator, a compromised process, memory
inspection, rollback of the whole temporary filesystem, or denial of service
within caller-approved resource limits.
