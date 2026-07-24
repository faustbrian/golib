# Conversions and serialization

Text and JSON round-trip canonical exact values; JSON deliberately emits
strings to prevent decoder precision loss. Binary codecs are deterministic,
versioned, and preserve decimal representation and float precision/rounding.
Mutable `math/big` values are copied on input and output.

Narrowing conversions are explicit and fail unless exact. Locale-aware output
and SQL policy belong in consumer adapters because locale and database numeric
contracts are not numeric identity.

