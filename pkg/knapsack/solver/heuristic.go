// Package solver provides bounded deterministic packing strategies.
package solver

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/constraint"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/objective"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
)

// Options selects deterministic solver extensions and partial-result policy.
// Collection fields are read through immutable callback views.
type Options struct {
	// Seed is recorded for reproducibility; built-in solvers use no ambient
	// randomness.
	Seed uint64
	// AllowUnpacked permits verified best-known plans that omit items.
	AllowUnpacked bool
	// Constraints are synchronous bounded custom placement predicates.
	Constraints []constraint.Placement
	// Objective is the built-in exact lexicographic objective. It conflicts
	// with PlanObjective when both are set.
	Objective objective.Objective
	// PlanObjective is a custom complete-plan comparison extension.
	PlanObjective objective.PlanObjective
}

// Heuristic is the deterministic bounded extreme-point solver.
type Heuristic struct{}
type bin struct {
	instance   knapsack.ContainerInstance
	info       knapsack.NormalizedContainer
	placements []knapsack.Placement
	items      []knapsack.NormalizedItem
	points     []geometry.Point
	weight     int64
}

// PackFixed packs into exactly the supplied finite instances and never creates
// another container. Unpacked items are reported without a false infeasibility
// proof.
func (Heuristic) PackFixed(ctx context.Context, request knapsack.NormalizedRequest, instances []knapsack.ContainerInstance, options Options) (knapsack.Plan, error) {
	if ctx == nil {
		return knapsack.Plan{}, knapsack.ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return knapsack.Plan{}, err
	}
	var err error
	options, err = validatedOptions(options)
	if err != nil {
		return knapsack.Plan{}, err
	}
	limits := request.Limits()
	if !limits.Valid() {
		return knapsack.Plan{}, knapsack.ErrInvalidRequest
	}
	if request.MemoryBytes() > limits.MaxMemoryBytes {
		return knapsack.Plan{}, knapsack.ErrMemoryBudgetExhausted
	}
	if _, ok := workingMemoryAvailable(request, len(instances), false); !ok {
		return knapsack.Plan{}, knapsack.ErrMemoryBudgetExhausted
	}
	items, types := request.Items(), request.Containers()
	if len(items) == 0 || len(types) == 0 || len(instances) == 0 {
		return knapsack.Plan{}, knapsack.ErrInvalidRequest
	}
	goal, err := resolvedObjective(options)
	if err != nil {
		return knapsack.Plan{}, err
	}
	typeByID := make(map[string]knapsack.NormalizedContainer, len(types))
	for _, container := range types {
		typeByID[container.ID] = container
	}
	seen := make(map[string]struct{}, len(instances))
	usedStock := make(map[string]uint32, len(types))
	bins := make([]*bin, 0, len(instances))
	for _, instance := range instances {
		info, ok := typeByID[instance.TypeID]
		if !ok || instance.ID == "" {
			return knapsack.Plan{}, knapsack.ErrInvalidContainer
		}
		if _, duplicate := seen[instance.ID]; duplicate {
			return knapsack.Plan{}, knapsack.ErrDuplicateID
		}
		usedStock[instance.TypeID]++
		if !info.Stock.Unlimited() && usedStock[instance.TypeID] > info.Stock.Count() {
			return knapsack.Plan{}, knapsack.ErrInsufficientStock
		}
		seen[instance.ID] = struct{}{}
		bins = append(bins, &bin{instance: instance, info: info, points: []geometry.Point{{}}})
	}
	slices.SortFunc(bins, func(a, b *bin) int { return strings.Compare(a.instance.ID, b.instance.ID) })
	slices.SortFunc(items, compareItems)
	unpacked := make([]string, 0)
	failedGroups := make(map[string]bool)
	var candidates uint64
	for index, item := range items {
		if err := ctx.Err(); err != nil {
			return interruptedHeuristicPlan(request, bins, append(unpacked, ids(items[index:])...), candidates, options, goal, err)
		}
		if item.Group != "" && failedGroups[item.Group] {
			unpacked = append(unpacked, item.ID)
			continue
		}
		chosen, _, placed, callbackErr := chooseHeuristicPlacement(ctx, request, item, bins, nil, nil, false, &candidates, options, goal, comparePlacement)
		if callbackErr != nil {
			return knapsack.Plan{}, callbackErr
		}
		if placed {
			bins = chosen
		}
		if !placed {
			if item.Group != "" {
				var removed []string
				bins, removed = rollbackGroup(bins, item.Group, false, nil)
				unpacked = append(unpacked, removed...)
				failedGroups[item.Group] = true
			}
			unpacked = append(unpacked, item.ID)
		}
		if candidates >= limits.MaxCandidatePlacements {
			return interruptedHeuristicPlan(request, bins, append(unpacked, ids(items[index+1:])...), candidates, options, goal, knapsack.ErrBudgetExhausted)
		}
	}
	var unbalanced []string
	bins, unbalanced = discardUnbalancedBins(bins, true)
	unpacked = append(unpacked, unbalanced...)
	unpacked = unique(unpacked)
	status, termination := knapsack.StatusFeasible, knapsack.TerminationCompleted
	if len(unpacked) > 0 {
		status, termination = knapsack.StatusBestKnown, knapsack.TerminationNoPlacement
	}
	plan, err := withObjective(ctx, request, buildPlan(bins, unpacked, status, termination, candidates, options.Seed, nil), goal)
	if err != nil {
		return knapsack.Plan{}, err
	}
	verificationOptions := verify.RequireAll()
	if options.AllowUnpacked || len(unpacked) > 0 {
		verificationOptions = verify.AllowUnpacked()
	}
	return plan, verifySolverPlan(request, plan, verificationOptions.WithObjective(goal).WithConstraints(options.Constraints...))
}

