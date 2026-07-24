# Troubleshooting

`ErrUnknownUnit` means the unit is unsupported or absent from the selected
profile. `ErrDimensionMismatch` means operands or formula inputs have different
dimensions. `ErrUnsupportedDimension` means multiplication or division would
leave the closed v1 ontology.

A shared `math` conversion error under `ExactConversion` means the quotient
does not terminate in base ten; choose and document a rounded scale. An
`ErrInvalidContext` means the zero context, invalid rounding mode, invalid
limits, or an out-of-range scale was supplied.

`ErrAffineArithmetic` means code attempted to add or subtract absolute
temperatures. Model the intended interval outside v1. Serialization errors
usually indicate numeric JSON, unknown fields, a missing unit, malformed
decimal text, trailing content, or a configured byte limit.
