# API

`Quantity(measurement.Quantity) ruleengine.Value` creates the canonical v1
tagged string. Supply a quantity constructed by the measurement package; a
zero-value `measurement.Quantity` has no unit and is rejected if evaluated.

`Operators() []ruleengine.Operator` returns five fresh operators:

| Name | Relation |
| --- | --- |
| `quantity_equal` | equal |
| `quantity_less_than` | less than |
| `quantity_less_or_equal` | less than or equal |
| `quantity_greater_than` | greater than |
| `quantity_greater_or_equal` | greater than or equal |

Every operator declares exactly `(KindString, KindString)`. Register the
returned set explicitly with `ruleengine.NewCompilerWithOperators`.

`ErrInvalidQuantity` classifies wrong kinds, malformed or noncanonical tags,
unknown units, size violations, and exact conversions that cannot be
represented. `ErrIncompatibleQuantity` classifies different dimensions.
Underlying measurement, decimal, math, and context causes remain detectable
with `errors.Is`; diagnostics do not reproduce the supplied amount or unit.

`MaxTaggedValueBytes` is the adapter's hard encoded-input bound.