// PackAll uses deterministic extreme points and never reports heuristic
// optimality or proven infeasibility.
func (Heuristic) PackAll(ctx context.Context, request knapsack.NormalizedRequest, options Options) (knapsack.Plan, error) {
	if ctx == nil {
		return knapsack.Plan{}, knapsack.ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return knapsack.Plan{}, err
	}
	var err error
	options, err = validatedOptions(options)
	if err != nil {
		return knapsack.Plan{}, err
	}
	goal, err := resolvedObjective(options)
	if err != nil {
		return knapsack.Plan{}, err
	}
	primary, err := (Heuristic{}).packAllPass(ctx, request, options, goal, comparePlacement)
	if err != nil {
		return primary, err
	}
	limits := request.Limits()
	used := primary.Work().CandidatePlacements
	if limits.MaxImprovementRounds == 0 || used >= limits.MaxCandidatePlacements {
		return primary, nil
	}
	improvementLimits := limits
	improvementLimits.MaxCandidatePlacements -= used
	alternate, improvementErr := (Heuristic{}).packAllPass(ctx, request.WithLimits(improvementLimits), options, goal, comparePlacementWidthFirst)
	totalCandidates := used + alternate.Work().CandidatePlacements
	if improvementErr != nil {
		if isSearchInterruption(improvementErr) {
			return interruptedBestPlan(primary, totalCandidates, 1, improvementErr)
		}
		return knapsack.Plan{}, improvementErr
	}
	preferred, err := objectivePrefers(ctx, goal, request, alternate, &primary)
	if err != nil {
		if isSearchInterruption(err) {
			return interruptedBestPlan(primary, totalCandidates, 1, err)
		}
		return knapsack.Plan{}, err
	}
	selected := primary
	if preferred {
		selected = alternate
	}
	spec := selected.Spec()
	spec.Work.CandidatePlacements = totalCandidates
	spec.Work.ImprovementRounds = 1
	spec.Work.Strategy = "deterministic_extreme_point_repack"
	selected, _ = knapsack.NewPlan(spec)
	verificationOptions := verify.RequireAll()
	if options.AllowUnpacked || len(selected.UnpackedItemIDs()) > 0 {
		verificationOptions = verify.AllowUnpacked()
	}
	return selected, verifySolverPlan(request, selected, verificationOptions.WithObjective(goal).WithConstraints(options.Constraints...))
}

func (Heuristic) packAllPass(ctx context.Context, request knapsack.NormalizedRequest, options Options, goal objective.PlanObjective, compare func(knapsack.Placement, knapsack.Placement) int) (knapsack.Plan, error) {
	if err := ctx.Err(); err != nil {
		return knapsack.Plan{}, err
	}
	limits := request.Limits()
	if !limits.Valid() {
		return knapsack.Plan{}, knapsack.ErrInvalidRequest
	}
	if request.MemoryBytes() > limits.MaxMemoryBytes {
		return knapsack.Plan{}, knapsack.ErrMemoryBudgetExhausted
	}
	if _, ok := workingMemoryAvailable(request, request.ItemCount(), false); !ok {
		return knapsack.Plan{}, knapsack.ErrMemoryBudgetExhausted
	}
	items, types := request.Items(), request.Containers()
	if len(items) == 0 || len(types) == 0 {
		return knapsack.Plan{}, knapsack.ErrInvalidRequest
	}
	slices.SortFunc(items, compareItems)
	slices.SortFunc(types, compareContainers)
	usedStock := make(map[string]uint32)
	bins := make([]*bin, 0)
	unpacked := make([]string, 0)
	diagnostics := make([]knapsack.Diagnostic, 0)
	failedGroups := make(map[string]bool)
	var candidates uint64
	for index, item := range items {
		if err := ctx.Err(); err != nil {
			return interruptedHeuristicPlan(request, bins, append(unpacked, ids(items[index:])...), candidates, options, goal, err)
		}
		if item.Group != "" && failedGroups[item.Group] {
			unpacked = append(unpacked, item.ID)
			continue
		}
		chosen, newType, placed, callbackErr := chooseHeuristicPlacement(ctx, request, item, bins, types, usedStock, true, &candidates, options, goal, compare)
		if callbackErr != nil {
			return knapsack.Plan{}, callbackErr
		}
		if placed {
			bins = chosen
			if newType != "" {
				usedStock[newType]++
			}
		}
		if !placed {
			if item.Group != "" {
				var removed []string
				bins, removed = rollbackGroup(bins, item.Group, true, usedStock)
				unpacked = append(unpacked, removed...)
				failedGroups[item.Group] = true
			}
			unpacked = append(unpacked, item.ID)
			if uint64(len(diagnostics)) < uint64(limits.MaxDiagnostics) {
				diagnostics = append(diagnostics, knapsack.Diagnostic{Code: "no_feasible_placement", ItemID: item.ID, Message: "heuristic found no feasible placement within stock and work limits"})
			}
		}
		if candidates >= limits.MaxCandidatePlacements {
			return interruptedHeuristicPlan(request, bins, append(unpacked, ids(items[index+1:])...), candidates, options, goal, knapsack.ErrBudgetExhausted)
		}
	}
	var unbalanced []string
	bins, unbalanced = discardUnbalancedBins(bins, false)
	unpacked = append(unpacked, unbalanced...)
	for _, itemID := range unbalanced {
		if uint64(len(diagnostics)) >= uint64(limits.MaxDiagnostics) {
			break
		}
		diagnostics = append(diagnostics, knapsack.Diagnostic{
			Code: "center_of_gravity", ItemID: itemID,
			Message: "heuristic discarded a container whose content center of gravity was outside configured bounds",
		})
	}
	unpacked = unique(unpacked)
	status, termination := knapsack.StatusFeasible, knapsack.TerminationCompleted
	if len(unpacked) > 0 {
		status, termination = knapsack.StatusBestKnown, knapsack.TerminationNoPlacement
	}
	plan, err := withObjective(ctx, request, buildPlan(bins, unpacked, status, termination, candidates, options.Seed, diagnostics), goal)
	if err != nil {
		return knapsack.Plan{}, err
	}
	verificationOptions := verify.RequireAll()
	if options.AllowUnpacked || len(unpacked) > 0 {
		verificationOptions = verify.AllowUnpacked()
	}
	return plan, verifySolverPlan(request, plan, verificationOptions.WithObjective(goal).WithConstraints(options.Constraints...))
}

