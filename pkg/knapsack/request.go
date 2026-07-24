package knapsack

import (
	"errors"
	"math"
	"math/big"
	"slices"
	"strings"

	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/measurement"
)

// Resolution defines exact positive length and mass lattice steps. Every input
// quantity must be exactly divisible unless a future explicit rounding API is
// selected.
type Resolution struct {
	// Length is the exact coordinate and dimension lattice step.
	Length measurement.Quantity
	// Mass is the exact weight and load lattice step.
	Mass measurement.Quantity
}

// Limits bounds normalization and solver work before allocations occur.
type Limits struct {
	// MaxItems bounds normalized item instances.
	MaxItems uint32
	// MaxContainerTypes bounds distinct container definitions.
	MaxContainerTypes uint32
	// MaxOrientations bounds allowed orientations per item.
	MaxOrientations uint32
	// MaxCandidatePlacements bounds heuristic and exact placement attempts.
	MaxCandidatePlacements uint64
	// MaxSearchNodes bounds exact-search states.
	MaxSearchNodes uint64
	// MaxBranches bounds exact-search edges and container configurations.
	MaxBranches uint64
	// MaxImprovementRounds bounds complete heuristic repacking passes.
	MaxImprovementRounds uint32
	// MaxMemoryBytes bounds conservative request and solver working estimates.
	MaxMemoryBytes uint64
	// MaxIDBytes bounds each item, type, and instance identifier.
	MaxIDBytes uint32
	// MaxDiagnostics bounds verifier and solver diagnostic collections.
	MaxDiagnostics uint32
}

// DefaultLimits returns conservative nonzero bounds for untrusted server work.
func DefaultLimits() Limits {
	return Limits{MaxItems: 10_000, MaxContainerTypes: 1_000, MaxOrientations: 6,
		MaxCandidatePlacements: 5_000_000, MaxSearchNodes: 1_000_000,
		MaxBranches: 5_000_000, MaxImprovementRounds: 1,
		MaxMemoryBytes: 256 << 20,
		MaxIDBytes:     1024, MaxDiagnostics: 1000}
}

func (l Limits) validate() error {
	if l.MaxItems == 0 || l.MaxContainerTypes == 0 || l.MaxOrientations == 0 ||
		l.MaxCandidatePlacements == 0 || l.MaxSearchNodes == 0 || l.MaxBranches == 0 ||
		l.MaxMemoryBytes == 0 || l.MaxIDBytes == 0 || l.MaxDiagnostics == 0 {
		return ErrInvalidOptions
	}
	return nil
}

// validateForSolver is exposed within the module through a stable boolean
// helper so subpackages can reject malformed copied limits.
func (l Limits) Valid() bool { return l.validate() == nil }

// NormalizedItem is a validated integer-lattice item. NewNormalizedRequest
// owns defensive copies of every slice, map, and pointer field.
type NormalizedItem struct {
	// ID is the unique logical item identity.
	ID string
	// SKU is optional non-identity metadata.
	SKU string
	// Dimensions are positive length-lattice counts.
	Dimensions geometry.Dimensions
	// Weight is a positive mass-lattice count.
	Weight int64
	// Orientations is the canonical nonempty allowed permutation set.
	Orientations []geometry.Orientation
	// Attributes is copied metadata used by eligibility constraints.
	Attributes map[string]string
	// FragileTop forbids supported placements above this item.
	FragileTop bool
	// MaxSupportedWeight is an optional non-negative mass-lattice load limit.
	MaxSupportedWeight *int64
	// MinimumSupportPPM is required footprint support in millionths.
	MinimumSupportPPM uint32
	// Group requires co-location with all items carrying the same value.
	Group string
	// IncompatibleGroups may not share a container with this item.
	IncompatibleGroups []string
	// MaxStackCount bounds supported levels above; zero is unrestricted.
	MaxStackCount uint32
	// Priority is the exact PackedPriority objective contribution.
	Priority int64
}

