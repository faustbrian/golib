package knapsack_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/constraint"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

func exampleQuantity(value int64, unit measurement.Unit) measurement.Quantity {
	return measurement.MustNew(decimal.New(value), unit)
}

func exampleRequest() knapsack.NormalizedRequest {
	item, _ := knapsack.NewItem(knapsack.ItemSpec{
		ID: "shirt-1",
		Dimensions: knapsack.PhysicalDimensions{
			X: exampleQuantity(20, measurement.Centimetre),
			Y: exampleQuantity(15, measurement.Centimetre),
			Z: exampleQuantity(5, measurement.Centimetre),
		},
		Weight:       exampleQuantity(250, measurement.Gram),
		Orientations: []geometry.Orientation{geometry.OrientationXYZ},
	})
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "mailer",
		InternalDimensions: knapsack.PhysicalDimensions{
			X: exampleQuantity(30, measurement.Centimetre),
			Y: exampleQuantity(20, measurement.Centimetre),
			Z: exampleQuantity(10, measurement.Centimetre),
		},
		MaxContentWeight: exampleQuantity(1, measurement.Kilogram),
		Stock:            knapsack.UnlimitedStock(),
	})
	request, _ := knapsack.NewRequest(
		[]knapsack.Item{item}, []knapsack.ContainerType{box},
		knapsack.Resolution{
			Length: exampleQuantity(1, measurement.Centimetre),
			Mass:   exampleQuantity(1, measurement.Gram),
		},
		knapsack.DefaultLimits(),
	)
	return request.Normalized()
}

func Example_packAll() {
	request := exampleRequest()
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	fmt.Println(err, plan.Status(), len(plan.Containers()), len(plan.Placements()))
	// Output: <nil> feasible 1 1
}

func Example_fixedContainers() {
	request := exampleRequest()
	plan, err := (solver.Heuristic{}).PackFixed(
		context.Background(), request,
		[]knapsack.ContainerInstance{{ID: "supplied-mailer", TypeID: "mailer"}},
		solver.Options{},
	)
	fmt.Println(err, plan.Containers()[0].ID)
	// Output: <nil> supplied-mailer
}

func Example_verifySuppliedPlan() {
	request := exampleRequest()
	plan, _ := (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	result := verify.Plan(request, plan, verify.RequireAll())
	fmt.Println(result.Valid(), len(result.Violations()))
	// Output: true 0
}

type rejectMailers struct{}

func (rejectMailers) Check(_ context.Context, view constraint.PlacementView) constraint.Decision {
	if view.Container().ID == "mailer" {
		return constraint.Reject("mailer_disabled", "mailers are disabled for this order")
	}
	return constraint.Accept()
}

func Example_customConstraint() {
	request := exampleRequest()
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{
		AllowUnpacked: true,
		Constraints:   []constraint.Placement{rejectMailers{}},
	})
	fmt.Println(err, plan.Status(), plan.UnpackedItemIDs())
	// Output: <nil> best_known [shirt-1]
}
