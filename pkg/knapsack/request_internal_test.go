package knapsack

import (
	"errors"
	"math"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

func internalQuantity(value string, unit measurement.Unit) measurement.Quantity {
	return measurement.MustNew(decimal.MustParse(value), unit)
}

func validNormalizedSpec() NormalizedSpec {
	return NormalizedSpec{
		Items: []NormalizedItem{{
			ID: "item", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1},
			Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ},
		}},
		Containers: []NormalizedContainer{{
			ID: "box", Dimensions: geometry.Dimensions{X: 2, Y: 2, Z: 2},
			MaxContentWeight: 2, Stock: UnlimitedStock(),
		}},
		Resolution: Resolution{
			Length: internalQuantity("1", measurement.Metre),
			Mass:   internalQuantity("1", measurement.Kilogram),
		},
		Limits: DefaultLimits(),
	}
}

func cloneNormalizedSpec(source NormalizedSpec) NormalizedSpec {
	result := source
	result.Items = cloneNormalizedItems(source.Items)
	result.Containers = cloneNormalizedContainers(source.Containers)
	return result
}

func TestNormalizedRequestValidatesAndCopiesCenterOfGravityBounds(t *testing.T) {
	t.Parallel()

	spec := validNormalizedSpec()
	bounds := CenterOfGravityBounds{
		MinXPPM: 100_000, MaxXPPM: 900_000,
		MinYPPM: 200_000, MaxYPPM: 800_000,
		MinZPPM: 300_000, MaxZPPM: 700_000,
	}
	spec.Containers[0].CenterOfGravity = &bounds
	request, err := NewNormalizedRequest(spec)
	if err != nil {
		t.Fatal(err)
	}
	bounds.MinXPPM = 900_001
	got := request.Containers()[0].CenterOfGravity
	if got == nil || got.MinXPPM != 100_000 {
		t.Fatalf("center-of-gravity bounds alias caller state: %+v", got)
	}
	got.MinXPPM = 999_999
	if request.Containers()[0].CenterOfGravity.MinXPPM != 100_000 {
		t.Fatal("center-of-gravity accessor returned an internal pointer")
	}

	invalid := validNormalizedSpec()
	invalid.Containers[0].CenterOfGravity = &CenterOfGravityBounds{MaxXPPM: 1_000_001}
	if _, err := NewNormalizedRequest(invalid); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("out-of-range bounds error = %v", err)
	}
	invalid = validNormalizedSpec()
	invalid.Containers[0].CenterOfGravity = &CenterOfGravityBounds{MinXPPM: 500_001, MaxXPPM: 500_000}
	if _, err := NewNormalizedRequest(invalid); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("inverted bounds error = %v", err)
	}
}