func chooseHeuristicPlacement(ctx context.Context, request knapsack.NormalizedRequest, item knapsack.NormalizedItem, bins []*bin, types []knapsack.NormalizedContainer, usedStock map[string]uint32, allowNew bool, candidates *uint64, options Options, goal objective.PlanObjective, comparisons ...func(knapsack.Placement, knapsack.Placement) int) ([]*bin, string, bool, error) {
	compare := comparePlacement
	if len(comparisons) > 0 {
		compare = comparisons[0]
	}
	_, groupStarted := groupTargets(bins, item.Group)
	var bestBins []*bin
	var bestPlan *knapsack.Plan
	bestNewType := ""
	for index, target := range bins {
		if groupStarted && !slices.ContainsFunc(target.items, func(existing knapsack.NormalizedItem) bool { return existing.Group == item.Group }) {
			continue
		}
		trial := cloneBins(bins)
		accepted, err := tryPlace(ctx, item, trial[index], candidates, request.Limits().MaxCandidatePlacements, options.Constraints, compare)
		if err != nil {
			return nil, "", false, err
		}
		if !accepted {
			continue
		}
		plan := buildPlan(trial, nil, knapsack.StatusBestKnown, knapsack.TerminationCompleted, *candidates, options.Seed, nil)
		preferred, err := objectivePrefers(ctx, goal, request, plan, bestPlan)
		if err != nil {
			return nil, "", false, err
		}
		if preferred {
			copy := plan
			bestPlan, bestBins, bestNewType = &copy, trial, ""
		}
	}
	if allowNew && !groupStarted {
		for _, container := range types {
			if !container.Stock.Unlimited() && usedStock[container.ID] >= container.Stock.Count() {
				continue
			}
			trial := cloneBins(bins)
			instanceNumber := usedStock[container.ID] + 1
			target := &bin{instance: knapsack.ContainerInstance{ID: fmt.Sprintf("%s#%06d", container.ID, instanceNumber), TypeID: container.ID}, info: container, points: []geometry.Point{{}}}
			accepted, err := tryPlace(ctx, item, target, candidates, request.Limits().MaxCandidatePlacements, options.Constraints, compare)
			if err != nil {
				return nil, "", false, err
			}
			if !accepted {
				continue
			}
			trial = append(trial, target)
			plan := buildPlan(trial, nil, knapsack.StatusBestKnown, knapsack.TerminationCompleted, *candidates, options.Seed, nil)
			preferred, err := objectivePrefers(ctx, goal, request, plan, bestPlan)
			if err != nil {
				return nil, "", false, err
			}
			if preferred {
				copy := plan
				bestPlan, bestBins, bestNewType = &copy, trial, container.ID
			}
		}
	}
	if bestPlan == nil {
		return bins, "", false, nil
	}
	return bestBins, bestNewType, true, nil
}

func cloneBins(source []*bin) []*bin {
	result := make([]*bin, len(source))
	for index, original := range source {
		copy := *original
		copy.placements = slices.Clone(original.placements)
		copy.items = slices.Clone(original.items)
		copy.points = slices.Clone(original.points)
		result[index] = &copy
	}
	return result
}

