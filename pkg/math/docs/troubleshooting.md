# Troubleshooting

`ErrInvalidSyntax` means the selected strict grammar rejected the input; enable
sign, exponent, underscore, whitespace, or leading-zero options deliberately.
`ErrLimitExceeded` means work was rejected before unsafe allocation; lower the
input size or raise a reviewed limit. `ErrConversion` from exact decimal
division means the rational expansion repeats. A trapped-condition error still
returns the decimal result and its conditions for diagnostics.