func TestNormalizedRequestRejectsEveryInvalidBoundary(t *testing.T) {
	t.Parallel()
	base := validNormalizedSpec()
	negative := int64(-1)
	outOfBounds, _ := geometry.NewCuboid(geometry.Point{X: 2}, geometry.Dimensions{X: 1, Y: 1, Z: 1})
	tests := []struct {
		name   string
		mutate func(*NormalizedSpec)
		target error
	}{
		{"empty items", func(s *NormalizedSpec) { s.Items = nil }, ErrInvalidRequest},
		{"memory", func(s *NormalizedSpec) { s.Limits.MaxMemoryBytes = 1 }, ErrMemoryBudgetExhausted},
		{"length dimension", func(s *NormalizedSpec) { s.Resolution.Length = internalQuantity("1", measurement.Kilogram) }, ErrInvalidRequest},
		{"zero length", func(s *NormalizedSpec) { s.Resolution.Length = internalQuantity("0", measurement.Metre) }, ErrInvalidRequest},
		{"zero mass", func(s *NormalizedSpec) { s.Resolution.Mass = internalQuantity("0", measurement.Kilogram) }, ErrInvalidRequest},
		{"item ID", func(s *NormalizedSpec) { s.Items[0].ID = "" }, ErrInvalidItem},
		{"item dimensions", func(s *NormalizedSpec) { s.Items[0].Dimensions.X = 0 }, ErrInvalidItem},
		{"item weight", func(s *NormalizedSpec) { s.Items[0].Weight = 0 }, ErrInvalidItem},
		{"item orientations", func(s *NormalizedSpec) { s.Items[0].Orientations = nil }, ErrInvalidItem},
		{"support ratio", func(s *NormalizedSpec) { s.Items[0].MinimumSupportPPM = 1_000_001 }, ErrInvalidItem},
		{"item volume overflow", func(s *NormalizedSpec) { s.Items[0].Dimensions = geometry.Dimensions{X: math.MaxInt64, Y: 2, Z: 1} }, ErrOverflow},
		{"duplicate item", func(s *NormalizedSpec) { s.Items = append(s.Items, s.Items[0]) }, ErrDuplicateID},
		{"invalid orientation", func(s *NormalizedSpec) { s.Items[0].Orientations[0] = "invalid" }, ErrInvalidItem},
		{"duplicate orientation", func(s *NormalizedSpec) {
			s.Items[0].Orientations = append(s.Items[0].Orientations, geometry.OrientationXYZ)
		}, ErrInvalidItem},
		{"negative support weight", func(s *NormalizedSpec) { s.Items[0].MaxSupportedWeight = &negative }, ErrInvalidItem},
		{"container ID", func(s *NormalizedSpec) { s.Containers[0].ID = "" }, ErrInvalidContainer},
		{"container dimensions", func(s *NormalizedSpec) { s.Containers[0].Dimensions.X = 0 }, ErrInvalidContainer},
		{"container weight", func(s *NormalizedSpec) { s.Containers[0].MaxContentWeight = 0 }, ErrInvalidContainer},
		{"container stock", func(s *NormalizedSpec) { s.Containers[0].Stock = FiniteStock(0) }, ErrInvalidContainer},
		{"container volume overflow", func(s *NormalizedSpec) {
			s.Containers[0].Dimensions = geometry.Dimensions{X: math.MaxInt64, Y: 2, Z: 1}
		}, ErrOverflow},
		{"duplicate container", func(s *NormalizedSpec) { s.Containers = append(s.Containers, s.Containers[0]) }, ErrDuplicateID},
		{"reserved outside", func(s *NormalizedSpec) { s.Containers[0].Reserved = []geometry.Cuboid{outOfBounds} }, ErrInvalidContainer},
		{"negative tare", func(s *NormalizedSpec) { s.Containers[0].TareWeight = -1 }, ErrInvalidContainer},
		{"gross below tare", func(s *NormalizedSpec) {
			s.Containers[0].TareWeight, s.Containers[0].MaxGrossWeight, s.Containers[0].HasGrossWeight = 2, 1, true
		}, ErrInvalidContainer},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			spec := cloneNormalizedSpec(base)
			test.mutate(&spec)
			if _, err := NewNormalizedRequest(spec); !errors.Is(err, test.target) {
				t.Fatalf("error = %v, want %v", err, test.target)
			}
		})
	}
}

