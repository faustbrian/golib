// Package verify independently checks supplied plans against normalized input.
package verify

import (
	"context"
	"math"
	"math/big"
	"slices"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/constraint"
	packingjson "github.com/faustbrian/golib/pkg/knapsack/encoding"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/objective"
)

// Code is a stable machine-readable verifier violation category.
type Code string

const (
	// CodeUnknownItem reports an item ID absent from the request.
	CodeUnknownItem Code = "unknown_item"
	// CodeUnknownContainer reports an unknown type or instance ID.
	CodeUnknownContainer Code = "unknown_container"
	// CodeDuplicateItem reports repeated or packed-and-unpacked item identity.
	CodeDuplicateItem Code = "duplicate_item"
	// CodeMissingItem reports a required item omitted from the plan.
	CodeMissingItem Code = "missing_item"
	// CodeOutsideContainer reports a placement outside usable bounds.
	CodeOutsideContainer Code = "outside_container"
	// CodeOverlap reports positive-volume intersection between items.
	CodeOverlap Code = "overlap"
	// CodeReservedOverlap reports intersection with reserved container space.
	CodeReservedOverlap Code = "reserved_overlap"
	// CodeForbiddenOrientation reports an orientation absent from the item.
	CodeForbiddenOrientation Code = "forbidden_orientation"
	// CodeAlteredItem reports plan dimensions or weight changed from input.
	CodeAlteredItem Code = "altered_item"
	// CodeOverweight reports content or gross-weight capacity exceeded.
	CodeOverweight Code = "overweight"
	// CodeCenterOfGravity reports content mass outside configured axis bounds.
	CodeCenterOfGravity Code = "center_of_gravity"
	// CodeStock reports finite stock or maximum item count exceeded.
	CodeStock Code = "stock"
	// CodeUnsupported reports insufficient geometric support area.
	CodeUnsupported Code = "unsupported"
	// CodeFragile reports a placement bearing on a fragile top.
	CodeFragile Code = "fragile"
	// CodeLoadBearing reports transitive supported load exceeded.
	CodeLoadBearing Code = "load_bearing"
	// CodeStackLimit reports maximum stack depth exceeded.
	CodeStackLimit Code = "stack_limit"
	// CodeGrouping reports required co-location violated.
	CodeGrouping Code = "grouping"
	// CodeIncompatible reports incompatible item groups sharing a container.
	CodeIncompatible Code = "incompatible"
	// CodeAccounting reports serialized statistics differing from recomputation.
	CodeAccounting Code = "accounting"
	// CodeEligibility reports a class disallowed by a container.
	CodeEligibility Code = "eligibility"
	// CodeSupportRelationship reports stale or altered supporter metadata.
	CodeSupportRelationship Code = "support_relationship"
	// CodeOverflow reports checked geometric or aggregate arithmetic overflow.
	CodeOverflow Code = "overflow"
	// CodeObjective reports objective components differing from recomputation.
	CodeObjective Code = "objective"
	// CodeProofStatus reports inconsistent status, termination, content, or proof.
	CodeProofStatus Code = "proof_status"
	// CodeConstraint reports failure while replaying a custom constraint.
	CodeConstraint Code = "constraint"
)

// Violation is a bounded verifier finding scoped to an item or container when
// applicable.
type Violation struct {
	// Code is the stable machine-readable category.
	Code Code
	// ItemID identifies the affected item, or is empty for plan-wide findings.
	ItemID string
	// ContainerID identifies the affected instance or type when applicable.
	ContainerID string
	// Message is a bounded human-readable explanation.
	Message string
}

// Result contains independent verification findings and callback errors.
type Result struct {
	violations []Violation
	maximum    uint32
	truncated  bool
	err        error
}

// Valid reports whether no violation was found.
func (r Result) Valid() bool { return len(r.violations) == 0 }

// Violations returns a defensive copy in deterministic discovery order.
func (r Result) Violations() []Violation { return slices.Clone(r.violations) }

