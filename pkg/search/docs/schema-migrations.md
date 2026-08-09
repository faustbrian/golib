# Schema migrations

An `IndexDefinition` contains an immutable physical name, mapping, settings,
and deterministic fingerprint. Compatibility checks decide whether an existing
index is reusable or requires migration.

The resumable workflow is: authorize lifecycle access, create a versioned
index, reindex with external versions, persist a checkpoint, verify counts and
drift, atomically swap the logical alias, observe, then clean up only after the
rollback window. A failed cutover keeps the old alias. Rollback swaps the alias
back to the verified previous index. Cleanup is a separately authorized and
observable destructive step.

Never mutate incompatible mappings in place or infer completion from a task
submission response. Persist migration state outside the search index.
