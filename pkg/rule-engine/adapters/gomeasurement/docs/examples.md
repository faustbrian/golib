# Examples

## Shipment threshold

```go
weight := ruleengine.MustPath("shipment", "weight")
limit := measurement.MustNew(decimal.New(1), measurement.Kilogram)

rules := ruleengine.RuleSet{ID: "weight", Rules: []ruleengine.Rule{{
    ID: "over-limit",
    When: ruleengine.Compare(
        ruleenginemeasurement.OpQuantityGreaterThan,
        ruleengine.Variable(weight),
        ruleengine.Literal(ruleenginemeasurement.Quantity(limit)),
    ),
}}}

compiler, err := ruleengine.NewCompilerWithOperators(
    ruleengine.DefaultLimits(),
    ruleenginemeasurement.Operators()...,
)
```

A `1001 g` fact matches the `1 kg` threshold because conversion is exact.

## Inspecting failures

```go
matched, err := operator.Evaluate(ctx, left, right)
switch {
case errors.Is(err, ruleenginemeasurement.ErrIncompatibleQuantity):
    // The units belong to different dimensions.
case errors.Is(err, ruleenginemeasurement.ErrInvalidQuantity):
    // The tag or exact conversion is invalid.
case err != nil:
    // Context cancellation or deadline.
default:
    _ = matched
}
```

Runnable examples are also maintained in `example_test.go` and compiled by the
Go test suite.