// Truncated reports whether MaxDiagnostics suppressed additional findings.
func (r Result) Truncated() bool { return r.truncated }

// Err returns a cancellation or custom-extension error encountered during
// verification; callers must inspect it independently from Valid.
func (r Result) Err() error { return r.err }

// Has reports whether at least one finding has code.
func (r Result) Has(code Code) bool {
	return slices.ContainsFunc(r.violations, func(v Violation) bool { return v.Code == code })
}

// Options selects item-conservation and custom-extension verification policy.
type Options struct {
	requireAll    bool
	objective     objective.PlanObjective
	constraints   []constraint.Placement
	constraintErr error
}

// RequireAll requires every request item to appear exactly once as a placement.
func RequireAll() Options { return Options{requireAll: true} }

// AllowUnpacked accepts items listed exactly once in UnpackedItemIDs.
func AllowUnpacked() Options { return Options{} }

// WithObjective requires the verifier to recompute the configured objective
// from immutable request and plan values.
func (o Options) WithObjective(value objective.PlanObjective) Options {
	o.objective = value
	return o
}

// WithConstraints requires the verifier to replay application placement
// constraints against immutable request and plan views.
func (o Options) WithConstraints(values ...constraint.Placement) Options {
	o.constraintErr = constraint.ValidateCallbacks(values)
	if o.constraintErr == nil {
		o.constraints = slices.Clone(values)
	} else {
		o.constraints = nil
	}
	return o
}

type placed struct {
	placement knapsack.Placement
	box       geometry.Cuboid
	item      knapsack.NormalizedItem
}

// Plan recomputes feasibility and statistics without using solver predicates.
func Plan(request knapsack.NormalizedRequest, plan knapsack.Plan, options Options) Result {
	result, _ := PlanContext(context.Background(), request, plan, options)
	return result
}

// PlanJSON strictly decodes a fresh plan value before independently verifying
// it. The supplied byte slice is never retained.
func PlanJSON(ctx context.Context, request knapsack.NormalizedRequest, input []byte, limits packingjson.Limits, options Options) (Result, error) {
	plan, err := packingjson.UnmarshalPlan(input, limits)
	if err != nil {
		return Result{}, err
	}
	return PlanContext(ctx, request, plan, options)
}

