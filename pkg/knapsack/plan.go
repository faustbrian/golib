package knapsack

import (
	"encoding/json"
	"slices"

	"github.com/faustbrian/golib/pkg/knapsack/geometry"
)

// Status distinguishes feasible evidence, exhaustive proof, heuristic partial
// results, and interrupted search.
type Status string

const (
	// StatusFeasible is a complete independently verified non-optimal plan.
	StatusFeasible Status = "feasible"
	// StatusOptimal is a complete plan proven optimal by the exact solver.
	StatusOptimal Status = "optimal"
	// StatusBestKnown is a verified partial heuristic plan without infeasibility
	// proof.
	StatusBestKnown Status = "best_known"
	// StatusInfeasible is exhaustive proof that no complete plan exists.
	StatusInfeasible Status = "infeasible"
	// StatusBudgetExhausted is the best safe verified result when bounded search
	// stops early.
	StatusBudgetExhausted Status = "budget_exhausted"
)

// TerminationReason identifies why solver work stopped independently of status.
type TerminationReason string

const (
	// TerminationCompleted means the selected solver strategy finished.
	TerminationCompleted TerminationReason = "completed"
	// TerminationCancelled means context cancellation stopped work.
	TerminationCancelled TerminationReason = "cancelled"
	// TerminationDeadline means the context deadline expired.
	TerminationDeadline TerminationReason = "deadline"
	// TerminationNodeLimit means MaxSearchNodes was reached.
	TerminationNodeLimit TerminationReason = "node_limit"
	// TerminationBranchLimit means MaxBranches was reached.
	TerminationBranchLimit TerminationReason = "branch_limit"
	// TerminationCandidateLimit means MaxCandidatePlacements was reached.
	TerminationCandidateLimit TerminationReason = "candidate_limit"
	// TerminationMemoryLimit means the conservative memory budget was exceeded.
	TerminationMemoryLimit TerminationReason = "memory_limit"
	// TerminationNoPlacement means a heuristic could not place every item.
	TerminationNoPlacement TerminationReason = "no_placement"
)

// ContainerInstance identifies one selected copy of a container type.
type ContainerInstance struct {
	// ID is the unique deterministic instance identity.
	ID string `json:"id"`
	// TypeID refers to a NormalizedContainer.ID in the request.
	TypeID string `json:"type_id"`
}

