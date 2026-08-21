# Standalone Go Adoption

Adopt Golib one owned boundary at a time. Existing `net/http`, `database/sql`,
pgx, slog, or OpenTelemetry types remain valid where package APIs deliberately
use standard contracts.

1. Inventory current ownership, public behavior, cleanup, and failure policy.
2. Select one package whose boundary already matches that ownership.
3. Add contract tests around the old and new implementations.
4. Construct the Golib component explicitly; do not introduce a registry or
   service locator to make adoption look smaller.
5. Run both paths against identical fixtures and dependency versions.
6. Remove the former owner only after lifecycle, metrics, errors, and resource
   behavior match the accepted contract.

Use direct clients when the application needs only their native contract. Add
Golib when it owns reusable semantics such as bounded lifecycle, durable
settlement, protocol conformance, or consistent cross-service behavior.

All modules are independently versioned. Before public releases exist, use the
repository workspace for development; do not publish local replacements as
consumer guidance. After release, pin only the modules the application imports.

See [choosing packages](../choosing-packages.md),
[comparisons](../comparisons/index.md), and [versioning](../versioning.md).

Return to the [migration index](index.md).