// PlanContext performs the same independent verification as Plan while
// allowing callers to stop adversarial or no-longer-needed verification work.
func PlanContext(ctx context.Context, request knapsack.NormalizedRequest, plan knapsack.Plan, options Options) (Result, error) {
	if ctx == nil {
		return Result{}, knapsack.ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if options.constraintErr != nil {
		return Result{maximum: 1, err: options.constraintErr}, options.constraintErr
	}
	items, containers := request.Items(), request.Containers()
	itemByID := make(map[string]knapsack.NormalizedItem, len(items))
	for _, item := range items {
		itemByID[item.ID] = item
	}
	typeByID := make(map[string]knapsack.NormalizedContainer, len(containers))
	for _, container := range containers {
		typeByID[container.ID] = container
	}
	instances := make(map[string]knapsack.NormalizedContainer)
	stock := make(map[string]uint32)
	maximum := request.Limits().MaxDiagnostics
	if maximum == 0 {
		maximum = 1
	}
	result := Result{maximum: maximum}
	for _, instance := range plan.Containers() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		typeInfo, ok := typeByID[instance.TypeID]
		if !ok || instance.ID == "" {
			result.add(CodeUnknownContainer, "", instance.ID, "container instance is unknown")
			continue
		}
		if _, duplicate := instances[instance.ID]; duplicate {
			result.add(CodeStock, "", instance.ID, "duplicate container instance")
		}
		instances[instance.ID] = typeInfo
		stock[instance.TypeID]++
	}
	for _, info := range containers {
		count := stock[info.ID]
		if !info.Stock.Unlimited() && count > info.Stock.Count() {
			result.add(CodeStock, "", info.ID, "finite stock exceeded")
		}
	}
	seen := make(map[string]struct{}, len(items))
	byContainer := make(map[string][]placed)
	for _, placement := range plan.Placements() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		item, ok := itemByID[placement.ItemID]
		if !ok {
			result.add(CodeUnknownItem, placement.ItemID, placement.ContainerID, "item is unknown")
			continue
		}
		if _, duplicate := seen[placement.ItemID]; duplicate {
			result.add(CodeDuplicateItem, placement.ItemID, placement.ContainerID, "item placed more than once")
		}
		seen[placement.ItemID] = struct{}{}
		container, ok := instances[placement.ContainerID]
		if !ok {
			result.add(CodeUnknownContainer, placement.ItemID, placement.ContainerID, "container is unknown")
			continue
		}
		oriented, err := placement.Orientation.Apply(item.Dimensions)
		if err != nil || !slices.Contains(item.Orientations, placement.Orientation) {
			result.add(CodeForbiddenOrientation, item.ID, placement.ContainerID, "orientation is not allowed")
			continue
		}
		if oriented != placement.Dimensions || item.Weight != placement.Weight {
			result.add(CodeAlteredItem, item.ID, placement.ContainerID, "reported dimensions or weight differ from input")
		}
		box, err := geometry.NewCuboid(placement.Origin, oriented)
		if err != nil {
			result.add(CodeOutsideContainer, item.ID, placement.ContainerID, err.Error())
			continue
		}
		outer, _ := geometry.NewCuboid(geometry.Point{}, container.Dimensions)
		if !outer.Contains(box) {
			result.add(CodeOutsideContainer, item.ID, placement.ContainerID, "placement escapes usable space")
		}
		for _, reserved := range container.Reserved {
			if box.Intersects(reserved) {
				result.add(CodeReservedOverlap, item.ID, placement.ContainerID, "placement intersects reserved space")
			}
		}
		if len(container.AllowedClasses) > 0 && !slices.Contains(container.AllowedClasses, item.Attributes["class"]) {
			result.add(CodeEligibility, item.ID, placement.ContainerID, "item class is not allowed")
		}
		byContainer[placement.ContainerID] = append(byContainer[placement.ContainerID], placed{placement, box, item})
	}
	unpacked := make(map[string]struct{})
	for _, id := range plan.UnpackedItemIDs() {
		if _, known := itemByID[id]; !known {
			result.add(CodeUnknownItem, id, "", "unpacked item is unknown")
		}
		if _, duplicate := unpacked[id]; duplicate {
			result.add(CodeDuplicateItem, id, "", "item repeated in unpacked list")
		}
		unpacked[id] = struct{}{}
	}
	for _, item := range items {
		_, packed := seen[item.ID]
		_, listed := unpacked[item.ID]
		if packed && listed {
			result.add(CodeDuplicateItem, item.ID, "", "item is both packed and unpacked")
		}
		if !packed && (options.requireAll || !listed) {
			result.add(CodeMissingItem, item.ID, "", "required item is not packed")
		}
	}
	var itemWeight, itemVolume, containerVolume, remainingWeight int64
	for _, instance := range plan.Containers() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		container, known := instances[instance.ID]
		if !known {
			continue
		}
		containerID := instance.ID
		placements := byContainer[containerID]
		if err := verifyConstraints(ctx, &result, placements, container, containerID, options.constraints); err != nil {
			result.err = err
			return result, err
		}
		var weight int64
		for i, current := range placements {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if !addChecked(&weight, current.item.Weight) || !addChecked(&itemWeight, current.item.Weight) {
				result.add(CodeOverflow, current.item.ID, containerID, "weight accounting overflow")
			}
			volume, err := current.item.Dimensions.Volume()
			if err == nil {
				if !addChecked(&itemVolume, volume) {
					result.add(CodeOverflow, current.item.ID, containerID, "volume accounting overflow")
				}
			}
			for j := i + 1; j < len(placements); j++ {
				if current.box.Intersects(placements[j].box) {
					result.add(CodeOverlap, current.item.ID, containerID, "positive-volume overlap")
				}
			}
		}
		if err := verifySupport(ctx, &result, placements, containerID); err != nil {
			return result, err
		}
		if weight > container.MaxContentWeight || container.HasGrossWeight && weight > container.MaxGrossWeight-container.TareWeight {
			result.add(CodeOverweight, "", containerID, "weight capacity exceeded")
		}
		if !centerOfGravityWithinBounds(placements, container) {
			result.add(CodeCenterOfGravity, "", containerID, "content center of gravity is outside configured bounds")
		}
		if !addChecked(&remainingWeight, container.MaxContentWeight-weight) {
			result.add(CodeOverflow, "", containerID, "remaining weight overflow")
		}
		volume, _ := container.Dimensions.Volume()
		if !addChecked(&containerVolume, volume) {
			result.add(CodeOverflow, "", containerID, "container volume overflow")
		}
		if container.MaxItemCount > 0 && uint64(len(placements)) > uint64(container.MaxItemCount) {
			result.add(CodeStock, "", containerID, "maximum item count exceeded")
		}
		if err := verifyRelations(ctx, &result, placements, containerID); err != nil {
			return result, err
		}
	}
	if err := verifyGrouping(ctx, &result, items, byContainer); err != nil {
		return result, err
	}
	stats := plan.Statistics()
	remainingVolume := containerVolume - itemVolume
	placementCount, containerCount := len(plan.Placements()), len(plan.Containers())
	// #nosec G115 -- normalized request limits cap both collections at uint32.
	if stats.PackedItems != uint32(placementCount) || stats.ContainerCount != uint32(containerCount) || stats.ItemWeight != itemWeight || stats.ItemVolume != itemVolume || stats.ContainerVolume != containerVolume || stats.RemainingWeight != remainingWeight || stats.RemainingVolume != remainingVolume {
		result.add(CodeAccounting, "", "", "reported statistics do not match recomputed totals")
	}
	if options.objective != nil {
		components, err := objective.SafeComponents(ctx, options.objective, request, plan)
		if err != nil {
			result.err = err
		}
		if err != nil || !slices.Equal(components, plan.Objective()) {
			result.add(CodeObjective, "", "", "reported objective does not match recomputed values")
		}
	}
	verifyProofStatus(&result, len(items), plan)
	return result, result.err
}

