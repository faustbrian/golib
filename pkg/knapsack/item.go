package knapsack

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/measurement"
)

// PhysicalDimensions are exact public-boundary lengths on named axes.
type PhysicalDimensions struct {
	// X is the exact physical width.
	X measurement.Quantity
	// Y is the exact physical depth.
	Y measurement.Quantity
	// Z is the exact physical height.
	Z measurement.Quantity
}

// ItemSpec is copied and validated by NewItem.
type ItemSpec struct {
	// ID is the required unique logical item identity.
	ID string
	// SKU is optional caller metadata and does not establish identity.
	SKU string
	// Dimensions are exact positive physical lengths.
	Dimensions PhysicalDimensions
	// Weight is the exact positive item mass.
	Weight measurement.Quantity
	// Orientations is the nonempty allowed physical-axis permutation set.
	Orientations []geometry.Orientation
	// Attributes is copied caller metadata used by eligibility constraints.
	Attributes map[string]string
	// FragileTop forbids any item from using this item as a supporter.
	FragileTop bool
	// MaxSupportedWeight is the optional non-negative total transitive load.
	MaxSupportedWeight *measurement.Quantity
	// MinimumSupportPPM is the required supported footprint in millionths.
	MinimumSupportPPM uint32
	// Group requires all same-group items to share one container.
	Group string
	// IncompatibleGroups may not share a container with this item.
	IncompatibleGroups []string
	// MaxStackCount bounds the number of supported levels above this item; zero
	// means no stack-depth constraint.
	MaxStackCount uint32
	// Priority is an exact caller value used by PackedPriority objectives.
	Priority int64
}

// Item is immutable from the caller's perspective.
type Item struct{ spec ItemSpec }

// NewItem validates the domain model and defensively copies all collections.
func NewItem(spec ItemSpec) (Item, error) {
	if strings.TrimSpace(spec.ID) == "" {
		return Item{}, &FieldError{Category: ErrInvalidItem, Field: "id", Reason: "must be non-empty"}
	}
	if !validPhysicalDimensions(spec.Dimensions) {
		return Item{}, &FieldError{Category: ErrInvalidItem, ID: spec.ID, Field: "dimensions", Reason: "must be positive lengths"}
	}
	if !validPhysicalQuantity(spec.Weight, measurement.MassDimension, false) {
		return Item{}, &FieldError{Category: ErrInvalidItem, ID: spec.ID, Field: "weight", Reason: "must be a positive mass"}
	}
	if spec.MaxSupportedWeight != nil && !validPhysicalQuantity(*spec.MaxSupportedWeight, measurement.MassDimension, true) {
		return Item{}, &FieldError{Category: ErrInvalidItem, ID: spec.ID, Field: "max_supported_weight", Reason: "must be a non-negative mass"}
	}
	if len(spec.Orientations) == 0 {
		return Item{}, &FieldError{Category: ErrInvalidItem, ID: spec.ID, Field: "orientations", Reason: "must be non-empty"}
	}
	seen := make(map[geometry.Orientation]struct{}, len(spec.Orientations))
	for _, orientation := range spec.Orientations {
		if _, err := orientation.Apply(geometry.Dimensions{X: 1, Y: 2, Z: 3}); err != nil {
			return Item{}, &FieldError{Category: ErrInvalidItem, ID: spec.ID, Field: "orientations", Reason: err.Error()}
		}
		if _, duplicate := seen[orientation]; duplicate {
			return Item{}, &FieldError{Category: ErrInvalidItem, ID: spec.ID, Field: "orientations", Reason: "duplicate"}
		}
		seen[orientation] = struct{}{}
	}
	if spec.MinimumSupportPPM > 1_000_000 {
		return Item{}, &FieldError{Category: ErrInvalidItem, ID: spec.ID, Field: "minimum_support_ppm", Reason: "exceeds 1000000"}
	}
	spec.Orientations = slices.Clone(spec.Orientations)
	slices.Sort(spec.IncompatibleGroups)
	spec.IncompatibleGroups = slices.Compact(slices.Clone(spec.IncompatibleGroups))
	spec.Attributes = cloneMap(spec.Attributes)
	return Item{spec: spec}, nil
}