// NormalizedContainer is a validated integer-lattice container type.
// NewNormalizedRequest owns defensive copies of collection fields.
type NormalizedContainer struct {
	// ID is the unique container type identity.
	ID string
	// Dimensions are positive usable length-lattice counts.
	Dimensions geometry.Dimensions
	// MaxContentWeight is the positive content mass-lattice limit.
	MaxContentWeight int64
	// TareWeight is the non-negative empty-container mass-lattice value.
	TareWeight int64
	// MaxGrossWeight is tare plus content capacity when HasGrossWeight is true.
	MaxGrossWeight int64
	// HasGrossWeight distinguishes an absent limit from a numeric value.
	HasGrossWeight bool
	// Stock explicitly selects finite or unlimited type availability.
	Stock Stock
	// Priority is the deterministic type-ordering value.
	Priority int64
	// MaxItemCount limits placements per instance; zero is unrestricted.
	MaxItemCount uint32
	// AllowedClasses is a copied item-class allowlist.
	AllowedClasses []string
	// Reserved is a copied set of unusable cuboids inside Dimensions.
	Reserved []geometry.Cuboid
	// CenterOfGravity optionally bounds combined content mass by axis PPM.
	CenterOfGravity *CenterOfGravityBounds
}

// NormalizedRequest is immutable validated solver input with canonical item
// and container ordering.
type NormalizedRequest struct {
	items       []NormalizedItem
	containers  []NormalizedContainer
	resolution  Resolution
	limits      Limits
	memoryBytes uint64
}

// NormalizedSpec is the validated wire boundary for an already lattice-scaled
// request. It exists for strict decoding and corpus fixtures.
type NormalizedSpec struct {
	// Items are already expressed in the selected integer lattice.
	Items []NormalizedItem
	// Containers are already expressed in the selected integer lattice.
	Containers []NormalizedContainer
	// Resolution preserves the exact physical meaning of lattice units.
	Resolution Resolution
	// Limits bounds construction, solving, verification, and diagnostics.
	Limits Limits
}