func centerOfGravityWithinBounds(placements []placed, container knapsack.NormalizedContainer) bool {
	bounds := container.CenterOfGravity
	if bounds == nil || len(placements) == 0 {
		return true
	}
	totalWeight := new(big.Int)
	moments := [3]*big.Int{new(big.Int), new(big.Int), new(big.Int)}
	for _, current := range placements {
		weight := big.NewInt(current.item.Weight)
		totalWeight.Add(totalWeight, weight)
		origin := current.box.Origin()
		dimensions := current.box.Dimensions()
		centers := [3]*big.Int{
			doubledCoordinate(origin.X, dimensions.X),
			doubledCoordinate(origin.Y, dimensions.Y),
			doubledCoordinate(origin.Z, dimensions.Z),
		}
		for axis := range moments {
			moments[axis].Add(moments[axis], new(big.Int).Mul(weight, centers[axis]))
		}
	}
	containerDimensions := [3]int64{container.Dimensions.X, container.Dimensions.Y, container.Dimensions.Z}
	minimums := [3]uint32{bounds.MinXPPM, bounds.MinYPPM, bounds.MinZPPM}
	maximums := [3]uint32{bounds.MaxXPPM, bounds.MaxYPPM, bounds.MaxZPPM}
	for axis, moment := range moments {
		scaledMoment := new(big.Int).Mul(moment, big.NewInt(1_000_000))
		doubledDimension := new(big.Int).Mul(big.NewInt(containerDimensions[axis]), big.NewInt(2))
		scale := new(big.Int).Mul(totalWeight, doubledDimension)
		minimum := new(big.Int).Mul(scale, new(big.Int).SetUint64(uint64(minimums[axis])))
		maximum := new(big.Int).Mul(scale, new(big.Int).SetUint64(uint64(maximums[axis])))
		if scaledMoment.Cmp(minimum) < 0 || scaledMoment.Cmp(maximum) > 0 {
			return false
		}
	}
	return true
}