func tryPlace(ctx context.Context, item knapsack.NormalizedItem, target *bin, candidates *uint64, limit uint64, callbacks []constraint.Placement, comparisons ...func(knapsack.Placement, knapsack.Placement) int) (bool, error) {
	compare := comparePlacement
	if len(comparisons) > 0 {
		compare = comparisons[0]
	}
	if target.info.MaxItemCount > 0 && uint64(len(target.placements)) >= uint64(target.info.MaxItemCount) || target.weight > target.info.MaxContentWeight-item.Weight {
		return false, nil
	}
	if target.info.HasGrossWeight && target.weight > target.info.MaxGrossWeight-target.info.TareWeight-item.Weight {
		return false, nil
	}
	var chosen *knapsack.Placement
	chosenCenterAllowed := false
	points := heuristicPoints(item, target)
	for _, point := range points {
		for _, orientation := range item.Orientations {
			(*candidates)++
			if *candidates > limit {
				return false, nil
			}
			dims, _ := orientation.Apply(item.Dimensions)
			box, err := geometry.NewCuboid(point, dims)
			if err != nil {
				continue
			}
			outer, _ := geometry.NewCuboid(geometry.Point{}, target.info.Dimensions)
			if !outer.Contains(box) {
				continue
			}
			blocked := slices.ContainsFunc(target.info.Reserved, box.Intersects)
			supporters := make([]string, 0)
			if blocked {
				continue
			}
			for _, existing := range target.placements {
				existingBox, _ := geometry.NewCuboid(existing.Origin, existing.Dimensions)
				if box.Intersects(existingBox) {
					blocked = true
					break
				}
				if _, ok := existingBox.SupportArea(box); ok {
					supporters = append(supporters, existing.ItemID)
				}
			}
			if blocked {
				continue
			}
			if !physicalPlacementAllowed(item, target, box, supporters) {
				continue
			}
			placement := knapsack.Placement{ItemID: item.ID, ContainerID: target.instance.ID, Origin: point, Orientation: orientation, Dimensions: dims, Weight: item.Weight, SupporterIDs: supporters}
			accepted := true
			if len(callbacks) > 0 {
				view, err := constraint.NewPlacementView(item, target.info, placement, target.placements)
				if err != nil {
					return false, err
				}
				for _, callback := range callbacks {
					decision, err := constraint.Evaluate(ctx, callback, view)
					if err != nil {
						return false, err
					}
					if !decision.Accepted {
						accepted = false
						break
					}
				}
			}
			if !accepted {
				continue
			}
			centerAllowed := centerOfGravityAllowedWith(item, target, placement)
			if chosen == nil || centerAllowed && !chosenCenterAllowed || centerAllowed == chosenCenterAllowed && compare(placement, *chosen) < 0 {
				candidate := placement
				chosen = &candidate
				chosenCenterAllowed = centerAllowed
			}
		}
	}
	if chosen == nil {
		return false, nil
	}
	target.placements = append(target.placements, *chosen)
	target.items = append(target.items, item)
	target.weight += item.Weight
	point, dims := chosen.Origin, chosen.Dimensions
	target.points = append(target.points, geometry.Point{X: point.X + dims.X, Y: point.Y, Z: point.Z}, geometry.Point{X: point.X, Y: point.Y + dims.Y, Z: point.Z}, geometry.Point{X: point.X, Y: point.Y, Z: point.Z + dims.Z})
	slices.SortFunc(target.points, comparePoints)
	target.points = slices.Compact(target.points)
	return true, nil
}

func heuristicPoints(item knapsack.NormalizedItem, target *bin) []geometry.Point {
	points := slices.Clone(target.points)
	if target.info.CenterOfGravity == nil {
		return points
	}
	bounds := target.info.CenterOfGravity
	for _, orientation := range item.Orientations {
		dimensions, _ := orientation.Apply(item.Dimensions)
		xs := gravityOrigins(target.info.Dimensions.X, dimensions.X, bounds.MinXPPM, bounds.MaxXPPM)
		ys := gravityOrigins(target.info.Dimensions.Y, dimensions.Y, bounds.MinYPPM, bounds.MaxYPPM)
		zs := gravityOrigins(target.info.Dimensions.Z, dimensions.Z, bounds.MinZPPM, bounds.MaxZPPM)
		for _, z := range zs {
			for _, y := range ys {
				for _, x := range xs {
					points = append(points, geometry.Point{X: x, Y: y, Z: z})
				}
			}
		}
	}
	slices.SortFunc(points, comparePoints)
	return slices.Compact(points)
}

func gravityOrigins(containerLength, itemLength int64, minimumPPM, maximumPPM uint32) []int64 {
	maximumOrigin := containerLength - itemLength
	if maximumOrigin < 0 {
		return nil
	}
	denominator := big.NewInt(2_000_000)
	numerator := func(ppm uint32) *big.Int {
		doubledLength := new(big.Int).Mul(big.NewInt(containerLength), big.NewInt(2))
		result := new(big.Int).Mul(doubledLength, new(big.Int).SetUint64(uint64(ppm)))
		return result.Sub(result, new(big.Int).Mul(big.NewInt(itemLength), big.NewInt(1_000_000)))
	}
	lower := ceilQuotient(numerator(minimumPPM), denominator)
	upper := new(big.Int).Div(numerator(maximumPPM), denominator)
	if lower.Sign() < 0 {
		lower.SetInt64(0)
	}
	if upper.Cmp(big.NewInt(maximumOrigin)) > 0 {
		upper.SetInt64(maximumOrigin)
	}
	if lower.Cmp(upper) > 0 || !lower.IsInt64() || !upper.IsInt64() {
		return nil
	}
	low, high := lower.Int64(), upper.Int64()
	return slices.Compact([]int64{low, low + (high-low)/2, high})
}

