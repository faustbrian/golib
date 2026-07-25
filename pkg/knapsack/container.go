package knapsack

import (
	"slices"
	"strings"

	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/measurement"
)

// Stock explicitly distinguishes finite stock from unlimited availability.
type Stock struct {
	unlimited bool
	count     uint32
}

// UnlimitedStock constructs explicitly unbounded container availability.
func UnlimitedStock() Stock { return Stock{unlimited: true} }

// FiniteStock constructs availability of count containers; constructors reject
// a zero finite count.
func FiniteStock(count uint32) Stock { return Stock{count: count} }

// Unlimited reports whether Count is inapplicable.
func (s Stock) Unlimited() bool { return s.unlimited }

// Count returns finite availability, or zero for unlimited stock.
func (s Stock) Count() uint32 { return s.count }

// ReservedRegion is expressed in the request lattice after normalization.
type ReservedRegion struct {
	// Origin is the non-negative lower lattice corner.
	Origin geometry.Point
	// Dimensions are exact positive physical lengths.
	Dimensions PhysicalDimensions
}

// CenterOfGravityBounds constrains the combined content center of gravity on
// each container axis. Values are inclusive millionths of the internal axis
// length. Item mass is modeled at the geometric center of a uniform cuboid.
type CenterOfGravityBounds struct {
	// MinXPPM is the inclusive horizontal X-axis lower bound.
	MinXPPM uint32 `json:"min_x_ppm"`
	// MaxXPPM is the inclusive horizontal X-axis upper bound.
	MaxXPPM uint32 `json:"max_x_ppm"`
	// MinYPPM is the inclusive horizontal Y-axis lower bound.
	MinYPPM uint32 `json:"min_y_ppm"`
	// MaxYPPM is the inclusive horizontal Y-axis upper bound.
	MaxYPPM uint32 `json:"max_y_ppm"`
	// MinZPPM is the inclusive vertical Z-axis lower bound.
	MinZPPM uint32 `json:"min_z_ppm"`
	// MaxZPPM is the inclusive vertical Z-axis upper bound.
	MaxZPPM uint32 `json:"max_z_ppm"`
}

func (b CenterOfGravityBounds) valid() bool {
	return b.MaxXPPM <= 1_000_000 && b.MaxYPPM <= 1_000_000 && b.MaxZPPM <= 1_000_000 &&
		b.MinXPPM <= b.MaxXPPM && b.MinYPPM <= b.MaxYPPM && b.MinZPPM <= b.MaxZPPM
}