func doubledCoordinate(origin, dimension int64) *big.Int {
	return new(big.Int).Add(new(big.Int).Mul(big.NewInt(origin), big.NewInt(2)), big.NewInt(dimension))
}

func verifyConstraints(ctx context.Context, result *Result, placements []placed, container knapsack.NormalizedContainer, containerID string, callbacks []constraint.Placement) error {
	if len(callbacks) == 0 {
		return nil
	}
	existing := make([]knapsack.Placement, 0, len(placements))
	for _, current := range placements {
		view, err := constraint.NewPlacementView(current.item, container, current.placement, existing)
		if err != nil {
			result.add(CodeConstraint, current.item.ID, containerID, "custom constraint view exceeds safe bounds")
			return err
		}
		for _, callback := range callbacks {
			decision, err := constraint.Evaluate(ctx, callback, view)
			if err != nil {
				result.add(CodeConstraint, current.item.ID, containerID, "custom constraint could not be verified")
				return err
			}
			if !decision.Accepted {
				result.add(CodeConstraint, current.item.ID, containerID, decision.Code+": "+decision.Message)
			}
		}
		existing = append(existing, current.placement)
	}
	return nil
}

func verifyProofStatus(result *Result, itemCount int, plan knapsack.Plan) {
	status, termination := plan.Status(), plan.Termination()
	placements, unpacked, work := plan.Placements(), plan.UnpackedItemIDs(), plan.Work()
	valid := true
	switch status {
	case knapsack.StatusFeasible:
		valid = termination == knapsack.TerminationCompleted && len(unpacked) == 0
	case knapsack.StatusOptimal:
		valid = termination == knapsack.TerminationCompleted && len(unpacked) == 0 && work.Solver == "exact"
	case knapsack.StatusBestKnown:
		valid = termination == knapsack.TerminationNoPlacement && len(unpacked) > 0
	case knapsack.StatusInfeasible:
		valid = termination == knapsack.TerminationCompleted && len(placements) == 0 && len(unpacked) == itemCount && work.Solver == "exact"
	case knapsack.StatusBudgetExhausted:
		switch termination {
		case knapsack.TerminationCancelled, knapsack.TerminationDeadline, knapsack.TerminationNodeLimit,
			knapsack.TerminationBranchLimit, knapsack.TerminationCandidateLimit, knapsack.TerminationMemoryLimit:
		default:
			valid = false
		}
	default:
		valid = false
	}
	if !valid {
		result.add(CodeProofStatus, "", "", "status, termination, contents, and solver proof are inconsistent")
	}
}

type supportEdge struct {
	itemID string
	area   int64
}

type supportRectangle struct {
	minX int64
	maxX int64
	minY int64
	maxY int64
}

