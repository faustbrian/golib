package ruleenginemeasurement_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
	ruleenginemeasurement "github.com/faustbrian/golib/pkg/rule-engine/adapters/measurement"
)

func ExampleQuantity() {
	operators := ruleenginemeasurement.Operators()
	operator := operators[0]
	for _, candidate := range operators {
		if candidate.Name() == ruleenginemeasurement.OpQuantityGreaterThan {
			operator = candidate
		}
	}
	actual := ruleenginemeasurement.Quantity(
		measurement.MustNew(decimal.New(1001), measurement.Gram),
	)
	limit := ruleenginemeasurement.Quantity(
		measurement.MustNew(decimal.New(1), measurement.Kilogram),
	)
	matched, err := operator.Evaluate(context.Background(), actual, limit)
	fmt.Println(matched, err)
	// Output: true <nil>
}