// NewNormalizedRequest validates a pre-scaled request, rejects duplicate IDs
// and overflow, canonicalizes ordering, and defensively copies all aliases.
func NewNormalizedRequest(spec NormalizedSpec) (NormalizedRequest, error) {
	if !spec.Limits.Valid() || len(spec.Items) == 0 || len(spec.Items) > int(spec.Limits.MaxItems) || len(spec.Containers) == 0 || len(spec.Containers) > int(spec.Limits.MaxContainerTypes) {
		return NormalizedRequest{}, ErrInvalidRequest
	}
	memoryBytes, withinMemory := normalizedMemoryUsage(spec.Items, spec.Containers, spec.Limits.MaxMemoryBytes)
	if !withinMemory {
		return NormalizedRequest{}, ErrMemoryBudgetExhausted
	}
	lengthDimension, lengthErr := spec.Resolution.Length.Dimension()
	massDimension, massErr := spec.Resolution.Mass.Dimension()
	if lengthErr != nil || massErr != nil || lengthDimension != measurement.LengthDimension || massDimension != measurement.MassDimension {
		return NormalizedRequest{}, ErrInvalidRequest
	}
	if err := positiveResolution(spec.Resolution.Length); err != nil {
		return NormalizedRequest{}, err
	}
	if err := positiveResolution(spec.Resolution.Mass); err != nil {
		return NormalizedRequest{}, err
	}
	itemIDs := make(map[string]struct{}, len(spec.Items))
	for _, item := range spec.Items {
		if strings.TrimSpace(item.ID) == "" || len(item.ID) > int(spec.Limits.MaxIDBytes) || !item.Dimensions.Valid() || item.Weight <= 0 || len(item.Orientations) == 0 || len(item.Orientations) > int(spec.Limits.MaxOrientations) || item.MinimumSupportPPM > 1_000_000 {
			return NormalizedRequest{}, ErrInvalidItem
		}
		if _, err := item.Dimensions.Volume(); err != nil {
			return NormalizedRequest{}, ErrOverflow
		}
		if _, duplicate := itemIDs[item.ID]; duplicate {
			return NormalizedRequest{}, ErrDuplicateID
		}
		itemIDs[item.ID] = struct{}{}
		seenOrientations := make(map[geometry.Orientation]struct{}, len(item.Orientations))
		for _, orientation := range item.Orientations {
			if _, err := orientation.Apply(item.Dimensions); err != nil {
				return NormalizedRequest{}, ErrInvalidItem
			}
			if _, duplicate := seenOrientations[orientation]; duplicate {
				return NormalizedRequest{}, ErrInvalidItem
			}
			seenOrientations[orientation] = struct{}{}
		}
		if item.MaxSupportedWeight != nil && *item.MaxSupportedWeight < 0 {
			return NormalizedRequest{}, ErrInvalidItem
		}
	}
	containerIDs := make(map[string]struct{}, len(spec.Containers))
	for _, container := range spec.Containers {
		if strings.TrimSpace(container.ID) == "" || len(container.ID) > int(spec.Limits.MaxIDBytes) || !container.Dimensions.Valid() || container.MaxContentWeight <= 0 || !container.Stock.Unlimited() && container.Stock.Count() == 0 {
			return NormalizedRequest{}, ErrInvalidContainer
		}
		if _, err := container.Dimensions.Volume(); err != nil {
			return NormalizedRequest{}, ErrOverflow
		}
		if _, duplicate := containerIDs[container.ID]; duplicate {
			return NormalizedRequest{}, ErrDuplicateID
		}
		containerIDs[container.ID] = struct{}{}
		outer, _ := geometry.NewCuboid(geometry.Point{}, container.Dimensions)
		for _, reserved := range container.Reserved {
			if !outer.Contains(reserved) {
				return NormalizedRequest{}, ErrInvalidContainer
			}
		}
		if container.TareWeight < 0 || container.HasGrossWeight && container.MaxGrossWeight < container.TareWeight {
			return NormalizedRequest{}, ErrInvalidContainer
		}
		if container.CenterOfGravity != nil && !container.CenterOfGravity.valid() {
			return NormalizedRequest{}, ErrInvalidContainer
		}
	}
	result := NormalizedRequest{items: cloneNormalizedItems(spec.Items), containers: cloneNormalizedContainers(spec.Containers), resolution: spec.Resolution, limits: spec.Limits, memoryBytes: memoryBytes}
	slices.SortFunc(result.items, func(a, b NormalizedItem) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(result.containers, func(a, b NormalizedContainer) int { return strings.Compare(a.ID, b.ID) })
	return result, nil
}

// Items returns deep defensive copies in canonical ID order.
func (r NormalizedRequest) Items() []NormalizedItem { return cloneNormalizedItems(r.items) }

// Containers returns defensive copies in canonical type-ID order.
func (r NormalizedRequest) Containers() []NormalizedContainer {
	return cloneNormalizedContainers(r.containers)
}

// Resolution returns the exact physical lattice steps.
func (r NormalizedRequest) Resolution() Resolution { return r.resolution }

// Limits returns the request's copied resource policy.
func (r NormalizedRequest) Limits() Limits { return r.limits }

// MemoryBytes returns the conservative normalized-input memory estimate.
func (r NormalizedRequest) MemoryBytes() uint64 { return r.memoryBytes }

// ItemCount returns the bounded normalized item instance count.
func (r NormalizedRequest) ItemCount() int { return len(r.items) }

// ContainerTypeCount returns the bounded distinct container type count.
func (r NormalizedRequest) ContainerTypeCount() int { return len(r.containers) }

// WithLimits returns a copy using caller-selected bounded work limits.
func (r NormalizedRequest) WithLimits(limits Limits) NormalizedRequest {
	r.limits = limits

	return r
}

// Request is immutable exact physical input normalized at construction.
type Request struct{ normalized NormalizedRequest }

// NewRequest validates exact physical values, requires exact lattice
// conversion, rejects ambiguous IDs, and owns defensive copies.
func NewRequest(items []Item, containers []ContainerType, resolution Resolution, limits Limits) (Request, error) {
	if err := limits.validate(); err != nil {
		return Request{}, err
	}
	if len(items) == 0 || len(items) > int(limits.MaxItems) || len(containers) == 0 || len(containers) > int(limits.MaxContainerTypes) {
		return Request{}, ErrInvalidRequest
	}
	if !domainMemoryWithin(items, containers, limits.MaxMemoryBytes) {
		return Request{}, ErrMemoryBudgetExhausted
	}
	if err := positiveResolution(resolution.Length); err != nil {
		return Request{}, &FieldError{Category: ErrInvalidRequest, Field: "length_resolution", Reason: err.Error()}
	}
	if err := positiveResolution(resolution.Mass); err != nil {
		return Request{}, &FieldError{Category: ErrInvalidRequest, Field: "mass_resolution", Reason: err.Error()}
	}

	normalized := NormalizedRequest{resolution: resolution, limits: limits}
	itemIDs := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, duplicate := itemIDs[item.ID()]; duplicate {
			return Request{}, &FieldError{Category: ErrDuplicateID, ID: item.ID()}
		}
		itemIDs[item.ID()] = struct{}{}
	}
	for _, item := range items {
		if len(item.ID()) > int(limits.MaxIDBytes) {
			return Request{}, &FieldError{Category: ErrInvalidItem, ID: item.ID(), Field: "id", Reason: "too long"}
		}
		n, err := normalizeItem(item, resolution)
		if err != nil {
			return Request{}, err
		}
		normalized.items = append(normalized.items, n)
	}
	containerIDs := make(map[string]struct{}, len(containers))
	for _, container := range containers {
		if len(container.ID()) > int(limits.MaxIDBytes) {
			return Request{}, &FieldError{Category: ErrInvalidContainer, ID: container.ID(), Field: "id", Reason: "too long"}
		}
		if _, duplicate := containerIDs[container.ID()]; duplicate {
			return Request{}, &FieldError{Category: ErrDuplicateID, ID: container.ID()}
		}
		containerIDs[container.ID()] = struct{}{}
		n, err := normalizeContainer(container, resolution)
		if err != nil {
			return Request{}, err
		}
		normalized.containers = append(normalized.containers, n)
	}
	slices.SortFunc(normalized.items, func(a, b NormalizedItem) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(normalized.containers, func(a, b NormalizedContainer) int { return strings.Compare(a.ID, b.ID) })
	// The pre-normalization estimate uses the same sizes and normalization can
	// only deduplicate metadata, so this calculation cannot exceed that bound.
	normalized.memoryBytes, _ = normalizedMemoryUsage(normalized.items, normalized.containers, math.MaxUint64)
	return Request{normalized: normalized}, nil
}

type memoryEstimate struct {
	used  uint64
	limit uint64
}

func (e *memoryEstimate) add(count, size uint64) bool {
	if size > 0 && count > (e.limit-e.used)/size {
		return false
	}
	e.used += count * size
	return true
}

func (e *memoryEstimate) addString(value string) bool {
	return e.add(uint64(len(value)), 1)
}

func normalizedMemoryUsage(items []NormalizedItem, containers []NormalizedContainer, limit uint64) (uint64, bool) {
	estimate := memoryEstimate{limit: limit}
	if !estimate.add(uint64(len(items)), 256) || !estimate.add(uint64(len(containers)), 256) {
		return estimate.used, false
	}
	for _, item := range items {
		if !estimate.addString(item.ID) || !estimate.addString(item.SKU) || !estimate.addString(item.Group) ||
			!estimate.add(uint64(len(item.Orientations)), 16) || !estimate.add(uint64(len(item.IncompatibleGroups)), 16) ||
			!estimate.add(uint64(len(item.Attributes)), 64) {
			return estimate.used, false
		}
		for key, value := range item.Attributes {
			if !estimate.addString(key) || !estimate.addString(value) {
				return estimate.used, false
			}
		}
		for _, group := range item.IncompatibleGroups {
			if !estimate.addString(group) {
				return estimate.used, false
			}
		}
	}
	for _, container := range containers {
		if !estimate.addString(container.ID) || !estimate.add(uint64(len(container.AllowedClasses)), 16) ||
			!estimate.add(uint64(len(container.Reserved)), 64) {
			return estimate.used, false
		}
		for _, class := range container.AllowedClasses {
			if !estimate.addString(class) {
				return estimate.used, false
			}
		}
	}
	return estimate.used, true
}

func domainMemoryWithin(items []Item, containers []ContainerType, limit uint64) bool {
	estimate := memoryEstimate{limit: limit}
	if !estimate.add(uint64(len(items)), 256) || !estimate.add(uint64(len(containers)), 256) {
		return false
	}
	for _, item := range items {
		spec := item.spec
		if !estimate.addString(spec.ID) || !estimate.addString(spec.SKU) || !estimate.addString(spec.Group) ||
			!estimate.add(uint64(len(spec.Orientations)), 16) || !estimate.add(uint64(len(spec.IncompatibleGroups)), 16) ||
			!estimate.add(uint64(len(spec.Attributes)), 64) {
			return false
		}
		for key, value := range spec.Attributes {
			if !estimate.addString(key) || !estimate.addString(value) {
				return false
			}
		}
		for _, group := range spec.IncompatibleGroups {
			if !estimate.addString(group) {
				return false
			}
		}
	}
	for _, container := range containers {
		spec := container.spec
		if !estimate.addString(spec.ID) || !estimate.add(uint64(len(spec.AllowedClasses)), 16) ||
			!estimate.add(uint64(len(spec.Reserved)), 64) {
			return false
		}
		for _, class := range spec.AllowedClasses {
			if !estimate.addString(class) {
				return false
			}
		}
	}
	return true
}

// Normalized returns a deep defensive copy of canonical lattice input.
func (r Request) Normalized() NormalizedRequest { return r.normalized.clone() }

func normalizeItem(item Item, resolution Resolution) (NormalizedItem, error) {
	dims, err := normalizeDimensions(item.Dimensions(), resolution.Length)
	if err != nil {
		return NormalizedItem{}, &FieldError{Category: category(err, ErrInvalidItem), ID: item.ID(), Field: "dimensions", Reason: err.Error()}
	}
	weight, err := latticeValue(item.Weight(), resolution.Mass)
	if err != nil || weight <= 0 {
		return NormalizedItem{}, &FieldError{Category: category(err, ErrInvalidItem), ID: item.ID(), Field: "weight", Reason: reason(err)}
	}
	result := NormalizedItem{ID: item.ID(), SKU: item.SKU(), Dimensions: dims, Weight: weight,
		Orientations: item.Orientations(), Attributes: item.Attributes(), FragileTop: item.FragileTop(),
		MinimumSupportPPM: item.MinimumSupportPPM(), Group: item.Group(), IncompatibleGroups: item.IncompatibleGroups(),
		MaxStackCount: item.MaxStackCount(), Priority: item.Priority()}
	if maximum := item.MaxSupportedWeight(); maximum != nil {
		value, conversionErr := latticeValue(*maximum, resolution.Mass)
		if conversionErr != nil || value < 0 {
			return NormalizedItem{}, &FieldError{Category: category(conversionErr, ErrInvalidItem), ID: item.ID(), Field: "max_supported_weight", Reason: reason(conversionErr)}
		}
		result.MaxSupportedWeight = &value
	}
	return result, nil
}

func normalizeContainer(container ContainerType, resolution Resolution) (NormalizedContainer, error) {
	dims, err := normalizeDimensions(container.InternalDimensions(), resolution.Length)
	if err != nil {
		return NormalizedContainer{}, &FieldError{Category: category(err, ErrInvalidContainer), ID: container.ID(), Field: "dimensions", Reason: err.Error()}
	}
	weight, err := latticeValue(container.MaxContentWeight(), resolution.Mass)
	if err != nil || weight <= 0 {
		return NormalizedContainer{}, &FieldError{Category: category(err, ErrInvalidContainer), ID: container.ID(), Field: "max_content_weight", Reason: reason(err)}
	}
	result := NormalizedContainer{ID: container.ID(), Dimensions: dims, MaxContentWeight: weight, Stock: container.Stock(), Priority: container.Priority(), MaxItemCount: container.MaxItemCount(), AllowedClasses: container.AllowedClasses(), CenterOfGravity: container.CenterOfGravity()}
	if tare := container.TareWeight(); tare != nil {
		result.TareWeight, err = latticeValue(*tare, resolution.Mass)
		if err != nil || result.TareWeight < 0 {
			return NormalizedContainer{}, &FieldError{Category: category(err, ErrInvalidContainer), ID: container.ID(), Field: "tare_weight", Reason: reason(err)}
		}
	}
	if gross := container.MaxGrossWeight(); gross != nil {
		result.MaxGrossWeight, err = latticeValue(*gross, resolution.Mass)
		if err != nil || result.MaxGrossWeight <= 0 || result.MaxGrossWeight < result.TareWeight {
			return NormalizedContainer{}, &FieldError{Category: category(err, ErrInvalidContainer), ID: container.ID(), Field: "max_gross_weight", Reason: reason(err)}
		}
		result.HasGrossWeight = true
	}
	for _, reserved := range container.Reserved() {
		d, conversionErr := normalizeDimensions(reserved.Dimensions, resolution.Length)
		if conversionErr != nil {
			return NormalizedContainer{}, conversionErr
		}
		box, boxErr := geometry.NewCuboid(reserved.Origin, d)
		if boxErr != nil {
			return NormalizedContainer{}, &FieldError{Category: category(boxErr, ErrInvalidContainer), ID: container.ID(), Field: "reserved", Reason: boxErr.Error()}
		}
		outer, _ := geometry.NewCuboid(geometry.Point{}, dims)
		if !outer.Contains(box) {
			return NormalizedContainer{}, &FieldError{Category: ErrInvalidContainer, ID: container.ID(), Field: "reserved", Reason: "outside usable space"}
		}
		result.Reserved = append(result.Reserved, box)
	}
	return result, nil
}

func normalizeDimensions(d PhysicalDimensions, resolution measurement.Quantity) (geometry.Dimensions, error) {
	x, err := latticeValue(d.X, resolution)
	if err != nil {
		return geometry.Dimensions{}, err
	}
	y, err := latticeValue(d.Y, resolution)
	if err != nil {
		return geometry.Dimensions{}, err
	}
	z, err := latticeValue(d.Z, resolution)
	if err != nil {
		return geometry.Dimensions{}, err
	}
	dims := geometry.Dimensions{X: x, Y: y, Z: z}
	if !dims.Valid() {
		return geometry.Dimensions{}, ErrInvalidRequest
	}
	if _, err = dims.Volume(); err != nil {
		return geometry.Dimensions{}, category(err, ErrInvalidRequest)
	}
	return dims, nil
}

func positiveResolution(q measurement.Quantity) error {
	ratio := q.Amount().BigRat()
	if ratio.Sign() <= 0 {
		return ErrInvalidRequest
	}
	return nil
}

func latticeValue(value, resolution measurement.Quantity) (int64, error) {
	converted, err := value.Convert(resolution.Unit(), measurement.ExactConversion())
	if err != nil {
		return 0, err
	}
	denominator := resolution.Amount().BigRat()
	if denominator.Sign() <= 0 {
		return 0, ErrInvalidRequest
	}
	ratio := new(big.Rat).Quo(converted.Amount().BigRat(), denominator)
	if !ratio.IsInt() {
		return 0, ErrInexactResolution
	}
	numerator := ratio.Num()
	if !numerator.IsInt64() {
		return 0, ErrOverflow
	}
	result := numerator.Int64()
	if result == math.MinInt64 {
		return 0, ErrOverflow
	}
	return result, nil
}

func category(err, fallback error) error {
	if errors.Is(err, ErrInexactResolution) || errors.Is(err, ErrOverflow) || errors.Is(err, geometry.ErrOverflow) {
		if errors.Is(err, geometry.ErrOverflow) {
			return ErrOverflow
		}
		return err
	}
	return fallback
}
func reason(err error) string {
	if err == nil {
		return "must be positive"
	}
	return err.Error()
}

func (r NormalizedRequest) clone() NormalizedRequest {
	r.items = cloneNormalizedItems(r.items)
	r.containers = cloneNormalizedContainers(r.containers)
	return r
}
func cloneNormalizedItems(source []NormalizedItem) []NormalizedItem {
	result := slices.Clone(source)
	for index := range result {
		result[index].Orientations = slices.Clone(result[index].Orientations)
		result[index].Attributes = cloneMap(result[index].Attributes)
		result[index].IncompatibleGroups = slices.Clone(result[index].IncompatibleGroups)
		if result[index].MaxSupportedWeight != nil {
			copy := *result[index].MaxSupportedWeight
			result[index].MaxSupportedWeight = &copy
		}
	}
	return result
}
func cloneNormalizedContainers(source []NormalizedContainer) []NormalizedContainer {
	result := slices.Clone(source)
	for index := range result {
		result[index].AllowedClasses = slices.Clone(result[index].AllowedClasses)
		result[index].Reserved = slices.Clone(result[index].Reserved)
		result[index].CenterOfGravity = cloneCenterOfGravityBounds(result[index].CenterOfGravity)
	}
	return result
}