func cloneCenterOfGravityBounds(value *CenterOfGravityBounds) *CenterOfGravityBounds {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// ContainerTypeSpec defines one validated reusable container type. New values
// own defensive copies of all pointer and slice fields.
type ContainerTypeSpec struct {
	// ID is the required unique type identity.
	ID string
	// InternalDimensions are the exact usable physical lengths.
	InternalDimensions PhysicalDimensions
	// ExternalDimensions optionally describe non-feasibility shipping size.
	ExternalDimensions *PhysicalDimensions
	// MaxContentWeight is the exact positive content mass limit.
	MaxContentWeight measurement.Quantity
	// TareWeight is the optional non-negative empty-container mass.
	TareWeight *measurement.Quantity
	// MaxGrossWeight optionally limits tare plus content mass.
	MaxGrossWeight *measurement.Quantity
	// Stock explicitly selects finite or unlimited availability.
	Stock Stock
	// Priority is the deterministic type ordering after objective equivalence.
	Priority int64
	// MaxItemCount limits placements per instance; zero means unrestricted.
	MaxItemCount uint32
	// AllowedClasses is a copied canonical allowlist for item class attributes.
	AllowedClasses []string
	// Reserved is a copied set of unusable cuboids inside internal dimensions.
	Reserved []ReservedRegion
	// CenterOfGravity optionally constrains the combined content mass center.
	CenterOfGravity *CenterOfGravityBounds
}

// ContainerType is immutable from the caller's perspective.
type ContainerType struct{ spec ContainerTypeSpec }

// NewContainerType validates dimensions, mass limits, stock, and reserved
// regions, then defensively copies caller-owned state.
func NewContainerType(spec ContainerTypeSpec) (ContainerType, error) {
	if strings.TrimSpace(spec.ID) == "" {
		return ContainerType{}, &FieldError{Category: ErrInvalidContainer, Field: "id", Reason: "must be non-empty"}
	}
	if !validPhysicalDimensions(spec.InternalDimensions) {
		return ContainerType{}, &FieldError{Category: ErrInvalidContainer, ID: spec.ID, Field: "internal_dimensions", Reason: "must be positive lengths"}
	}
	if spec.ExternalDimensions != nil && !validPhysicalDimensions(*spec.ExternalDimensions) {
		return ContainerType{}, &FieldError{Category: ErrInvalidContainer, ID: spec.ID, Field: "external_dimensions", Reason: "must be positive lengths"}
	}
	if !validPhysicalQuantity(spec.MaxContentWeight, measurement.MassDimension, false) {
		return ContainerType{}, &FieldError{Category: ErrInvalidContainer, ID: spec.ID, Field: "max_content_weight", Reason: "must be a positive mass"}
	}
	if spec.TareWeight != nil && !validPhysicalQuantity(*spec.TareWeight, measurement.MassDimension, true) {
		return ContainerType{}, &FieldError{Category: ErrInvalidContainer, ID: spec.ID, Field: "tare_weight", Reason: "must be a non-negative mass"}
	}
	if spec.MaxGrossWeight != nil && !validPhysicalQuantity(*spec.MaxGrossWeight, measurement.MassDimension, false) {
		return ContainerType{}, &FieldError{Category: ErrInvalidContainer, ID: spec.ID, Field: "max_gross_weight", Reason: "must be a positive mass"}
	}
	if spec.TareWeight != nil && spec.MaxGrossWeight != nil {
		gross, err := spec.MaxGrossWeight.Convert(spec.TareWeight.Unit(), measurement.ExactConversion())
		if err != nil || gross.Amount().BigRat().Cmp(spec.TareWeight.Amount().BigRat()) < 0 {
			return ContainerType{}, &FieldError{Category: ErrInvalidContainer, ID: spec.ID, Field: "max_gross_weight", Reason: "must not be less than tare weight"}
		}
	}
	for _, reserved := range spec.Reserved {
		if reserved.Origin.X < 0 || reserved.Origin.Y < 0 || reserved.Origin.Z < 0 || !validPhysicalDimensions(reserved.Dimensions) {
			return ContainerType{}, &FieldError{Category: ErrInvalidContainer, ID: spec.ID, Field: "reserved", Reason: "must have a non-negative origin and positive length dimensions"}
		}
	}
	if spec.CenterOfGravity != nil && !spec.CenterOfGravity.valid() {
		return ContainerType{}, &FieldError{Category: ErrInvalidContainer, ID: spec.ID, Field: "center_of_gravity", Reason: "bounds must be ordered millionths from 0 through 1000000"}
	}
	if !spec.Stock.unlimited && spec.Stock.count == 0 {
		return ContainerType{}, &FieldError{Category: ErrInvalidContainer, ID: spec.ID, Field: "stock", Reason: "finite stock must be positive"}
	}
	spec.AllowedClasses = slices.Clone(spec.AllowedClasses)
	slices.Sort(spec.AllowedClasses)
	spec.AllowedClasses = slices.Compact(spec.AllowedClasses)
	spec.Reserved = slices.Clone(spec.Reserved)
	if spec.ExternalDimensions != nil {
		copy := *spec.ExternalDimensions
		spec.ExternalDimensions = &copy
	}
	spec.TareWeight = cloneQuantity(spec.TareWeight)
	spec.MaxGrossWeight = cloneQuantity(spec.MaxGrossWeight)
	spec.CenterOfGravity = cloneCenterOfGravityBounds(spec.CenterOfGravity)
	return ContainerType{spec: spec}, nil
}

// ID returns the unique container type identity.
func (c ContainerType) ID() string { return c.spec.ID }

// InternalDimensions returns exact usable physical dimensions.
func (c ContainerType) InternalDimensions() PhysicalDimensions { return c.spec.InternalDimensions }

// MaxContentWeight returns the exact content mass limit.
func (c ContainerType) MaxContentWeight() measurement.Quantity { return c.spec.MaxContentWeight }

// Stock returns finite or unlimited availability.
func (c ContainerType) Stock() Stock { return c.spec.Stock }

// Priority returns the deterministic type priority.
func (c ContainerType) Priority() int64 { return c.spec.Priority }

// MaxItemCount returns the per-instance count limit, or zero when unrestricted.
func (c ContainerType) MaxItemCount() uint32 { return c.spec.MaxItemCount }

// AllowedClasses returns a defensive copy in canonical order.
func (c ContainerType) AllowedClasses() []string { return slices.Clone(c.spec.AllowedClasses) }

// Reserved returns a defensive copy of unusable regions.
func (c ContainerType) Reserved() []ReservedRegion { return slices.Clone(c.spec.Reserved) }

// CenterOfGravity returns a copy of the optional content-mass bounds.
func (c ContainerType) CenterOfGravity() *CenterOfGravityBounds {
	return cloneCenterOfGravityBounds(c.spec.CenterOfGravity)
}

// TareWeight returns a copy of the optional exact empty-container mass.
func (c ContainerType) TareWeight() *measurement.Quantity { return cloneQuantity(c.spec.TareWeight) }

// MaxGrossWeight returns a copy of the optional tare-plus-content mass limit.
func (c ContainerType) MaxGrossWeight() *measurement.Quantity {
	return cloneQuantity(c.spec.MaxGrossWeight)
}