// Placement records one item's immutable lattice position and derived support
// evidence.
type Placement struct {
	// ItemID refers to exactly one NormalizedItem.ID.
	ItemID string `json:"item_id"`
	// ContainerID refers to one ContainerInstance.ID.
	ContainerID string `json:"container_id"`
	// Origin is the non-negative lower lattice corner.
	Origin geometry.Point `json:"origin"`
	// Orientation is the selected allowed physical-axis permutation.
	Orientation geometry.Orientation `json:"orientation"`
	// Dimensions are the recomputable oriented lattice lengths.
	Dimensions geometry.Dimensions `json:"dimensions"`
	// Weight is the recomputable item mass-lattice count.
	Weight int64 `json:"weight"`
	// SupporterIDs is the canonical set of items with positive contact beneath.
	SupporterIDs []string `json:"supporter_ids,omitempty"`
	// Diagnostics is bounded non-authoritative placement metadata.
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

// Diagnostic is bounded human-readable solver metadata and never replaces
// independent verification.
type Diagnostic struct {
	// Code is the stable diagnostic category.
	Code string `json:"code"`
	// ItemID optionally scopes the diagnostic to an item.
	ItemID string `json:"item_id,omitempty"`
	// ContainerID optionally scopes the diagnostic to an instance or type.
	ContainerID string `json:"container_id,omitempty"`
	// Message is the bounded human-readable explanation.
	Message string `json:"message"`
}

// ScoreComponent is one exact serializable objective value in precedence order.
type ScoreComponent struct {
	// Name is the stable objective metric identifier.
	Name string `json:"name"`
	// Direction is either "min" or "max".
	Direction string `json:"direction"`
	// Unit identifies the exact count, lattice, priority, or currency domain.
	Unit string `json:"unit"`
	// Value is the canonical exact decimal representation.
	Value string `json:"value"`
}

// Statistics contains verifier-recomputable exact plan aggregates.
type Statistics struct {
	// PackedItems is the placement count.
	PackedItems uint32 `json:"packed_items"`
	// ContainerCount is the selected instance count.
	ContainerCount uint32 `json:"container_count"`
	// ItemWeight is total packed mass in request lattice units.
	ItemWeight int64 `json:"item_weight"`
	// ItemVolume is total packed volume in cubed length-lattice units.
	ItemVolume int64 `json:"item_volume"`
	// ContainerVolume is total selected usable lattice volume.
	ContainerVolume int64 `json:"container_volume"`
	// RemainingWeight is unused content capacity in mass-lattice units.
	RemainingWeight int64 `json:"remaining_weight"`
	// RemainingVolume is container volume minus item volume.
	RemainingVolume int64 `json:"remaining_volume"`
}

// Work records deterministic bounded search counters, not wall-clock timing.
type Work struct {
	// Solver is the stable solver implementation identifier.
	Solver string `json:"solver"`
	// Strategy is the stable search-strategy identifier.
	Strategy string `json:"strategy"`
	// Seed is the caller-provided reproducibility value.
	Seed uint64 `json:"seed"`
	// Nodes is the number of exact-search states entered.
	Nodes uint64 `json:"nodes"`
	// Branches is the number of exact-search edges or configurations considered.
	Branches uint64 `json:"branches"`
	// CandidatePlacements is the number of orientation-position attempts.
	CandidatePlacements uint64 `json:"candidate_placements"`
	// ImprovementRounds is the number of complete heuristic repacking passes.
	ImprovementRounds uint32 `json:"improvement_rounds"`
}

// PlanSpec is the owned serialization boundary validated by NewPlanWithLimits.
type PlanSpec struct {
	// Containers is the selected canonical container instance list.
	Containers []ContainerInstance `json:"containers"`
	// Placements contains at most one entry for each packed item.
	Placements []Placement `json:"placements"`
	// UnpackedItemIDs identifies omitted items for permitted partial plans.
	UnpackedItemIDs []string `json:"unpacked_item_ids"`
	// Objective contains exact components in precedence order.
	Objective []ScoreComponent `json:"objective"`
	// Status states the level of feasibility or proof claimed.
	Status Status `json:"status"`
	// Termination states why solver work stopped.
	Termination TerminationReason `json:"termination"`
	// Statistics contains independently recomputable aggregates.
	Statistics Statistics `json:"statistics"`
	// Work contains deterministic bounded search counters.
	Work Work `json:"work"`
	// Diagnostics contains bounded non-authoritative plan metadata.
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// PlanLimits bounds direct plan construction before collection cloning or
// canonical serialization can amplify untrusted data.
type PlanLimits struct {
	// MaxContainers bounds selected instance records.
	MaxContainers uint32
	// MaxPlacements bounds placement records.
	MaxPlacements uint32
	// MaxUnpackedItems bounds partial-result identity records.
	MaxUnpackedItems uint32
	// MaxObjectiveComponents bounds lexicographic score records.
	MaxObjectiveComponents uint32
	// MaxDiagnostics bounds plan and placement diagnostics combined.
	MaxDiagnostics uint32
	// MaxIDBytes bounds every item, type, instance, and supporter ID.
	MaxIDBytes uint32
	// MaxMetadataBytes bounds each diagnostic and objective string.
	MaxMetadataBytes uint32
	// MaxBytes bounds the conservative owned plan-size estimate.
	MaxBytes uint64
}

// DefaultPlanLimits returns conservative limits for server-side untrusted
// plan construction.
func DefaultPlanLimits() PlanLimits {
	return PlanLimits{
		MaxContainers: 10_000, MaxPlacements: 10_000, MaxUnpackedItems: 10_000,
		MaxObjectiveComponents: 32, MaxDiagnostics: 1_000, MaxIDBytes: 1_024,
		MaxMetadataBytes: 64 << 10, MaxBytes: 16 << 20,
	}
}

func (l PlanLimits) valid() bool {
	return l.MaxContainers > 0 && l.MaxPlacements > 0 && l.MaxUnpackedItems > 0 &&
		l.MaxObjectiveComponents > 0 && l.MaxDiagnostics > 0 && l.MaxIDBytes > 0 &&
		l.MaxMetadataBytes > 0 && l.MaxBytes > 0
}

// Plan owns defensive copies and only exposes copies of collection state.
type Plan struct{ spec PlanSpec }

// NewPlan validates and defensively copies spec using DefaultPlanLimits.
func NewPlan(spec PlanSpec) (Plan, error) {
	return NewPlanWithLimits(spec, DefaultPlanLimits())
}

// NewPlanWithLimits validates resource bounds before taking defensive copies
// of the supplied plan.
func NewPlanWithLimits(spec PlanSpec, limits PlanLimits) (Plan, error) {
	if !limits.valid() {
		return Plan{}, ErrInvalidOptions
	}
	if !validStatus(spec.Status) || !validTermination(spec.Termination) {
		return Plan{}, ErrInvalidRequest
	}
	if len(spec.Containers) > int(limits.MaxContainers) || len(spec.Placements) > int(limits.MaxPlacements) ||
		len(spec.UnpackedItemIDs) > int(limits.MaxUnpackedItems) || len(spec.Objective) > int(limits.MaxObjectiveComponents) {
		return Plan{}, ErrBudgetExhausted
	}
	if !planWithinLimits(spec, limits) {
		return Plan{}, ErrBudgetExhausted
	}
	return Plan{spec: clonePlanSpec(spec)}, nil
}

func planWithinLimits(spec PlanSpec, limits PlanLimits) bool {
	estimate := memoryEstimate{limit: limits.MaxBytes}
	if !estimate.add(uint64(len(spec.Containers)), 64) || !estimate.add(uint64(len(spec.Placements)), 256) ||
		!estimate.add(uint64(len(spec.UnpackedItemIDs)), 16) || !estimate.add(uint64(len(spec.Objective)), 64) ||
		!estimate.add(uint64(len(spec.Diagnostics)), 64) {
		return false
	}
	diagnostics := uint64(len(spec.Diagnostics))
	addString := func(value string, maximum uint32) bool {
		return len(value) <= int(maximum) && estimate.add(uint64(len(value)), 6)
	}
	for _, container := range spec.Containers {
		if !addString(container.ID, limits.MaxIDBytes) || !addString(container.TypeID, limits.MaxIDBytes) {
			return false
		}
	}
	for _, placement := range spec.Placements {
		if !addString(placement.ItemID, limits.MaxIDBytes) || !addString(placement.ContainerID, limits.MaxIDBytes) ||
			!estimate.add(uint64(len(placement.SupporterIDs)), 16) || !estimate.add(uint64(len(placement.Diagnostics)), 64) {
			return false
		}
		diagnostics += uint64(len(placement.Diagnostics))
		for _, supporter := range placement.SupporterIDs {
			if !addString(supporter, limits.MaxIDBytes) {
				return false
			}
		}
		for _, diagnostic := range placement.Diagnostics {
			if !planDiagnosticWithinLimits(diagnostic, limits, addString) {
				return false
			}
		}
	}
	if diagnostics > uint64(limits.MaxDiagnostics) {
		return false
	}
	for _, id := range spec.UnpackedItemIDs {
		if !addString(id, limits.MaxIDBytes) {
			return false
		}
	}
	for _, component := range spec.Objective {
		if !addString(component.Name, limits.MaxMetadataBytes) || !addString(component.Direction, limits.MaxMetadataBytes) ||
			!addString(component.Unit, limits.MaxMetadataBytes) || !addString(component.Value, limits.MaxMetadataBytes) {
			return false
		}
	}
	for _, diagnostic := range spec.Diagnostics {
		if !planDiagnosticWithinLimits(diagnostic, limits, addString) {
			return false
		}
	}
	return true
}

func planDiagnosticWithinLimits(diagnostic Diagnostic, limits PlanLimits, addString func(string, uint32) bool) bool {
	return addString(diagnostic.Code, limits.MaxMetadataBytes) &&
		addString(diagnostic.ItemID, limits.MaxIDBytes) &&
		addString(diagnostic.ContainerID, limits.MaxIDBytes) &&
		addString(diagnostic.Message, limits.MaxMetadataBytes)
}

// Spec returns a deep defensive copy suitable for serialization or mutation.
func (p Plan) Spec() PlanSpec { return clonePlanSpec(p.spec) }

// Containers returns a defensive copy in canonical order.
func (p Plan) Containers() []ContainerInstance { return slices.Clone(p.spec.Containers) }

// Placements returns deep defensive copies in canonical order.
func (p Plan) Placements() []Placement { return clonePlacements(p.spec.Placements) }

// UnpackedItemIDs returns a defensive copy in canonical order.
func (p Plan) UnpackedItemIDs() []string { return slices.Clone(p.spec.UnpackedItemIDs) }

// Objective returns a defensive copy in lexicographic precedence order.
func (p Plan) Objective() []ScoreComponent { return slices.Clone(p.spec.Objective) }

// Status returns the claimed feasibility or proof level.
func (p Plan) Status() Status { return p.spec.Status }

// Termination returns the exact reason solver work stopped.
func (p Plan) Termination() TerminationReason { return p.spec.Termination }

// Statistics returns independently recomputable aggregates.
func (p Plan) Statistics() Statistics { return p.spec.Statistics }

// Work returns deterministic search counters.
func (p Plan) Work() Work { return p.spec.Work }

// Diagnostics returns a defensive copy of non-authoritative metadata.
func (p Plan) Diagnostics() []Diagnostic { return slices.Clone(p.spec.Diagnostics) }

// CanonicalString returns deterministic compact JSON for comparison and
// tie-breaking; persistence should use the versioned encoding package.
func (p Plan) CanonicalString() string { encoded, _ := json.Marshal(p.spec); return string(encoded) }

func validStatus(status Status) bool {
	switch status {
	case StatusFeasible, StatusOptimal, StatusBestKnown, StatusInfeasible, StatusBudgetExhausted:
		return true
	default:
		return false
	}
}

func validTermination(termination TerminationReason) bool {
	switch termination {
	case TerminationCompleted, TerminationCancelled, TerminationDeadline,
		TerminationNodeLimit, TerminationBranchLimit, TerminationCandidateLimit,
		TerminationMemoryLimit, TerminationNoPlacement:
		return true
	default:
		return false
	}
}
func clonePlanSpec(spec PlanSpec) PlanSpec {
	spec.Containers = slices.Clone(spec.Containers)
	spec.Placements = clonePlacements(spec.Placements)
	spec.UnpackedItemIDs = slices.Clone(spec.UnpackedItemIDs)
	spec.Objective = slices.Clone(spec.Objective)
	spec.Diagnostics = slices.Clone(spec.Diagnostics)
	return spec
}
func clonePlacements(source []Placement) []Placement {
	result := slices.Clone(source)
	for index := range result {
		result[index].SupporterIDs = slices.Clone(result[index].SupporterIDs)
		result[index].Diagnostics = slices.Clone(result[index].Diagnostics)
	}
	return result
}