func verifySupport(ctx context.Context, result *Result, all []placed, containerID string) error {
	edges := make(map[string][]supportEdge, len(all))
	above := make(map[string][]string, len(all))
	for _, current := range all {
		if err := ctx.Err(); err != nil {
			return err
		}
		actualIDs := make([]string, 0)
		supportRectangles := make([]supportRectangle, 0)
		if current.box.Origin().Z > 0 {
			for _, candidate := range all {
				if err := ctx.Err(); err != nil {
					return err
				}
				if area, ok := candidate.box.SupportArea(current.box); ok {
					supportRectangles = append(supportRectangles, supportIntersection(candidate.box, current.box))
					edges[current.item.ID] = append(edges[current.item.ID], supportEdge{candidate.item.ID, area})
					above[candidate.item.ID] = append(above[candidate.item.ID], current.item.ID)
					actualIDs = append(actualIDs, candidate.item.ID)
					if candidate.item.FragileTop {
						result.add(CodeFragile, current.item.ID, containerID, "placed on fragile top")
					}
				}
			}
			if current.item.MinimumSupportPPM > 0 {
				supported := supportUnionArea(supportRectangles)
				dimensions := current.box.Dimensions()
				left := new(big.Int).Mul(supported, big.NewInt(1_000_000))
				right := new(big.Int).Mul(big.NewInt(dimensions.X), big.NewInt(dimensions.Y))
				right.Mul(right, big.NewInt(int64(current.item.MinimumSupportPPM)))
				if left.Cmp(right) < 0 {
					result.add(CodeUnsupported, current.item.ID, containerID, "minimum support ratio not met")
				}
			}
		}
		slices.Sort(actualIDs)
		reported := slices.Clone(current.placement.SupporterIDs)
		slices.Sort(reported)
		if !slices.Equal(actualIDs, reported) {
			result.add(CodeSupportRelationship, current.item.ID, containerID, "reported supporters differ from geometry")
		}
	}

	loads := make(map[string]*big.Rat, len(all))
	ordered := slices.Clone(all)
	slices.SortFunc(ordered, func(a, b placed) int { return -compareInt64(a.box.Origin().Z, b.box.Origin().Z) })
	for _, current := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		total := new(big.Rat).Add(ratFor(loads, current.item.ID), new(big.Rat).SetInt64(current.item.Weight))
		areaTotal := new(big.Int)
		var areaTotalBounded int64
		areaOverflow := false
		for _, edge := range edges[current.item.ID] {
			areaTotal.Add(areaTotal, big.NewInt(edge.area))
			if !areaOverflow && !addChecked(&areaTotalBounded, edge.area) {
				result.add(CodeOverflow, current.item.ID, containerID, "load-share area accumulation overflow")
				areaOverflow = true
			}
		}
		if areaTotal.Sign() > 0 {
			for _, edge := range edges[current.item.ID] {
				share := new(big.Rat).Mul(total, new(big.Rat).SetFrac(big.NewInt(edge.area), areaTotal))
				loads[edge.itemID] = new(big.Rat).Add(ratFor(loads, edge.itemID), share)
			}
		}
	}
	for _, current := range all {
		if err := ctx.Err(); err != nil {
			return err
		}
		if current.item.MaxSupportedWeight != nil && ratFor(loads, current.item.ID).Cmp(new(big.Rat).SetInt64(*current.item.MaxSupportedWeight)) > 0 {
			result.add(CodeLoadBearing, current.item.ID, containerID, "transitive supported load exceeded")
		}
		if current.item.MaxStackCount > 0 {
			depth, err := stackDepth(ctx, current.item.ID, above, make(map[string]bool))
			if err != nil {
				return err
			}
			if depth > current.item.MaxStackCount {
				result.add(CodeStackLimit, current.item.ID, containerID, "maximum stack count exceeded")
			}
		}
	}
	return nil
}

func supportIntersection(lower, upper geometry.Cuboid) supportRectangle {
	lowerOrigin, lowerMax := lower.Origin(), lower.Max()
	upperOrigin, upperMax := upper.Origin(), upper.Max()
	return supportRectangle{
		minX: max(lowerOrigin.X, upperOrigin.X),
		maxX: min(lowerMax.X, upperMax.X),
		minY: max(lowerOrigin.Y, upperOrigin.Y),
		maxY: min(lowerMax.Y, upperMax.Y),
	}
}

