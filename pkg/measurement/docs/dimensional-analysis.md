# Dimensional Analysis

V1 uses a closed dimension set instead of symbolic algebra. Addition,
subtraction, comparison, and clamping require identical dimensions. Multiplying
length by length produces area; area by length produces volume; density by
volume produces mass. Dividing mass by volume produces density, volume by
length produces area, and equal dimensions produce a dimensionless ratio.

Operations whose result is outside the closed set return
`ErrUnsupportedDimension`. This prevents a typo from inventing an unreviewed
unit ontology. Unknown units and incompatible dimensions fail before decimal
arithmetic.

Absolute temperatures are affine values. They can be converted and compared,
but adding or subtracting them returns `ErrAffineArithmetic`; temperature
intervals require a future distinct dimension. Loading metre is a semantic
logistics dimension and is not interchangeable with length.
