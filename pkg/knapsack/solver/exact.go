package solver

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/constraint"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/objective"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
)

// Exact exhaustively enumerates compact orthogonal placements for bounded
// small fixed-container instances. It is intended as an oracle, not a
// large-order solver.
type Exact struct{}

type exactSearch struct {
	ctx          context.Context
	request      knapsack.NormalizedRequest
	limits       knapsack.Limits
	bins         []*bin
	nodes        uint64
	branches     uint64
	candidates   uint64
	best         *knapsack.Plan
	budgeted     bool
	termination  knapsack.TerminationReason
	budgetCause  error
	cancelled    error
	seed         uint64
	callbacks    []constraint.Placement
	callbackErr  error
	goal         objective.PlanObjective
	objectiveErr error
	invariantErr error
	pointMemory  uint64
	fullPoints   map[string][]geometry.Point
}

// PackAll exhaustively considers bounded container multisets in increasing
// container-count order, then exhaustively searches every multiset at the
// first feasible count. Its built-in proof objective is container count with a
// canonical plan tie-break.
func (Exact) PackAll(ctx context.Context, request knapsack.NormalizedRequest, options Options) (knapsack.Plan, error) {
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
	if _, ok := workingMemoryAvailable(request, request.ItemCount(), true); !ok {
		return knapsack.Plan{}, knapsack.ErrMemoryBudgetExhausted
	}
	items, types := request.Items(), request.Containers()
	if len(items) == 0 || len(types) == 0 {
		return knapsack.Plan{}, knapsack.ErrInvalidRequest
	}
	goal, err := resolvedObjective(options)
	if err != nil {
		return knapsack.Plan{}, err
	}
	slices.SortFunc(types, compareContainers)
	var configurations uint64
	var totalWork knapsack.Work
	var globalBest *knapsack.Plan
	var interruptedBy knapsack.TerminationReason
	for count := exactContainerLowerBound(items, types); count <= len(items); count++ {
		var best *knapsack.Plan
		err := enumerateTypeMultisets(ctx, len(types), count, func(selected []int) error {
			configurations++
			if interruptedBy = variableBudgetTermination(configurations, totalWork, limits); interruptedBy != "" {
				return knapsack.ErrBudgetExhausted
			}
			instances := make([]knapsack.ContainerInstance, len(selected))
			used := make(map[string]uint32)
			for index, typeIndex := range selected {
				container := types[typeIndex]
				used[container.ID]++
				if !container.Stock.Unlimited() && used[container.ID] > container.Stock.Count() {
					return nil
				}
				instances[index] = knapsack.ContainerInstance{ID: fmt.Sprintf("%s#%06d", container.ID, used[container.ID]), TypeID: container.ID}
			}
			scopedLimits := limits
			scopedLimits.MaxSearchNodes -= totalWork.Nodes
			scopedLimits.MaxBranches -= configurations + totalWork.Branches
			scopedLimits.MaxCandidatePlacements -= totalWork.CandidatePlacements
			plan, err := (Exact{}).PackFixed(ctx, request.WithLimits(scopedLimits), instances, options)
			candidateWork := plan.Work()
			totalWork.Nodes += candidateWork.Nodes
			totalWork.Branches += candidateWork.Branches
			totalWork.CandidatePlacements += candidateWork.CandidatePlacements
			if err != nil {
				if len(plan.UnpackedItemIDs()) == 0 && len(plan.Placements()) == len(items) {
					copy := plan
					best = &copy
				}
				if errors.Is(err, knapsack.ErrProvenInfeasible) {
					return nil
				}
				if !isSearchInterruption(err) {
					return err
				}
				interruptedBy = plan.Termination()
				if interruptedBy == "" {
					interruptedBy = terminationForError(err)
				}
				return err
			}
			preferred, err := objectivePrefers(ctx, goal, request, plan, best)
			if err != nil {
				return err
			}
			if preferred {
				copy := plan
				best = &copy
			}
			return nil
		})
		if err != nil {
			if !isSearchInterruption(err) {
				return knapsack.Plan{}, err
			}
			if interruptedBy == "" {
				interruptedBy = terminationForError(err)
			}
			partial := best
			if partial == nil {
				partial = globalBest
			}
			if partial != nil {
				spec := partial.Spec()
				spec.Status = knapsack.StatusBudgetExhausted
				spec.Termination = interruptedBy
				spec.Work = totalWork
				spec.Work.Solver = "exact"
				spec.Work.Strategy = "exhaustive_variable_container"
				spec.Work.Seed = options.Seed
				spec.Work.Branches += configurations
				plan, _ := knapsack.NewPlan(spec)
				return plan, err
			}
			return emptyExactPlan(items, knapsack.StatusBudgetExhausted, interruptedBy), err
		}
		if best != nil {
			spec := best.Spec()
			spec.Status = knapsack.StatusOptimal
			spec.Work = totalWork
			spec.Work.Solver = "exact"
			spec.Work.Strategy = "exhaustive_variable_container"
			spec.Work.Seed = options.Seed
			spec.Work.Branches += configurations
			plan, _ := knapsack.NewPlan(spec)
			plan, err = withObjective(ctx, request, plan, goal)
			if err != nil {
				return knapsack.Plan{}, err
			}
			preferred, err := objectivePrefers(ctx, goal, request, plan, globalBest)
			if err != nil {
				return knapsack.Plan{}, err
			}
			if preferred {
				copy := plan
				globalBest = &copy
			}
		}
	}
	if globalBest != nil {
		return *globalBest, nil
	}
	return emptyExactPlan(items, knapsack.StatusInfeasible, knapsack.TerminationCompleted), knapsack.ErrProvenInfeasible
}

