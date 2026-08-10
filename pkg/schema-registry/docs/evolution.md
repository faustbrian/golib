# Evolution and compatibility

Applications own evolution policy. Before registering, select a mode advertised
by the provider and call `CheckCompatibility`. Unsupported is not compatible.
Confluent checks use the subject's configured mode and reject a caller-requested
mode that does not match it. Glue cannot perform an equivalent dry-run; its
configured policy is enforced by registration, so applications must treat a
failed or indeterminate registration as non-deployable.
Provider-specific checks require both the explicit provider-specific mode and a
nonempty provider mode name; portable requests cannot silently carry provider
extensions.

Backward means new readers can consume prior data; forward means prior readers
can consume new data; full requires both. Transitive modes compare the complete
provider-defined history. Format rules still differ, so a shared mode label is
not proof of identical evolution behavior.

Use immutable fixtures and independent clients to verify intended changes.
Never downgrade compatibility automatically after a rejection. Coordinate
producer and consumer rollout before deleting any version.
