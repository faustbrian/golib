# Adoption

1. Construct quantities through `measurement.New` or `measurement.MustNew`.
2. Convert them to rule values only through `ruleenginemeasurement.Quantity`.
3. Register `Operators()` on each compiler that evaluates quantity rules.
4. Use the quantity-specific operator names; built-in string ordering compares
   encoded text and is not a measurement comparison.
5. Store the complete tagged value without localization or normalization.
6. Handle invalid and incompatible errors separately and retain context errors.

Adopt this adapter when rule facts already have explicit exact units. If an
input omits a unit, reject it before rule evaluation or resolve it through a
caller-owned domain policy; this adapter will not infer one.
