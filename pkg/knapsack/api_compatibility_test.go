package knapsack_test

import (
	"context"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/constraint"
	packingjson "github.com/faustbrian/golib/pkg/knapsack/encoding"
	"github.com/faustbrian/golib/pkg/knapsack/objective"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
)

// Compile-time assignments pin the initial cross-package adoption surface.
var (
	_ func(knapsack.ItemSpec) (knapsack.Item, error)                                                                                          = knapsack.NewItem
	_ func(knapsack.ContainerTypeSpec) (knapsack.ContainerType, error)                                                                        = knapsack.NewContainerType
	_ func(knapsack.NormalizedSpec) (knapsack.NormalizedRequest, error)                                                                       = knapsack.NewNormalizedRequest
	_ func(knapsack.PlanSpec) (knapsack.Plan, error)                                                                                          = knapsack.NewPlan
	_ func(knapsack.NormalizedItem, knapsack.NormalizedContainer, knapsack.Placement, []knapsack.Placement) (constraint.PlacementView, error) = constraint.NewPlacementView
	_ func([]constraint.Placement) error                                                                                                      = constraint.ValidateCallbacks
	_ func(context.Context, constraint.Placement, constraint.PlacementView) (constraint.Decision, error)                                      = constraint.Evaluate
	_ func(...objective.Criterion) (objective.Objective, error)                                                                               = objective.New
	_ func([]byte, packingjson.Limits) (knapsack.NormalizedRequest, error)                                                                    = packingjson.UnmarshalRequest
	_ func([]byte, packingjson.Limits) (knapsack.Plan, error)                                                                                 = packingjson.UnmarshalPlan
	_ func(knapsack.NormalizedRequest, knapsack.Plan, verify.Options) verify.Result                                                           = verify.Plan
	_ interface {
		PackAll(context.Context, knapsack.NormalizedRequest, solver.Options) (knapsack.Plan, error)
		PackFixed(context.Context, knapsack.NormalizedRequest, []knapsack.ContainerInstance, solver.Options) (knapsack.Plan, error)
	} = solver.Heuristic{}
	_ interface {
		PackAll(context.Context, knapsack.NormalizedRequest, solver.Options) (knapsack.Plan, error)
		PackFixed(context.Context, knapsack.NormalizedRequest, []knapsack.ContainerInstance, solver.Options) (knapsack.Plan, error)
	} = solver.Exact{}
)
