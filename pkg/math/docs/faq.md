# FAQ

**Why not one Number interface?** The four families have incompatible closure,
precision, and rounding semantics.

**Why are JSON values strings?** JSON number decoders commonly narrow through
binary floating point.

**Does Decimal preserve trailing zeros?** Construction and quantization can;
use `SameRepresentation` when scale matters and `Equal` for numeric equality.

**When should I use built-in numbers?** Whenever the domain is bounded and
their overflow and precision behavior is correct.