func ceilQuotient(numerator, denominator *big.Int) *big.Int {
	negated := new(big.Int).Neg(numerator)
	return new(big.Int).Neg(new(big.Int).Div(negated, denominator))
}

func allCentersOfGravityAllowed(bins []*bin) bool {
	for _, target := range bins {
		if !centerOfGravityAllowed(target) {
			return false
		}
	}
	return true
}

func discardUnbalancedBins(bins []*bin, keepEmpty bool) ([]*bin, []string) {
	result := make([]*bin, 0, len(bins))
	var unpacked []string
	for _, target := range bins {
		if centerOfGravityAllowed(target) {
			result = append(result, target)
			continue
		}
		unpacked = append(unpacked, ids(target.items)...)
		if keepEmpty {
			target.placements = nil
			target.items = nil
			target.points = []geometry.Point{{}}
			target.weight = 0
			result = append(result, target)
		}
	}
	return result, unpacked
}

func centerOfGravityAllowed(target *bin) bool {
	return centerOfGravityAllowedFor(target.info, target.items, target.placements)
}

func centerOfGravityAllowedWith(item knapsack.NormalizedItem, target *bin, placement knapsack.Placement) bool {
	if target.info.CenterOfGravity == nil {
		return true
	}
	items := append(slices.Clone(target.items), item)
	placements := append(slices.Clone(target.placements), placement)
	return centerOfGravityAllowedFor(target.info, items, placements)
}

func centerOfGravityAllowedFor(container knapsack.NormalizedContainer, items []knapsack.NormalizedItem, placements []knapsack.Placement) bool {
	bounds := container.CenterOfGravity
	if bounds == nil || len(placements) == 0 {
		return true
	}
	totalWeight := new(big.Int)
	moments := [3]*big.Int{new(big.Int), new(big.Int), new(big.Int)}
	for index, placement := range placements {
		weight := big.NewInt(items[index].Weight)
		totalWeight.Add(totalWeight, weight)
		centers := [3]*big.Int{
			doubledCenter(placement.Origin.X, placement.Dimensions.X),
			doubledCenter(placement.Origin.Y, placement.Dimensions.Y),
			doubledCenter(placement.Origin.Z, placement.Dimensions.Z),
		}
		for axis, center := range centers {
			moments[axis].Add(moments[axis], new(big.Int).Mul(weight, center))
		}
	}
	dimensions := [3]int64{container.Dimensions.X, container.Dimensions.Y, container.Dimensions.Z}
	minimums := [3]uint32{bounds.MinXPPM, bounds.MinYPPM, bounds.MinZPPM}
	maximums := [3]uint32{bounds.MaxXPPM, bounds.MaxYPPM, bounds.MaxZPPM}
	for axis, moment := range moments {
		scaled := new(big.Int).Mul(moment, big.NewInt(1_000_000))
		doubledDimension := new(big.Int).Mul(big.NewInt(dimensions[axis]), big.NewInt(2))
		axisScale := new(big.Int).Mul(totalWeight, doubledDimension)
		minimum := new(big.Int).Mul(axisScale, new(big.Int).SetUint64(uint64(minimums[axis])))
		maximum := new(big.Int).Mul(axisScale, new(big.Int).SetUint64(uint64(maximums[axis])))
		if scaled.Cmp(minimum) < 0 || scaled.Cmp(maximum) > 0 {
			return false
		}
	}
	return true
}

func doubledCenter(origin, dimension int64) *big.Int {
	return new(big.Int).Add(new(big.Int).Mul(big.NewInt(origin), big.NewInt(2)), big.NewInt(dimension))
}

func comparePlacement(left, right knapsack.Placement) int {
	leftMaximum, rightMaximum := left.Origin.Z+left.Dimensions.Z, right.Origin.Z+right.Dimensions.Z
	if comparison := compareInt64(leftMaximum, rightMaximum); comparison != 0 {
		return comparison
	}
	leftMaximum, rightMaximum = left.Origin.Y+left.Dimensions.Y, right.Origin.Y+right.Dimensions.Y
	if comparison := compareInt64(leftMaximum, rightMaximum); comparison != 0 {
		return comparison
	}
	leftMaximum, rightMaximum = left.Origin.X+left.Dimensions.X, right.Origin.X+right.Dimensions.X
	if comparison := compareInt64(leftMaximum, rightMaximum); comparison != 0 {
		return comparison
	}
	if comparison := comparePoints(left.Origin, right.Origin); comparison != 0 {
		return comparison
	}
	return strings.Compare(string(left.Orientation), string(right.Orientation))
}