// ID returns the unique item identity.
func (i Item) ID() string { return i.spec.ID }

// SKU returns optional non-identity metadata.
func (i Item) SKU() string { return i.spec.SKU }

// Dimensions returns exact physical dimensions.
func (i Item) Dimensions() PhysicalDimensions { return i.spec.Dimensions }

// Weight returns the exact physical mass.
func (i Item) Weight() measurement.Quantity { return i.spec.Weight }

// Orientations returns a defensive copy of allowed permutations.
func (i Item) Orientations() []geometry.Orientation { return slices.Clone(i.spec.Orientations) }

// Attributes returns a defensive copy of caller metadata.
func (i Item) Attributes() map[string]string { return cloneMap(i.spec.Attributes) }

// FragileTop reports whether the item may bear any other item.
func (i Item) FragileTop() bool { return i.spec.FragileTop }

// MinimumSupportPPM returns the required footprint support in millionths.
func (i Item) MinimumSupportPPM() uint32 { return i.spec.MinimumSupportPPM }

// Group returns the required co-location group.
func (i Item) Group() string { return i.spec.Group }

// IncompatibleGroups returns a defensive copy in canonical order.
func (i Item) IncompatibleGroups() []string { return slices.Clone(i.spec.IncompatibleGroups) }

// MaxStackCount returns the allowed levels above, or zero when unrestricted.
func (i Item) MaxStackCount() uint32 { return i.spec.MaxStackCount }

// Priority returns the exact subset-selection priority.
func (i Item) Priority() int64 { return i.spec.Priority }

// MaxSupportedWeight returns a copy of the optional exact load limit.
func (i Item) MaxSupportedWeight() *measurement.Quantity {
	return cloneQuantity(i.spec.MaxSupportedWeight)
}

// ExpandQuantity creates stable instance IDs without retaining caller aliases.
func ExpandQuantity(spec ItemSpec, quantity uint32) ([]Item, error) {
	return ExpandQuantityWithLimit(spec, quantity, DefaultLimits().MaxItems)
}

// ExpandQuantityWithLimit expands at most maximum instances and rejects the
// request before allocating when the caller's quantity budget would be
// exceeded.
func ExpandQuantityWithLimit(spec ItemSpec, quantity, maximum uint32) ([]Item, error) {
	if quantity == 0 {
		return nil, &FieldError{Category: ErrInvalidItem, Field: "quantity", Reason: "must be positive"}
	}
	if maximum == 0 {
		return nil, &FieldError{Category: ErrInvalidOptions, Field: "maximum", Reason: "must be positive"}
	}
	if quantity > maximum {
		return nil, &FieldError{Category: ErrBudgetExhausted, Field: "quantity", Reason: "exceeds expansion limit"}
	}
	items := make([]Item, quantity)
	for index := range quantity {
		instance := spec
		instance.ID = fmt.Sprintf("%s#%06d", spec.ID, index+1)
		item, err := NewItem(instance)
		if err != nil {
			return nil, err
		}
		items[index] = item
	}
	return items, nil
}

func validPhysicalDimensions(dimensions PhysicalDimensions) bool {
	return validPhysicalQuantity(dimensions.X, measurement.LengthDimension, false) &&
		validPhysicalQuantity(dimensions.Y, measurement.LengthDimension, false) &&
		validPhysicalQuantity(dimensions.Z, measurement.LengthDimension, false)
}

func validPhysicalQuantity(quantity measurement.Quantity, dimension measurement.Dimension, allowZero bool) bool {
	actual, err := quantity.Dimension()
	if err != nil || actual != dimension {
		return false
	}
	sign := quantity.Amount().BigRat().Sign()
	return sign > 0 || allowZero && sign == 0
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	maps.Copy(result, source)
	return result
}

func cloneQuantity(value *measurement.Quantity) *measurement.Quantity {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
