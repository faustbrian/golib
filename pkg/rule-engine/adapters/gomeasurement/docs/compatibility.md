# Compatibility

The stable public contract consists of the five operator names, their string
signatures, the v1 tag grammar, exact conversion policy, error classifications,
and caller-owned registration. Adding an operator or accepting a new tag
version requires an explicit compatibility decision because registries and
persisted rules may depend on the closed set.

The unit catalog and conversion definitions are owned by the measurement
module. Adding a unit there is compatible only when its canonical symbol and
dimension remain unambiguous and this adapter's matrix, fuzz, race, benchmark,
API, and mutation gates continue to pass.

Changing a unit symbol, dimension identity, relation name, operand signature,
tag grammar, rounding policy, or stable error classification is breaking.