func TestLimitsAndExactLatticeFailures(t *testing.T) {
	t.Parallel()
	limits := DefaultLimits()
	invalidLimits := []func(*Limits){
		func(l *Limits) { l.MaxItems = 0 },
		func(l *Limits) { l.MaxContainerTypes = 0 },
		func(l *Limits) { l.MaxOrientations = 0 },
		func(l *Limits) { l.MaxCandidatePlacements = 0 },
		func(l *Limits) { l.MaxSearchNodes = 0 },
		func(l *Limits) { l.MaxBranches = 0 },
		func(l *Limits) { l.MaxMemoryBytes = 0 },
		func(l *Limits) { l.MaxIDBytes = 0 },
		func(l *Limits) { l.MaxDiagnostics = 0 },
	}
	for index, mutate := range invalidLimits {
		candidate := limits
		mutate(&candidate)
		if candidate.Valid() {
			t.Fatalf("invalid limits %d accepted", index)
		}
	}
	if _, err := latticeValue(internalQuantity("1", measurement.Metre), internalQuantity("1", measurement.Kilogram)); err == nil {
		t.Fatal("mixed dimensions accepted")
	}
	if _, err := latticeValue(internalQuantity("1.5", measurement.Metre), internalQuantity("1", measurement.Metre)); !errors.Is(err, ErrInexactResolution) {
		t.Fatalf("inexact error = %v", err)
	}
	if _, err := latticeValue(internalQuantity("9223372036854775808", measurement.Metre), internalQuantity("1", measurement.Metre)); !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow error = %v", err)
	}
	if _, err := latticeValue(internalQuantity("-9223372036854775808", measurement.Metre), internalQuantity("1", measurement.Metre)); !errors.Is(err, ErrOverflow) {
		t.Fatalf("minimum error = %v", err)
	}
	if _, err := latticeValue(internalQuantity("1", measurement.Metre), internalQuantity("0", measurement.Metre)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero denominator error = %v", err)
	}
	if got := reason(errors.New("specific")); got != "specific" {
		t.Fatalf("reason = %q", got)
	}
	if got := reason(nil); got != "must be positive" {
		t.Fatalf("nil reason = %q", got)
	}
	for axis, dimensions := range map[string]PhysicalDimensions{
		"x": {X: internalQuantity("0", measurement.Metre), Y: internalQuantity("1", measurement.Metre), Z: internalQuantity("1", measurement.Metre)},
		"y": {X: internalQuantity("1", measurement.Metre), Y: internalQuantity("0", measurement.Metre), Z: internalQuantity("1", measurement.Metre)},
		"z": {X: internalQuantity("1", measurement.Metre), Y: internalQuantity("1", measurement.Metre), Z: internalQuantity("0", measurement.Metre)},
	} {
		if _, err := normalizeDimensions(dimensions, internalQuantity("1", measurement.Metre)); err == nil {
			t.Fatalf("%s dimension accepted", axis)
		}
	}
	for axis, dimensions := range map[string]PhysicalDimensions{
		"x": {X: internalQuantity("1", measurement.Kilogram), Y: internalQuantity("1", measurement.Metre), Z: internalQuantity("1", measurement.Metre)},
		"y": {X: internalQuantity("1", measurement.Metre), Y: internalQuantity("1", measurement.Kilogram), Z: internalQuantity("1", measurement.Metre)},
		"z": {X: internalQuantity("1", measurement.Metre), Y: internalQuantity("1", measurement.Metre), Z: internalQuantity("1", measurement.Kilogram)},
	} {
		if _, err := normalizeDimensions(dimensions, internalQuantity("1", measurement.Metre)); err == nil {
			t.Fatalf("mixed %s dimension accepted", axis)
		}
	}
	if _, err := normalizeDimensions(PhysicalDimensions{X: internalQuantity("4611686018427387904", measurement.Metre), Y: internalQuantity("1", measurement.Metre), Z: internalQuantity("3", measurement.Metre)}, internalQuantity("1", measurement.Metre)); !errors.Is(err, ErrOverflow) {
		t.Fatalf("volume overflow error = %v", err)
	}
}

