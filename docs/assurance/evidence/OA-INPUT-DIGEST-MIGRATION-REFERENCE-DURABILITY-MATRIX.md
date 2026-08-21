# Reference Durability Matrix Input-Digest Migration

The `pkg/service/integration/reference-durability` operational-assurance input
changed from
`455bdad5d44463f786da9b5eac9d5e06c02f3afdbde63b03408b4ff6d4ea5b54`
to
`196df1b199dfba931e7ccdd2da6e511695b9060444f1a763d26b1500a42ac51f`.

The reviewed change adds a separate PostgreSQL 14 through 18 composition
runner, immutable backend identity files, and documentation. It does not alter
`check-recovery.sh`, the recovery probe, the reference composition production
code, or the recovery assertions exercised by the retained process-death and
dependency-replacement campaign.

The prior recovery evidence therefore remains behaviorally identical. This is
a one-way, exact input-identity migration for that retained evidence only. It
does not validate any other previous digest, broaden the original claim, or
replace the separately executed PostgreSQL version-matrix evidence.