func comparePlacementWidthFirst(left, right knapsack.Placement) int {
	leftMaximum, rightMaximum := left.Origin.Z+left.Dimensions.Z, right.Origin.Z+right.Dimensions.Z
	if comparison := compareInt64(leftMaximum, rightMaximum); comparison != 0 {
		return comparison
	}
	leftMaximum, rightMaximum = left.Origin.X+left.Dimensions.X, right.Origin.X+right.Dimensions.X
	if comparison := compareInt64(leftMaximum, rightMaximum); comparison != 0 {
		return comparison
	}
	leftMaximum, rightMaximum = left.Origin.Y+left.Dimensions.Y, right.Origin.Y+right.Dimensions.Y
	if comparison := compareInt64(leftMaximum, rightMaximum); comparison != 0 {
		return comparison
	}
	if comparison := comparePoints(left.Origin, right.Origin); comparison != 0 {
		return comparison
	}
	return strings.Compare(string(left.Orientation), string(right.Orientation))
}

func physicalPlacementAllowed(item knapsack.NormalizedItem, target *bin, box geometry.Cuboid, supporters []string) bool {
	if len(target.info.AllowedClasses) > 0 && !slices.Contains(target.info.AllowedClasses, item.Attributes["class"]) {
		return false
	}
	var supportArea int64
	for index, existing := range target.placements {
		existingItem := target.items[index]
		if slices.Contains(item.IncompatibleGroups, existingItem.Group) || slices.Contains(existingItem.IncompatibleGroups, item.Group) {
			return false
		}
		if slices.Contains(supporters, existing.ItemID) {
			if existingItem.FragileTop || existingItem.MaxSupportedWeight != nil && item.Weight > *existingItem.MaxSupportedWeight {
				return false
			}
			existingBox, _ := geometry.NewCuboid(existing.Origin, existing.Dimensions)
			area, _ := existingBox.SupportArea(box)
			supportArea += area
		}
	}
	if box.Origin().Z > 0 && item.MinimumSupportPPM > 0 {
		if !supportRatioSatisfied(supportArea, box.Dimensions(), item.MinimumSupportPPM) {
			return false
		}
	}
	return stackAllowed(item, target, box)
}

func supportRatioSatisfied(supportArea int64, dimensions geometry.Dimensions, required uint32) bool {
	left := new(big.Int).Mul(big.NewInt(supportArea), big.NewInt(1_000_000))
	right := new(big.Int).Mul(big.NewInt(dimensions.X), big.NewInt(dimensions.Y))
	right.Mul(right, new(big.Int).SetUint64(uint64(required)))

	return left.Cmp(right) >= 0
}

type stackNode struct {
	item knapsack.NormalizedItem
	box  geometry.Cuboid
}

func stackAllowed(item knapsack.NormalizedItem, target *bin, box geometry.Cuboid) bool {
	nodes := make([]stackNode, 0, len(target.items)+1)
	for index, existingItem := range target.items {
		existingBox, _ := geometry.NewCuboid(target.placements[index].Origin, target.placements[index].Dimensions)
		nodes = append(nodes, stackNode{existingItem, existingBox})
	}
	nodes = append(nodes, stackNode{item, box})
	edges := make(map[string][]supportEdge)
	above := make(map[string][]string)
	for _, current := range nodes {
		for _, supporter := range nodes {
			if area, ok := supporter.box.SupportArea(current.box); ok {
				edges[current.item.ID] = append(edges[current.item.ID], supportEdge{supporter.item.ID, area})
				above[supporter.item.ID] = append(above[supporter.item.ID], current.item.ID)
			}
		}
	}
	loads := make(map[string]*big.Rat)
	slices.SortFunc(nodes, func(a, b stackNode) int { return -compareInt64(a.box.Origin().Z, b.box.Origin().Z) })
	for _, current := range nodes {
		total := new(big.Rat).Add(loadFor(loads, current.item.ID), new(big.Rat).SetInt64(current.item.Weight))
		var areaTotal int64
		for _, edge := range edges[current.item.ID] {
			areaTotal += edge.area
		}
		if areaTotal > 0 {
			for _, edge := range edges[current.item.ID] {
				share := new(big.Rat).Mul(total, new(big.Rat).SetFrac64(edge.area, areaTotal))
				loads[edge.itemID] = new(big.Rat).Add(loadFor(loads, edge.itemID), share)
			}
		}
	}
	for _, current := range nodes {
		if current.item.MaxSupportedWeight != nil && loadFor(loads, current.item.ID).Cmp(new(big.Rat).SetInt64(*current.item.MaxSupportedWeight)) > 0 {
			return false
		}
		if current.item.MaxStackCount > 0 && solverStackDepth(current.item.ID, above) > current.item.MaxStackCount {
			return false
		}
	}
	return true
}

type supportEdge struct {
	itemID string
	area   int64
}

