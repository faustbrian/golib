# Migration notes

Before v1, public API adjustments may occur but are recorded in the changelog.
Pin a tagged version rather than `main`.

When migrating from `encoding/csv`, note that `NewDelimitedReader` requires an
explicit delimiter, header processing is opt-in, normalization returns copies,
and parser errors are wrapped in stable tabular kinds.

When migrating from direct Excelize use, this package intentionally exposes
only ordered strings, cell-error policy, sheet selection, limits, and common
row semantics. Code requiring styles, formulas, merged cells, or editing
should continue to use a spreadsheet-specific API.

When replacing another XLS library, validate real BIFF8 fixtures. The internal
reader supports a documented subset and rejects unsupported or corrupt
structures rather than attempting recovery.
