// Package constraint defines a small immutable callback boundary for
// application-specific placement feasibility.
package constraint

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/faustbrian/golib/pkg/knapsack"
)

var (
	// ErrInvalidConstraint identifies nil or typed-nil callback values.
	ErrInvalidConstraint = errors.New("constraint: invalid callback")
	// ErrInvalidDecision identifies malformed callback output.
	ErrInvalidDecision = errors.New("constraint: invalid decision")
	// ErrCallbackPanic wraps a panic raised by application callback code.
	ErrCallbackPanic = errors.New("constraint: callback panic")
	// ErrViewLimit identifies callback input that exceeds safe copy bounds.
	ErrViewLimit = errors.New("constraint: placement view limit exceeded")
)

const (
	maxCodeBytes      = 64
	maxMessageBytes   = 1024
	maxViewPlacements = 10_000
	maxViewBytes      = 16 << 20
	// MaxCallbacks bounds synchronous predicates attached to one operation.
	MaxCallbacks = 32
)

// Decision is a bounded acceptance or rejection returned by a placement
// callback.
type Decision struct {
	// Accepted reports whether packing may retain the candidate.
	Accepted bool
	// Code is the stable application rejection identifier.
	Code string
	// Message is the bounded human-readable rejection explanation.
	Message string
}

// Accept constructs a valid acceptance decision.
func Accept() Decision { return Decision{Accepted: true} }

// Reject constructs a rejection decision validated when the callback returns.
func Reject(code, message string) Decision { return Decision{Code: code, Message: message} }

// ValidateDecision rejects contradictory, empty, or oversized decisions.
func ValidateDecision(decision Decision) (Decision, error) {
	if decision.Accepted {
		if decision.Code != "" || decision.Message != "" {
			return Decision{}, ErrInvalidDecision
		}
		return decision, nil
	}
	if decision.Code == "" || decision.Message == "" || len(decision.Code) > maxCodeBytes || len(decision.Message) > maxMessageBytes {
		return Decision{}, ErrInvalidDecision
	}
	return decision, nil
}

// Placement is the narrow synchronous custom feasibility extension point.
// Implementations must be deterministic, bounded, and honor context.
type Placement interface {
	// Check decides whether the immutable candidate view is acceptable.
	Check(context.Context, PlacementView) Decision
}

// PlacementView owns copies of every collection supplied to a callback.
type PlacementView struct {
	item       knapsack.NormalizedItem
	container  knapsack.NormalizedContainer
	candidate  knapsack.Placement
	placements []knapsack.Placement
}

// NewPlacementView allows at most 10,000 prior placements and a conservative
// 16 MiB owned-copy estimate before defensively copying callback-visible state.
func NewPlacementView(item knapsack.NormalizedItem, container knapsack.NormalizedContainer, candidate knapsack.Placement, placements []knapsack.Placement) (PlacementView, error) {
	if len(placements) > maxViewPlacements || !placementViewWithinLimits(item, container, candidate, placements) {
		return PlacementView{}, ErrViewLimit
	}
	return PlacementView{item: cloneItem(item), container: cloneContainer(container), candidate: clonePlacement(candidate), placements: clonePlacements(placements)}, nil
}

// Item returns a defensive copy of the candidate item.
func (v PlacementView) Item() knapsack.NormalizedItem { return cloneItem(v.item) }

// Container returns a defensive copy of the target container type.
func (v PlacementView) Container() knapsack.NormalizedContainer { return cloneContainer(v.container) }

// Candidate returns a defensive copy of the proposed placement.
func (v PlacementView) Candidate() knapsack.Placement { return clonePlacement(v.candidate) }

// Placements returns defensive copies of placements already in the container.
func (v PlacementView) Placements() []knapsack.Placement { return clonePlacements(v.placements) }

// Evaluate invokes a callback with panic conversion and decision validation.
func Evaluate(ctx context.Context, callback Placement, view PlacementView) (decision Decision, err error) {
	if ctx == nil {
		return Decision{}, ErrInvalidConstraint
	}
	if callback == nil || typedNil(callback) {
		return Decision{}, ErrInvalidConstraint
	}
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			decision = Decision{}
			err = fmt.Errorf("%w: %v", ErrCallbackPanic, recovered)
		}
	}()
	return ValidateDecision(callback.Check(ctx, view))
}