func loadFor(loads map[string]*big.Rat, itemID string) *big.Rat {
	if load := loads[itemID]; load != nil {
		return new(big.Rat).Set(load)
	}
	return new(big.Rat)
}
func solverStackDepth(itemID string, above map[string][]string) uint32 {
	var maximum uint32
	for _, supported := range above[itemID] {
		if depth := solverStackDepth(supported, above) + 1; depth > maximum {
			maximum = depth
		}
	}
	return maximum
}
func compareInt64(left, right int64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func buildPlan(bins []*bin, unpacked []string, status knapsack.Status, termination knapsack.TerminationReason, candidates, seed uint64, diagnostics []knapsack.Diagnostic) knapsack.Plan {
	spec := knapsack.PlanSpec{Status: status, Termination: termination, UnpackedItemIDs: slices.Clone(unpacked), Diagnostics: diagnostics, Work: knapsack.Work{Solver: "heuristic", Strategy: "deterministic_extreme_point", Seed: seed, CandidatePlacements: candidates}}
	for _, target := range bins {
		spec.Containers = append(spec.Containers, target.instance)
		spec.Placements = append(spec.Placements, placementsWithFinalSupport(target.placements)...)
		volume, _ := target.info.Dimensions.Volume()
		spec.Statistics.ContainerVolume += volume
		spec.Statistics.RemainingWeight += target.info.MaxContentWeight - target.weight
	}
	for _, placement := range spec.Placements {
		volume, _ := placement.Dimensions.Volume()
		spec.Statistics.ItemVolume += volume
		spec.Statistics.ItemWeight += placement.Weight
	}
	// #nosec G115 -- normalized request limits cap both counts at uint32.
	spec.Statistics.PackedItems = uint32(len(spec.Placements))
	// #nosec G115 -- normalized request limits cap both counts at uint32.
	spec.Statistics.ContainerCount = uint32(len(spec.Containers))
	spec.Statistics.RemainingVolume = spec.Statistics.ContainerVolume - spec.Statistics.ItemVolume
	spec.Objective = []knapsack.ScoreComponent{{Name: "container_count", Direction: "min", Unit: "count", Value: strconv.Itoa(len(spec.Containers))}, {Name: "unused_volume", Direction: "min", Unit: "lattice^3", Value: strconv.FormatInt(spec.Statistics.RemainingVolume, 10)}}
	plan, _ := knapsack.NewPlan(spec)
	return plan
}

func placementsWithFinalSupport(placements []knapsack.Placement) []knapsack.Placement {
	result := make([]knapsack.Placement, len(placements))
	for index, placement := range placements {
		placement.SupporterIDs = nil
		if placement.Origin.Z > 0 {
			box, _ := geometry.NewCuboid(placement.Origin, placement.Dimensions)
			for _, candidate := range placements {
				candidateBox, _ := geometry.NewCuboid(candidate.Origin, candidate.Dimensions)
				if _, ok := candidateBox.SupportArea(box); ok {
					placement.SupporterIDs = append(placement.SupporterIDs, candidate.ItemID)
				}
			}
			slices.Sort(placement.SupporterIDs)
		}
		result[index] = placement
	}
	return result
}

func interruptedPlan(request knapsack.NormalizedRequest, bins []*bin, unpacked []string, candidates, seed uint64, cause error) (knapsack.Plan, error) {
	termination := knapsack.TerminationCancelled
	if errors.Is(cause, context.DeadlineExceeded) {
		termination = knapsack.TerminationDeadline
	}
	if errors.Is(cause, knapsack.ErrBudgetExhausted) {
		termination = knapsack.TerminationCandidateLimit
	}
	var unbalanced []string
	bins, unbalanced = discardUnbalancedBins(bins, true)
	unpacked = append(unpacked, unbalanced...)
	plan := buildPlan(bins, unique(unpacked), knapsack.StatusBudgetExhausted, termination, candidates, seed, nil)
	if len(plan.Placements()) > 0 {
		if result := verify.Plan(request, plan, verify.AllowUnpacked()); !result.Valid() {
			return knapsack.Plan{}, fmt.Errorf("%w: %v", knapsack.ErrInternalInvariant, result.Violations())
		}
	}
	return plan, cause
}

func interruptedHeuristicPlan(request knapsack.NormalizedRequest, bins []*bin, unpacked []string, candidates uint64, options Options, goal objective.PlanObjective, cause error) (knapsack.Plan, error) {
	if options.PlanObjective != nil || len(options.Constraints) > 0 {
		return knapsack.Plan{}, cause
	}
	plan, err := interruptedPlan(request, bins, unpacked, candidates, options.Seed, cause)
	if plan.Status() == "" {
		return plan, err
	}
	plan, objectiveErr := withObjective(context.Background(), request, plan, goal)
	if objectiveErr != nil {
		return knapsack.Plan{}, objectiveErr
	}
	if verificationErr := verifySolverPlan(request, plan, verify.AllowUnpacked().WithObjective(goal)); verificationErr != nil {
		return knapsack.Plan{}, verificationErr
	}
	return plan, err
}

func interruptedBestPlan(best knapsack.Plan, candidates uint64, rounds uint32, cause error) (knapsack.Plan, error) {
	spec := best.Spec()
	spec.Status = knapsack.StatusBudgetExhausted
	switch {
	case errors.Is(cause, context.Canceled):
		spec.Termination = knapsack.TerminationCancelled
	case errors.Is(cause, context.DeadlineExceeded):
		spec.Termination = knapsack.TerminationDeadline
	default:
		spec.Termination = knapsack.TerminationCandidateLimit
	}
	spec.Work.Strategy = "deterministic_extreme_point_repack"
	spec.Work.CandidatePlacements = candidates
	spec.Work.ImprovementRounds = rounds
	plan, _ := knapsack.NewPlan(spec)
	return plan, cause
}

func verifySolverPlan(request knapsack.NormalizedRequest, plan knapsack.Plan, options verify.Options) error {
	result := verify.Plan(request, plan, options)
	if err := result.Err(); err != nil {
		return err
	}
	if !result.Valid() {
		return fmt.Errorf("%w: %v", knapsack.ErrInternalInvariant, result.Violations())
	}
	return nil
}

func workingMemoryAvailable(request knapsack.NormalizedRequest, maximumBins int, exact bool) (uint64, bool) {
	total := new(big.Int).SetUint64(request.MemoryBytes())
	total.Mul(total, big.NewInt(2)) // retained request plus accessor copies
	items := big.NewInt(int64(request.ItemCount()))
	bins := big.NewInt(int64(maximumBins))
	total.Add(total, new(big.Int).Mul(items, big.NewInt(1024)))
	total.Add(total, new(big.Int).Mul(bins, big.NewInt(1024)))
	if exact {
		total.Add(total, new(big.Int).Mul(new(big.Int).Mul(items, items), big.NewInt(256)))
		total.Add(total, new(big.Int).Mul(new(big.Int).Mul(bins, items), big.NewInt(512)))
	}
	limit := new(big.Int).SetUint64(request.Limits().MaxMemoryBytes)
	if total.Cmp(limit) > 0 {
		return 0, false
	}
	return new(big.Int).Sub(limit, total).Uint64(), true
}

func compareItems(a, b knapsack.NormalizedItem) int {
	av, _ := a.Dimensions.Volume()
	bv, _ := b.Dimensions.Volume()
	if av != bv {
		if av > bv {
			return -1
		}
		return 1
	}
	if a.Weight != b.Weight {
		if a.Weight > b.Weight {
			return -1
		}
		return 1
	}
	return strings.Compare(a.ID, b.ID)
}
func compareContainers(a, b knapsack.NormalizedContainer) int {
	if a.Priority != b.Priority {
		if a.Priority < b.Priority {
			return -1
		}
		return 1
	}
	av, _ := a.Dimensions.Volume()
	bv, _ := b.Dimensions.Volume()
	if av != bv {
		if av < bv {
			return -1
		}
		return 1
	}
	return strings.Compare(a.ID, b.ID)
}
func comparePoints(a, b geometry.Point) int {
	if a.Z != b.Z {
		if a.Z < b.Z {
			return -1
		}
		return 1
	}
	if a.Y != b.Y {
		if a.Y < b.Y {
			return -1
		}
		return 1
	}
	if a.X < b.X {
		return -1
	}
	if a.X > b.X {
		return 1
	}
	return 0
}
func ids(items []knapsack.NormalizedItem) []string {
	result := make([]string, len(items))
	for index := range items {
		result[index] = items[index].ID
	}
	return result
}
func unique(values []string) []string { slices.Sort(values); return slices.Compact(values) }

func groupTargets(bins []*bin, group string) ([]*bin, bool) {
	if group == "" {
		return bins, false
	}
	result := make([]*bin, 0)
	for _, target := range bins {
		if slices.ContainsFunc(target.items, func(item knapsack.NormalizedItem) bool { return item.Group == group }) {
			result = append(result, target)
		}
	}
	if len(result) == 0 {
		return bins, false
	}
	return result, true
}

func rollbackGroup(bins []*bin, group string, discardEmpty bool, usedStock map[string]uint32) ([]*bin, []string) {
	result := make([]*bin, 0, len(bins))
	removed := make([]string, 0)
	for _, target := range bins {
		placements := make([]knapsack.Placement, 0, len(target.placements))
		items := make([]knapsack.NormalizedItem, 0, len(target.items))
		var weight int64
		for index, item := range target.items {
			if item.Group == group {
				removed = append(removed, item.ID)
				continue
			}
			items = append(items, item)
			placements = append(placements, target.placements[index])
			weight += item.Weight
		}
		target.items, target.placements, target.weight = items, placements, weight
		target.points = []geometry.Point{{}}
		for _, placement := range placements {
			target.points = append(target.points, geometry.Point{X: placement.Origin.X + placement.Dimensions.X, Y: placement.Origin.Y, Z: placement.Origin.Z}, geometry.Point{X: placement.Origin.X, Y: placement.Origin.Y + placement.Dimensions.Y, Z: placement.Origin.Z}, geometry.Point{X: placement.Origin.X, Y: placement.Origin.Y, Z: placement.Origin.Z + placement.Dimensions.Z})
		}
		slices.SortFunc(target.points, comparePoints)
		target.points = slices.Compact(target.points)
		if discardEmpty && len(placements) == 0 {
			if usedStock != nil {
				usedStock[target.info.ID]--
			}
			continue
		}
		result = append(result, target)
	}
	return result, removed
}