func TestDomainConstructorsRejectInvalidAndCloneOptionalState(t *testing.T) {
	t.Parallel()
	base := ItemSpec{ID: "item", Dimensions: PhysicalDimensions{X: internalQuantity("1", measurement.Metre), Y: internalQuantity("1", measurement.Metre), Z: internalQuantity("1", measurement.Metre)}, Weight: internalQuantity("1", measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}}
	for name, mutate := range map[string]func(*ItemSpec){
		"id":                    func(s *ItemSpec) { s.ID = "" },
		"orientations":          func(s *ItemSpec) { s.Orientations = nil },
		"invalid orientation":   func(s *ItemSpec) { s.Orientations[0] = "invalid" },
		"duplicate orientation": func(s *ItemSpec) { s.Orientations = append(s.Orientations, geometry.OrientationXYZ) },
		"support":               func(s *ItemSpec) { s.MinimumSupportPPM = 1_000_001 },
	} {
		spec := base
		spec.Orientations = append([]geometry.Orientation(nil), base.Orientations...)
		mutate(&spec)
		if _, err := NewItem(spec); !errors.Is(err, ErrInvalidItem) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	if _, err := NewContainerType(ContainerTypeSpec{}); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("container ID error = %v", err)
	}
	if _, err := NewContainerType(ContainerTypeSpec{ID: "box", InternalDimensions: base.Dimensions, MaxContentWeight: base.Weight, Stock: FiniteStock(0)}); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("container stock error = %v", err)
	}
	if _, err := NewContainerType(ContainerTypeSpec{
		ID: "box", InternalDimensions: base.Dimensions,
		MaxContentWeight: base.Weight, Stock: UnlimitedStock(),
		CenterOfGravity: &CenterOfGravityBounds{MinZPPM: 2, MaxZPPM: 1},
	}); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("center-of-gravity bounds error = %v", err)
	}
	external := PhysicalDimensions{
		X: internalQuantity("1", measurement.Metre),
		Y: internalQuantity("1", measurement.Metre),
		Z: internalQuantity("1", measurement.Metre),
	}
	bounds := CenterOfGravityBounds{MaxXPPM: 1_000_000, MaxYPPM: 1_000_000, MaxZPPM: 1_000_000}
	container, err := NewContainerType(ContainerTypeSpec{
		ID: "box", InternalDimensions: external,
		MaxContentWeight: internalQuantity("1", measurement.Kilogram),
		Stock:            UnlimitedStock(), ExternalDimensions: &external,
		CenterOfGravity: &bounds,
	})
	if err != nil {
		t.Fatal(err)
	}
	external.X = internalQuantity("2", measurement.Metre)
	if container.spec.ExternalDimensions.X.Amount().String() != "1" {
		t.Fatal("external dimensions alias caller state")
	}
	bounds.MaxXPPM = 1
	returnedBounds := container.CenterOfGravity()
	returnedBounds.MaxXPPM = 2
	if container.CenterOfGravity().MaxXPPM != 1_000_000 {
		t.Fatal("center-of-gravity bounds alias caller or accessor state")
	}
	field := (&FieldError{Category: ErrInvalidItem, ID: "item", Field: "weight", Reason: "bad"}).Error()
	if field != "knapsack: invalid item item field weight: bad" {
		t.Fatalf("field error = %q", field)
	}
}

func TestNewRequestRejectsDomainNormalizationFailures(t *testing.T) {
	t.Parallel()
	resolution := Resolution{Length: internalQuantity("1", measurement.Metre), Mass: internalQuantity("1", measurement.Kilogram)}
	validItem := ItemSpec{
		ID: "item", Dimensions: PhysicalDimensions{
			X: internalQuantity("1", measurement.Metre), Y: internalQuantity("1", measurement.Metre), Z: internalQuantity("1", measurement.Metre),
		},
		Weight: internalQuantity("1", measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ},
	}
	validContainer := ContainerTypeSpec{
		ID: "box", InternalDimensions: PhysicalDimensions{
			X: internalQuantity("2", measurement.Metre), Y: internalQuantity("2", measurement.Metre), Z: internalQuantity("2", measurement.Metre),
		},
		MaxContentWeight: internalQuantity("2", measurement.Kilogram), Stock: UnlimitedStock(),
	}
	makeInputs := func(itemSpec ItemSpec, containerSpec ContainerTypeSpec) ([]Item, []ContainerType) {
		item, itemErr := NewItem(itemSpec)
		container, containerErr := NewContainerType(containerSpec)
		if itemErr != nil || containerErr != nil {
			t.Fatalf("fixture errors: %v %v", itemErr, containerErr)
		}
		return []Item{item}, []ContainerType{container}
	}
	for _, test := range []struct {
		name       string
		item       ItemSpec
		container  ContainerTypeSpec
		resolution Resolution
		limits     Limits
		target     error
	}{
		{"inexact reserved dimension", validItem, func() ContainerTypeSpec {
			s := validContainer
			s.Reserved = []ReservedRegion{{Dimensions: PhysicalDimensions{X: internalQuantity("0.5", measurement.Metre), Y: internalQuantity("1", measurement.Metre), Z: internalQuantity("1", measurement.Metre)}}}
			return s
		}(), resolution, DefaultLimits(), ErrInexactResolution},
		{"reserved coordinate overflow", validItem, func() ContainerTypeSpec {
			s := validContainer
			s.Reserved = []ReservedRegion{{Origin: geometry.Point{X: math.MaxInt64}, Dimensions: PhysicalDimensions{X: internalQuantity("1", measurement.Metre), Y: internalQuantity("1", measurement.Metre), Z: internalQuantity("1", measurement.Metre)}}}
			return s
		}(), resolution, DefaultLimits(), ErrOverflow},
		{"reserved outside", validItem, func() ContainerTypeSpec {
			s := validContainer
			s.Reserved = []ReservedRegion{{Origin: geometry.Point{X: 2}, Dimensions: PhysicalDimensions{X: internalQuantity("1", measurement.Metre), Y: internalQuantity("1", measurement.Metre), Z: internalQuantity("1", measurement.Metre)}}}
			return s
		}(), resolution, DefaultLimits(), ErrInvalidContainer},
	} {
		t.Run(test.name, func(t *testing.T) {
			items, containers := makeInputs(test.item, test.container)
			if _, err := NewRequest(items, containers, test.resolution, test.limits); !errors.Is(err, test.target) {
				t.Fatalf("error = %v, want %v", err, test.target)
			}
		})
	}

	items, containers := makeInputs(validItem, validContainer)
	invalidLimits := DefaultLimits()
	invalidLimits.MaxItems = 0
	if _, err := NewRequest(items, containers, resolution, invalidLimits); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("limits error = %v", err)
	}
	if _, err := NewRequest(nil, containers, resolution, DefaultLimits()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty error = %v", err)
	}
	zeroResolution := resolution
	zeroResolution.Length = internalQuantity("0", measurement.Metre)
	if _, err := NewRequest(items, containers, zeroResolution, DefaultLimits()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("resolution error = %v", err)
	}
	zeroResolution = resolution
	zeroResolution.Mass = internalQuantity("0", measurement.Kilogram)
	if _, err := NewRequest(items, containers, zeroResolution, DefaultLimits()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("mass resolution error = %v", err)
	}
	limits := DefaultLimits()
	limits.MaxIDBytes = 1
	if _, err := NewRequest(items, containers, resolution, limits); !errors.Is(err, ErrInvalidItem) {
		t.Fatalf("ID error = %v", err)
	}
	shortItem := items[0]
	shortItem.spec.ID = "i"
	if _, err := NewRequest([]Item{shortItem}, containers, resolution, limits); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("container ID error = %v", err)
	}
	if _, err := NewRequest(items, []ContainerType{containers[0], containers[0]}, resolution, DefaultLimits()); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("duplicate container error = %v", err)
	}
}