func supportUnionArea(rectangles []supportRectangle) *big.Int {
	xs := make([]int64, 0, len(rectangles)*2)
	for _, rectangle := range rectangles {
		xs = append(xs, rectangle.minX, rectangle.maxX)
	}
	slices.Sort(xs)
	xs = slices.Compact(xs)
	area := new(big.Int)
	for index := 0; index+1 < len(xs); index++ {
		minX, maxX := xs[index], xs[index+1]
		intervals := make([][2]int64, 0, len(rectangles))
		for _, rectangle := range rectangles {
			if rectangle.minX <= minX && rectangle.maxX >= maxX {
				intervals = append(intervals, [2]int64{rectangle.minY, rectangle.maxY})
			}
		}
		slices.SortFunc(intervals, func(left, right [2]int64) int {
			if order := compareInt64(left[0], right[0]); order != 0 {
				return order
			}
			return compareInt64(left[1], right[1])
		})
		coveredY := new(big.Int)
		var start, end int64
		for intervalIndex, interval := range intervals {
			if intervalIndex == 0 {
				start, end = interval[0], interval[1]
				continue
			}
			if interval[0] <= end {
				end = max(end, interval[1])
				continue
			}
			coveredY.Add(coveredY, big.NewInt(end-start))
			start, end = interval[0], interval[1]
		}
		if len(intervals) > 0 {
			coveredY.Add(coveredY, big.NewInt(end-start))
		}
		width := big.NewInt(maxX - minX)
		area.Add(area, new(big.Int).Mul(width, coveredY))
	}
	return area
}
func verifyRelations(ctx context.Context, result *Result, placements []placed, containerID string) error {
	groups := make(map[string]struct{})
	for _, p := range placements {
		if err := ctx.Err(); err != nil {
			return err
		}
		if p.item.Group != "" {
			groups[p.item.Group] = struct{}{}
		}
	}
	for _, p := range placements {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, incompatible := range p.item.IncompatibleGroups {
			if _, exists := groups[incompatible]; exists {
				result.add(CodeIncompatible, p.item.ID, containerID, "incompatible group shares container")
			}
		}
	}
	return nil
}

func verifyGrouping(ctx context.Context, result *Result, items []knapsack.NormalizedItem, byContainer map[string][]placed) error {
	locations := make(map[string]string, len(items))
	for containerID, placements := range byContainer {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, placement := range placements {
			locations[placement.item.ID] = containerID
		}
	}
	groups := make(map[string]string)
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if item.Group == "" {
			continue
		}
		location := locations[item.ID]
		if existing, found := groups[item.Group]; found && existing != location {
			result.add(CodeGrouping, item.ID, location, "linked group is split across containers or packing states")
		} else if !found {
			groups[item.Group] = location
		}
	}
	return nil
}

func ratFor(values map[string]*big.Rat, key string) *big.Rat {
	if value := values[key]; value != nil {
		return new(big.Rat).Set(value)
	}
	return new(big.Rat)
}

func stackDepth(ctx context.Context, itemID string, above map[string][]string, visiting map[string]bool) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if visiting[itemID] {
		return math.MaxUint32, nil
	}
	visiting[itemID] = true
	var maximum uint32
	for _, supported := range above[itemID] {
		depth, err := stackDepth(ctx, supported, above, visiting)
		if err != nil {
			return 0, err
		}
		if depth == math.MaxUint32 {
			return math.MaxUint32, nil
		}
		if depth+1 > maximum {
			maximum = depth + 1
		}
	}
	delete(visiting, itemID)
	return maximum, nil
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

func addChecked(total *int64, value int64) bool {
	if value > 0 && *total > math.MaxInt64-value || value < 0 && *total < math.MinInt64-value {
		return false
	}
	*total += value
	return true
}
func (r *Result) add(code Code, itemID, containerID, message string) {
	if uint64(len(r.violations)) >= uint64(r.maximum) {
		r.truncated = true
		return
	}
	r.violations = append(r.violations, Violation{code, itemID, containerID, message})
}
