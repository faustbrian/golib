# Conversion and dimensions

Each operator parses both tagged quantities, asks the measurement package to
compare them with `measurement.ExactConversion()`, and applies its relation to
the resulting ordering. The left quantity's unit is the comparison target;
changing operand order can therefore change whether a particular conversion
has a terminating decimal representation, but it cannot silently round.

The supported dimension identities are dimensionless, length, area, volume,
mass, absolute temperature, density, and loading metre. Only quantities with
the same identity are comparable. Loading metres remain distinct from ordinary
length, despite their names, and affine temperature offsets are handled by the
measurement conversion definitions.

If a conversion is exact and terminating, equivalent units compare normally.
If it would require a non-terminating decimal or exceed arithmetic limits, the
operator returns `ErrInvalidQuantity` while preserving the measurement or math
cause. Different dimensions return `ErrIncompatibleQuantity` and preserve
`measurement.ErrDimensionMismatch`.