// ValidateCallbacks rejects excessive, nil, and typed-nil callback lists
// before callers clone or execute them.
func ValidateCallbacks(values []Placement) error {
	if len(values) > MaxCallbacks {
		return ErrInvalidConstraint
	}
	for _, value := range values {
		if value == nil || typedNil(value) {
			return ErrInvalidConstraint
		}
	}
	return nil
}

func typedNil(value any) bool {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

type viewEstimate struct{ used uint64 }

func (e *viewEstimate) add(count, size uint64) bool {
	if size > 0 && count > (maxViewBytes-e.used)/size {
		return false
	}
	e.used += count * size
	return true
}

func (e *viewEstimate) addString(value string) bool {
	return e.add(uint64(len(value)), 1)
}

func placementViewWithinLimits(item knapsack.NormalizedItem, container knapsack.NormalizedContainer, candidate knapsack.Placement, placements []knapsack.Placement) bool {
	estimate := viewEstimate{}
	if !estimate.add(3, 256) || !estimate.add(uint64(len(placements)), 256) ||
		!estimate.add(uint64(len(item.Orientations)), 16) ||
		!estimate.add(uint64(len(item.IncompatibleGroups)), 16) ||
		!estimate.add(uint64(len(item.Attributes)), 64) ||
		!estimate.add(uint64(len(container.AllowedClasses)), 16) ||
		!estimate.add(uint64(len(container.Reserved)), 64) ||
		!placementWithinLimits(&estimate, candidate) ||
		!estimate.addString(item.ID) || !estimate.addString(item.SKU) ||
		!estimate.addString(item.Group) || !estimate.addString(container.ID) {
		return false
	}
	for key, value := range item.Attributes {
		if !estimate.addString(key) || !estimate.addString(value) {
			return false
		}
	}
	for _, group := range item.IncompatibleGroups {
		if !estimate.addString(group) {
			return false
		}
	}
	for _, class := range container.AllowedClasses {
		if !estimate.addString(class) {
			return false
		}
	}
	for _, placement := range placements {
		if !placementWithinLimits(&estimate, placement) {
			return false
		}
	}
	return true
}

func placementWithinLimits(estimate *viewEstimate, placement knapsack.Placement) bool {
	if !estimate.add(uint64(len(placement.SupporterIDs)), 16) ||
		!estimate.add(uint64(len(placement.Diagnostics)), 64) ||
		!estimate.addString(placement.ItemID) || !estimate.addString(placement.ContainerID) {
		return false
	}
	for _, supporter := range placement.SupporterIDs {
		if !estimate.addString(supporter) {
			return false
		}
	}
	for _, diagnostic := range placement.Diagnostics {
		if !estimate.addString(diagnostic.Code) || !estimate.addString(diagnostic.ItemID) ||
			!estimate.addString(diagnostic.ContainerID) || !estimate.addString(diagnostic.Message) {
			return false
		}
	}
	return true
}

func cloneItem(item knapsack.NormalizedItem) knapsack.NormalizedItem {
	item.Orientations = slices.Clone(item.Orientations)
	item.IncompatibleGroups = slices.Clone(item.IncompatibleGroups)
	if item.Attributes != nil {
		attributes := make(map[string]string, len(item.Attributes))
		maps.Copy(attributes, item.Attributes)
		item.Attributes = attributes
	}
	if item.MaxSupportedWeight != nil {
		value := *item.MaxSupportedWeight
		item.MaxSupportedWeight = &value
	}
	return item
}
func cloneContainer(container knapsack.NormalizedContainer) knapsack.NormalizedContainer {
	container.AllowedClasses = slices.Clone(container.AllowedClasses)
	container.Reserved = slices.Clone(container.Reserved)
	if container.CenterOfGravity != nil {
		bounds := *container.CenterOfGravity
		container.CenterOfGravity = &bounds
	}
	return container
}
func clonePlacement(placement knapsack.Placement) knapsack.Placement {
	placement.SupporterIDs = slices.Clone(placement.SupporterIDs)
	placement.Diagnostics = slices.Clone(placement.Diagnostics)
	return placement
}
func clonePlacements(placements []knapsack.Placement) []knapsack.Placement {
	result := slices.Clone(placements)
	for index := range result {
		result[index] = clonePlacement(result[index])
	}
	return result
}
