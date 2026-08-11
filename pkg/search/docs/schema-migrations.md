# Schema migrations

An `IndexDefinition` contains an immutable physical name, mapping, settings,
and deterministic fingerprint. Compatibility checks decide whether an existing
index is reusable or requires migration.

Fingerprint wire contract v1 canonicalizes settings and mappings as JSON
objects using Go JSON number text and lexicographically ordered object keys,
then computes lowercase hex SHA-256 over `settings || 0x00 || mappings`. The
physical index name and backend server defaults are excluded. Applications must
materialize every behavior-relevant default, template, analyzer, and setting in
the supplied objects before fingerprinting. The known-answer fixture in
`TestIndexDefinitionFingerprintIsCanonicalAndCompatibilityIsExplicit` protects
cross-release compatibility. Changing this algorithm is a migration-protocol
break and requires a separately versioned contract; mixed versions must not
compare fingerprints produced by different algorithms.

The resumable workflow is: authorize lifecycle access, create a versioned
index, reindex with external versions, persist a checkpoint, verify complete
source/target semantics and the live target definition against the plan's
`IndexDefinition` fingerprint, then use `CutoverAlias` to hold an application
write fence across a fresh complete verification and the atomic logical-alias
mutation. Counts alone, sampling, or stored metadata without live definition
attestation cannot authorize cutover. A failure before dispatch keeps the old
alias; after dispatch, the backend must classify the alias outcome as known or
unknown and the application must resolve it before retrying.
Rollback uses the same verified cutover contract with the retained
`SourceFingerprint`; an unverified raw alias swap is not part of the core
lifecycle backend. Cleanup remains a separately authorized and observable
destructive step. Migration IDs, tenant labels, physical names, fingerprints,
and per-run reindex steps are bounded before authorization or backend work.

The `MigrationStore` supplied to `NewMigrator` must also implement
`MigrationCoordinator`. Its durable exclusive boundary is keyed by migration
ID, spans the complete callback including backend I/O, and applies equally to
`Run`, `Rollback`, and `Cleanup`. Process-local mutexes do not satisfy this
multi-instance contract. A coordinator that omits, repeats, or overlaps the
callback fails closed.

If alias mutation succeeds but the following migration checkpoint does not,
the next run fails with `ErrAliasChanged`; it does not infer that an already
moved alias proves the write fence and verification contract completed. Resolve
the alias outcome, reconcile durable writes, and repair migration state through
an application-owned recovery procedure before resuming. Rollback uses the same
fail-closed rule.

Before starting a backend reindex task, the migrator durably checkpoints
`MigrationDispatching`. If task submission or its following cursor checkpoint
is interrupted, a later run returns `ErrMigrationRecovery` instead of silently
submitting another task. Resolve the backend task outcome and repair the
application-owned migration state before resuming.

Before creating a physical generation, the migrator durably checkpoints
`MigrationCreating`. If creation or its completion checkpoint is interrupted,
a later run returns `ErrMigrationRecovery` instead of silently repeating a
create whose outcome may be ambiguous. Inspect the physical generation and
repair the application-owned migration state before resuming.

Before deleting an inactive generation, cleanup durably checkpoints
`MigrationCleaning`. If deletion or its completion checkpoint is interrupted,
a later cleanup returns `ErrMigrationRecovery` instead of risking deletion of a
new resource that reused the same physical name. Resolve the backend outcome
and repair the application-owned migration state before resuming.

Never mutate incompatible mappings in place or infer completion from a task
submission response. Persist migration state outside the search index.