func isSearchInterruption(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, knapsack.ErrBudgetExhausted)
}

func variableBudgetTermination(configurations uint64, work knapsack.Work, limits knapsack.Limits) knapsack.TerminationReason {
	switch {
	case configurations > limits.MaxBranches || work.Branches >= limits.MaxBranches-configurations:
		return knapsack.TerminationBranchLimit
	case work.Nodes >= limits.MaxSearchNodes:
		return knapsack.TerminationNodeLimit
	case work.CandidatePlacements >= limits.MaxCandidatePlacements:
		return knapsack.TerminationCandidateLimit
	default:
		return ""
	}
}

func terminationForError(err error) knapsack.TerminationReason {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return knapsack.TerminationDeadline
	case errors.Is(err, context.Canceled):
		return knapsack.TerminationCancelled
	case errors.Is(err, knapsack.ErrMemoryBudgetExhausted):
		return knapsack.TerminationMemoryLimit
	default:
		return knapsack.TerminationNodeLimit
	}
}

func enumerateTypeMultisets(ctx context.Context, typeCount, count int, visit func([]int) error) error {
	var walk func(int, []int) error
	walk = func(start int, selected []int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(selected) == count {
			return visit(slices.Clone(selected))
		}
		for index := start; index < typeCount; index++ {
			if err := walk(index, append(selected, index)); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(0, nil)
}

func exactContainerLowerBound(items []knapsack.NormalizedItem, containers []knapsack.NormalizedContainer) int {
	totalVolume, totalWeight := new(big.Int), new(big.Int)
	var maximumVolume, maximumWeight int64
	for _, item := range items {
		volume, _ := item.Dimensions.Volume()
		totalVolume.Add(totalVolume, big.NewInt(volume))
		totalWeight.Add(totalWeight, big.NewInt(item.Weight))
	}
	for _, container := range containers {
		volume, _ := container.Dimensions.Volume()
		if volume > maximumVolume {
			maximumVolume = volume
		}
		weight := container.MaxContentWeight
		if container.HasGrossWeight && container.MaxGrossWeight-container.TareWeight < weight {
			weight = container.MaxGrossWeight - container.TareWeight
		}
		if weight > maximumWeight {
			maximumWeight = weight
		}
	}
	volumeBound := ceilingQuotient(totalVolume, maximumVolume)
	weightBound := ceilingQuotient(totalWeight, maximumWeight)
	if volumeBound > weightBound {
		return volumeBound
	}
	return weightBound
}

func ceilingQuotient(total *big.Int, capacity int64) int {
	if capacity <= 0 {
		return int(^uint(0) >> 1)
	}
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(total, big.NewInt(capacity), remainder)
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() || quotient.Int64() > int64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(quotient.Int64())
}

func emptyExactPlan(items []knapsack.NormalizedItem, status knapsack.Status, termination knapsack.TerminationReason) knapsack.Plan {
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{UnpackedItemIDs: ids(items), Status: status, Termination: termination, Work: knapsack.Work{Solver: "exact", Strategy: "exhaustive_variable_container"}})
	return plan
}

// PackFixed exhaustively enumerates compact placements in exactly the supplied
// finite instances. It reports optimal only after completing bounded search.
func (Exact) PackFixed(ctx context.Context, request knapsack.NormalizedRequest, instances []knapsack.ContainerInstance, options Options) (knapsack.Plan, error) {
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
	pointMemory, ok := workingMemoryAvailable(request, len(instances), true)
	if !ok {
		return knapsack.Plan{}, knapsack.ErrMemoryBudgetExhausted
	}
	items, types := request.Items(), request.Containers()
	if len(items) == 0 || len(types) == 0 || len(instances) == 0 {
		return knapsack.Plan{}, knapsack.ErrInvalidRequest
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
	goal, err := resolvedObjective(options)
	if err != nil {
		return knapsack.Plan{}, err
	}
	fullPoints, remainingPointMemory, ok := exactCenterGrids(bins, pointMemory)
	if !ok {
		return knapsack.Plan{}, knapsack.ErrMemoryBudgetExhausted
	}
	search := &exactSearch{ctx: ctx, request: request, limits: limits, bins: bins, seed: options.Seed, callbacks: slices.Clone(options.Constraints), goal: goal, pointMemory: remainingPointMemory, fullPoints: fullPoints}
	search.visit(items)
	if search.objectiveErr != nil {
		return knapsack.Plan{}, search.objectiveErr
	}
	if search.invariantErr != nil {
		return knapsack.Plan{}, search.invariantErr
	}
	if search.callbackErr != nil {
		return knapsack.Plan{}, search.callbackErr
	}
	if search.cancelled != nil {
		termination := knapsack.TerminationCancelled
		if errors.Is(search.cancelled, context.DeadlineExceeded) {
			termination = knapsack.TerminationDeadline
		}
		return search.partial(items, termination, search.cancelled)
	}
	if search.budgeted {
		return search.partial(items, search.termination, search.budgetCause)
	}
	if search.best == nil {
		plan := search.empty(items, knapsack.StatusInfeasible, knapsack.TerminationCompleted)
		return plan, knapsack.ErrProvenInfeasible
	}
	best := search.best.Spec()
	best.Status = knapsack.StatusOptimal
	best.Termination = knapsack.TerminationCompleted
	best.Work = knapsack.Work{Solver: "exact", Strategy: "exhaustive_compact_fixed", Seed: options.Seed, Nodes: search.nodes, Branches: search.branches, CandidatePlacements: search.candidates}
	plan, _ := knapsack.NewPlan(best)
	plan, err = withObjective(ctx, request, plan, goal)
	if err != nil {
		return knapsack.Plan{}, err
	}
	return plan, verifySolverPlan(request, plan, verify.RequireAll().WithObjective(goal).WithConstraints(options.Constraints...))
}

func (s *exactSearch) visit(remaining []knapsack.NormalizedItem) {
	if s.stopped() {
		return
	}
	s.nodes++
	if s.nodes > s.limits.MaxSearchNodes {
		s.exhaust(knapsack.TerminationNodeLimit, knapsack.ErrBudgetExhausted)
		return
	}
	if len(remaining) == 0 {
		if !allCentersOfGravityAllowed(s.bins) {
			return
		}
		candidate := buildPlan(s.bins, nil, knapsack.StatusFeasible, knapsack.TerminationCompleted, s.candidates, s.seed, nil)
		candidate, err := withObjective(s.ctx, s.request, candidate, s.goal)
		if err != nil {
			s.objectiveErr = err
			return
		}
		verificationErr := verifySolverPlan(s.request, candidate, verify.RequireAll().WithObjective(s.goal).WithConstraints(s.callbacks...))
		if verificationErr != nil {
			if errors.Is(verificationErr, objective.ErrInvalidObjective) || errors.Is(verificationErr, context.Canceled) || errors.Is(verificationErr, context.DeadlineExceeded) {
				s.objectiveErr = verificationErr
			} else {
				s.invariantErr = verificationErr
			}
			return
		}
		preferred, err := objectivePrefers(s.ctx, s.goal, s.request, candidate, s.best)
		if err != nil {
			s.objectiveErr = err
			return
		}
		if preferred {
			copy := candidate
			s.best = &copy
		}
		return
	}
	for itemIndex, item := range remaining {
		next := slices.Clone(remaining)
		next = append(next[:itemIndex], next[itemIndex+1:]...)
		for _, target := range s.bins {
			points := s.fullPoints[target.info.ID]
			if points == nil {
				var ok bool
				points, ok = exactPoints(target, s.pointMemory)
				if !ok {
					s.exhaust(knapsack.TerminationMemoryLimit, knapsack.ErrMemoryBudgetExhausted)
					return
				}
			}
			for _, point := range points {
				for _, orientation := range item.Orientations {
					s.branches++
					s.candidates++
					if s.branches > s.limits.MaxBranches {
						s.exhaust(knapsack.TerminationBranchLimit, knapsack.ErrBudgetExhausted)
						return
					}
					if s.candidates > s.limits.MaxCandidatePlacements {
						s.exhaust(knapsack.TerminationCandidateLimit, knapsack.ErrBudgetExhausted)
						return
					}
					placement, ok := exactPlacement(item, target, point, orientation)
					if !ok {
						continue
					}
					accepted := true
					if len(s.callbacks) > 0 {
						view, err := constraint.NewPlacementView(item, target.info, placement, target.placements)
						if err != nil {
							s.callbackErr = err
							return
						}
						for _, callback := range s.callbacks {
							decision, err := constraint.Evaluate(s.ctx, callback, view)
							if err != nil {
								s.callbackErr = err
								return
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
					target.placements = append(target.placements, placement)
					target.items = append(target.items, item)
					target.weight += item.Weight
					s.visit(next)
					target.weight -= item.Weight
					target.placements = target.placements[:len(target.placements)-1]
					target.items = target.items[:len(target.items)-1]
					if s.stopped() {
						return
					}
				}
			}
		}
	}
}

func exactCenterGrids(bins []*bin, memoryLimit uint64) (map[string][]geometry.Point, uint64, bool) {
	grids := make(map[string][]geometry.Point)
	remaining := memoryLimit
	for _, target := range bins {
		if target.info.CenterOfGravity == nil || grids[target.info.ID] != nil {
			continue
		}
		points, ok := exactPoints(target, remaining)
		if !ok {
			return nil, 0, false
		}
		// exactPoints proves this retained slice fits the remaining byte budget.
		bytes := uint64(len(points)) * 24
		grids[target.info.ID] = points
		remaining -= bytes
	}
	return grids, remaining, true
}

func (s *exactSearch) exhaust(termination knapsack.TerminationReason, cause error) {
	s.budgeted = true
	s.termination = termination
	s.budgetCause = cause
}

func (s *exactSearch) stopped() bool {
	if s.budgeted || s.cancelled != nil || s.callbackErr != nil || s.objectiveErr != nil || s.invariantErr != nil {
		return true
	}
	if err := s.ctx.Err(); err != nil {
		s.cancelled = err
		return true
	}
	return false
}

func exactPoints(target *bin, memoryLimit uint64) ([]geometry.Point, bool) {
	xs, ys, zs := []int64{0}, []int64{0}, []int64{0}
	if target.info.CenterOfGravity != nil {
		xLength := new(big.Int).SetInt64(target.info.Dimensions.X).Uint64()
		yLength := new(big.Int).SetInt64(target.info.Dimensions.Y).Uint64()
		zLength := new(big.Int).SetInt64(target.info.Dimensions.Z).Uint64()
		count, ok := checkedProduct(
			xLength,
			yLength,
			zLength,
		)
		// Validated volume bounds prove the positive-axis sum fits uint64.
		coordinates := xLength + yLength + zLength
		coordinateBytes, coordinatesOK := checkedProduct(coordinates, 8)
		pointBytes, pointsOK := checkedProduct(count, 24)
		if !ok || !coordinatesOK || !pointsOK || count > uint64(^uint(0)>>1) ||
			coordinateBytes > memoryLimit || pointBytes > memoryLimit-coordinateBytes {
			return nil, false
		}
		xs = latticeCoordinates(target.info.Dimensions.X)
		ys = latticeCoordinates(target.info.Dimensions.Y)
		zs = latticeCoordinates(target.info.Dimensions.Z)
	}
	for _, placement := range target.placements {
		xs = append(xs, placement.Origin.X+placement.Dimensions.X)
		ys = append(ys, placement.Origin.Y+placement.Dimensions.Y)
		zs = append(zs, placement.Origin.Z+placement.Dimensions.Z)
	}
	for _, reserved := range target.info.Reserved {
		maximum := reserved.Max()
		xs = append(xs, maximum.X)
		ys = append(ys, maximum.Y)
		zs = append(zs, maximum.Z)
	}
	slices.Sort(xs)
	xs = slices.Compact(xs)
	slices.Sort(ys)
	ys = slices.Compact(ys)
	slices.Sort(zs)
	zs = slices.Compact(zs)
	count, ok := checkedProduct(uint64(len(xs)), uint64(len(ys)), uint64(len(zs)))
	if !ok || count > uint64(^uint(0)>>1) || count > memoryLimit/24 {
		return nil, false
	}
	points := make([]geometry.Point, 0, int(count))
	for _, z := range zs {
		for _, y := range ys {
			for _, x := range xs {
				points = append(points, geometry.Point{X: x, Y: y, Z: z})
			}
		}
	}
	return points, true
}

func latticeCoordinates(length int64) []int64 {
	coordinates := make([]int64, length)
	for coordinate := range length {
		coordinates[coordinate] = coordinate
	}
	return coordinates
}

func checkedProduct(values ...uint64) (uint64, bool) {
	result := uint64(1)
	for _, value := range values {
		if value != 0 && result > ^uint64(0)/value {
			return 0, false
		}
		result *= value
	}
	return result, true
}

func exactPlacement(item knapsack.NormalizedItem, target *bin, point geometry.Point, orientation geometry.Orientation) (knapsack.Placement, bool) {
	if target.info.MaxItemCount > 0 && uint64(len(target.placements)) >= uint64(target.info.MaxItemCount) || target.weight > target.info.MaxContentWeight-item.Weight {
		return knapsack.Placement{}, false
	}
	if target.info.HasGrossWeight && target.weight > target.info.MaxGrossWeight-target.info.TareWeight-item.Weight {
		return knapsack.Placement{}, false
	}
	if len(target.info.AllowedClasses) > 0 && !slices.Contains(target.info.AllowedClasses, item.Attributes["class"]) {
		return knapsack.Placement{}, false
	}
	dims, err := orientation.Apply(item.Dimensions)
	if err != nil {
		return knapsack.Placement{}, false
	}
	box, err := geometry.NewCuboid(point, dims)
	if err != nil {
		return knapsack.Placement{}, false
	}
	outer, _ := geometry.NewCuboid(geometry.Point{}, target.info.Dimensions)
	if !outer.Contains(box) {
		return knapsack.Placement{}, false
	}
	if slices.ContainsFunc(target.info.Reserved, box.Intersects) {
		return knapsack.Placement{}, false
	}
	var support int64
	supporters := make([]string, 0)
	for _, existing := range target.placements {
		existingBox, _ := geometry.NewCuboid(existing.Origin, existing.Dimensions)
		if box.Intersects(existingBox) {
			return knapsack.Placement{}, false
		}
		if area, ok := existingBox.SupportArea(box); ok {
			support += area
			supporters = append(supporters, existing.ItemID)
		}
	}
	if point.Z > 0 && item.MinimumSupportPPM > 0 {
		if !supportRatioSatisfied(support, dims, item.MinimumSupportPPM) {
			return knapsack.Placement{}, false
		}
	}
	if !physicalPlacementAllowed(item, target, box, supporters) {
		return knapsack.Placement{}, false
	}
	return knapsack.Placement{ItemID: item.ID, ContainerID: target.instance.ID, Origin: point, Orientation: orientation, Dimensions: dims, Weight: item.Weight, SupporterIDs: supporters}, true
}

func (s *exactSearch) empty(items []knapsack.NormalizedItem, status knapsack.Status, termination knapsack.TerminationReason) knapsack.Plan {
	plan := buildPlan(s.bins, ids(items), status, termination, s.candidates, s.seed, nil)
	spec := plan.Spec()
	spec.Work = knapsack.Work{Solver: "exact", Strategy: "exhaustive_compact_fixed", Seed: s.seed, Nodes: s.nodes, Branches: s.branches, CandidatePlacements: s.candidates}
	plan, _ = knapsack.NewPlan(spec)
	return plan
}
func (s *exactSearch) partial(items []knapsack.NormalizedItem, termination knapsack.TerminationReason, cause error) (knapsack.Plan, error) {
	if s.best != nil {
		spec := s.best.Spec()
		spec.Status = knapsack.StatusBudgetExhausted
		spec.Termination = termination
		spec.Work = knapsack.Work{Solver: "exact", Strategy: "exhaustive_compact_fixed", Seed: s.seed, Nodes: s.nodes, Branches: s.branches, CandidatePlacements: s.candidates}
		plan, _ := knapsack.NewPlan(spec)
		return plan, cause
	}
	return s.empty(items, knapsack.StatusBudgetExhausted, termination), cause
}

func resolvedObjective(options Options) (objective.PlanObjective, error) {
	if options.PlanObjective != nil {
		if options.Objective.Valid() {
			return nil, knapsack.ErrInvalidOptions
		}
		return options.PlanObjective, nil
	}
	if options.Objective.Valid() {
		return options.Objective, nil
	}
	resolved, _ := objective.New(objective.Minimize(objective.ContainerCount), objective.Minimize(objective.UnusedVolume))
	return resolved, nil
}

func validatedOptions(options Options) (Options, error) {
	if err := constraint.ValidateCallbacks(options.Constraints); err != nil {
		return Options{}, err
	}
	options.Constraints = slices.Clone(options.Constraints)
	return options, nil
}

func objectivePrefers(ctx context.Context, goal objective.PlanObjective, request knapsack.NormalizedRequest, left knapsack.Plan, right *knapsack.Plan) (bool, error) {
	if right == nil {
		if _, err := objective.SafeComponents(ctx, goal, request, left); err != nil {
			return false, err
		}
		return true, nil
	}
	comparison, err := objective.SafeCompare(ctx, goal, request, left, *right)
	return comparison < 0, err
}

func withObjective(ctx context.Context, request knapsack.NormalizedRequest, plan knapsack.Plan, goal objective.PlanObjective) (knapsack.Plan, error) {
	components, err := objective.SafeComponents(ctx, goal, request, plan)
	if err != nil {
		return knapsack.Plan{}, err
	}
	spec := plan.Spec()
	spec.Objective = components
	updated, _ := knapsack.NewPlan(spec)
	return updated, nil
}