func TestNormalizationStillDefendsAgainstInvalidDomainValues(t *testing.T) {
	t.Parallel()

	resolution := Resolution{Length: internalQuantity("1", measurement.Metre), Mass: internalQuantity("1", measurement.Kilogram)}
	validItem := Item{spec: ItemSpec{
		ID: "item",
		Dimensions: PhysicalDimensions{
			X: internalQuantity("1", measurement.Metre),
			Y: internalQuantity("1", measurement.Metre),
			Z: internalQuantity("1", measurement.Metre),
		},
		Weight:       internalQuantity("1", measurement.Kilogram),
		Orientations: []geometry.Orientation{geometry.OrientationXYZ},
	}}
	zeroWeight := validItem
	zeroWeight.spec.Weight = internalQuantity("0", measurement.Kilogram)
	if _, err := normalizeItem(zeroWeight, resolution); !errors.Is(err, ErrInvalidItem) {
		t.Fatalf("zero item weight error = %v", err)
	}
	invalidSupport := validItem
	wrongUnit := internalQuantity("1", measurement.Metre)
	invalidSupport.spec.MaxSupportedWeight = &wrongUnit
	if _, err := normalizeItem(invalidSupport, resolution); !errors.Is(err, ErrInvalidItem) {
		t.Fatalf("supported weight error = %v", err)
	}

	validContainer := ContainerType{spec: ContainerTypeSpec{
		ID:                 "box",
		InternalDimensions: validItem.spec.Dimensions,
		MaxContentWeight:   validItem.spec.Weight,
		Stock:              UnlimitedStock(),
	}}
	invalidDimensions := validContainer
	invalidDimensions.spec.InternalDimensions.X = internalQuantity("1", measurement.Kilogram)
	if _, err := normalizeContainer(invalidDimensions, resolution); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("container dimensions error = %v", err)
	}
	zeroContainerWeight := validContainer
	zeroContainerWeight.spec.MaxContentWeight = internalQuantity("0", measurement.Kilogram)
	if _, err := normalizeContainer(zeroContainerWeight, resolution); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("container weight error = %v", err)
	}
	negativeTare := validContainer
	tare := internalQuantity("-1", measurement.Kilogram)
	negativeTare.spec.TareWeight = &tare
	if _, err := normalizeContainer(negativeTare, resolution); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("tare error = %v", err)
	}
	invalidGross := validContainer
	tare, gross := internalQuantity("2", measurement.Kilogram), internalQuantity("1", measurement.Kilogram)
	invalidGross.spec.TareWeight, invalidGross.spec.MaxGrossWeight = &tare, &gross
	if _, err := normalizeContainer(invalidGross, resolution); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("gross error = %v", err)
	}
	if got := category(errors.New("other"), ErrInvalidItem); !errors.Is(got, ErrInvalidItem) {
		t.Fatalf("fallback category = %v", got)
	}
}

