# Migration

Choose one family from the domain invariant, not the source representation.
Replace integer wrappers with `integer.Integer`, exact ratios with `Rational`,
money-like base-10 values with `Decimal`, and only explicitly binary numerical
algorithms with `bigfloat.Float`. Parse persisted values as strings, define
limits centrally, and make every former implicit rounding point an explicit
context or quantization call.

During migration, compare serialized fixtures and run both implementations at
consumer boundaries. Do not convert legacy decimal values through `float64`.

