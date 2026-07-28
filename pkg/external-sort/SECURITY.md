# Security policy

Report vulnerabilities privately to the repository owner. Reports must not
contain production keys, records, paths, or customer data.

## Security properties

Temporary records are encrypted independently with AES-256-GCM and fresh
nonces from `crypto/rand`. Authenticated data binds the format version, chunk
number, record ordinal, and fixed record size. Corrupt, truncated, reordered,
or substituted records fail closed.

The caller owns key derivation and must provide a unique 32-byte storage key
for each independent sensitive dataset. The module never logs or returns keys,
records, or temporary paths in errors.

## Operational requirements

The configured parent must be a trusted local directory with no group or other
permissions. Place it on encrypted ephemeral storage, call `Close` on every
path, and separately clean stale process directories after crashes. The module
does not defend against a privileged host administrator, a compromised process,
memory inspection, rollback of the whole temporary filesystem, or denial of
service within caller-approved resource limits.