func TestMemoryEstimatesBoundNestedMetadata(t *testing.T) {
	t.Parallel()
	item := validNormalizedSpec().Items[0]
	item.Attributes = map[string]string{"class": "ordinary"}
	item.IncompatibleGroups = []string{"hazardous"}
	container := validNormalizedSpec().Containers[0]
	container.AllowedClasses = []string{"ordinary"}
	container.Reserved = []geometry.Cuboid{{}}
	used, ok := normalizedMemoryUsage([]NormalizedItem{item}, []NormalizedContainer{container}, math.MaxUint64)
	if !ok || used == 0 {
		t.Fatalf("used=%d ok=%v", used, ok)
	}
	if _, ok = normalizedMemoryUsage([]NormalizedItem{item}, []NormalizedContainer{container}, used-1); ok {
		t.Fatal("normalized estimate accepted a short budget")
	}
	for _, limit := range []uint64{512, 612, 625, 634, 717} {
		if _, within := normalizedMemoryUsage([]NormalizedItem{item}, []NormalizedContainer{container}, limit); within {
			t.Fatalf("normalized metadata accepted limit %d", limit)
		}
	}
	domainItem, _ := NewItem(ItemSpec{ID: item.ID, Dimensions: PhysicalDimensions{X: internalQuantity("1", measurement.Metre), Y: internalQuantity("1", measurement.Metre), Z: internalQuantity("1", measurement.Metre)}, Weight: internalQuantity("1", measurement.Kilogram), Orientations: item.Orientations, Attributes: item.Attributes, IncompatibleGroups: item.IncompatibleGroups})
	domainContainer, _ := NewContainerType(ContainerTypeSpec{
		ID: container.ID,
		InternalDimensions: PhysicalDimensions{
			X: internalQuantity("1", measurement.Metre),
			Y: internalQuantity("1", measurement.Metre),
			Z: internalQuantity("1", measurement.Metre),
		},
		MaxContentWeight: internalQuantity("1", measurement.Kilogram),
		Stock:            UnlimitedStock(),
		AllowedClasses:   container.AllowedClasses,
		Reserved: []ReservedRegion{{Dimensions: PhysicalDimensions{
			X: internalQuantity("1", measurement.Metre),
			Y: internalQuantity("1", measurement.Metre),
			Z: internalQuantity("1", measurement.Metre),
		}}},
	})
	if !domainMemoryWithin([]Item{domainItem}, []ContainerType{domainContainer}, math.MaxUint64) || domainMemoryWithin([]Item{domainItem}, []ContainerType{domainContainer}, 1) {
		t.Fatal("domain metadata estimate did not enforce its budget")
	}
	for _, limit := range []uint64{512, 612, 625, 634, 717} {
		if domainMemoryWithin([]Item{domainItem}, []ContainerType{domainContainer}, limit) {
			t.Fatalf("domain metadata accepted limit %d", limit)
		}
	}
}

func TestQuantityExpansionPropagatesInvalidInstanceSpec(t *testing.T) {
	t.Parallel()
	if _, err := ExpandQuantity(ItemSpec{ID: "item", Orientations: []geometry.Orientation{"invalid"}}, 1); !errors.Is(err, ErrInvalidItem) {
		t.Fatalf("error = %v", err)
	}
}
