package knapsack_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

func length(value string, unit measurement.Unit) measurement.Quantity {
	return measurement.MustNew(decimal.MustParse(value), unit)
}

func mass(value string, unit measurement.Unit) measurement.Quantity {
	return measurement.MustNew(decimal.MustParse(value), unit)
}

func itemSpec(id string) knapsack.ItemSpec {
	return knapsack.ItemSpec{
		ID: id,
		Dimensions: knapsack.PhysicalDimensions{
			X: length("10", measurement.Centimetre),
			Y: length("20", measurement.Centimetre),
			Z: length("30", measurement.Centimetre),
		},
		Weight:       mass("2", measurement.Kilogram),
		Orientations: []geometry.Orientation{geometry.OrientationXYZ},
	}
}

func TestDomainObjectsDefensivelyCopy(t *testing.T) {
	t.Parallel()

	spec := itemSpec("sku-1")
	spec.Attributes = map[string]string{"class": "ordinary"}
	item, err := knapsack.NewItem(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Orientations[0] = geometry.OrientationZYX
	spec.Attributes["class"] = "changed"

	if item.Orientations()[0] != geometry.OrientationXYZ || item.Attributes()["class"] != "ordinary" {
		t.Fatal("item aliases caller-owned input")
	}
	attributes := item.Attributes()
	attributes["class"] = "again"
	if item.Attributes()["class"] != "ordinary" {
		t.Fatal("item exposes mutable attributes")
	}
}

func TestDomainConstructorsRejectInvalidPhysicalValues(t *testing.T) {
	t.Parallel()

	validItem := itemSpec("item")
	for _, test := range []struct {
		name   string
		mutate func(*knapsack.ItemSpec)
	}{
		{"zero dimension", func(spec *knapsack.ItemSpec) { spec.Dimensions.X = length("0", measurement.Metre) }},
		{"wrong dimension unit", func(spec *knapsack.ItemSpec) { spec.Dimensions.Y = mass("1", measurement.Kilogram) }},
		{"zero weight", func(spec *knapsack.ItemSpec) { spec.Weight = mass("0", measurement.Kilogram) }},
		{"wrong weight unit", func(spec *knapsack.ItemSpec) { spec.Weight = length("1", measurement.Metre) }},
		{"negative supported weight", func(spec *knapsack.ItemSpec) {
			value := mass("-1", measurement.Kilogram)
			spec.MaxSupportedWeight = &value
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := validItem
			test.mutate(&spec)
			if _, err := knapsack.NewItem(spec); !errors.Is(err, knapsack.ErrInvalidItem) {
				t.Fatalf("error = %v, want invalid item", err)
			}
		})
	}

	validContainer := knapsack.ContainerTypeSpec{
		ID: "box",
		InternalDimensions: knapsack.PhysicalDimensions{
			X: length("1", measurement.Metre),
			Y: length("1", measurement.Metre),
			Z: length("1", measurement.Metre),
		},
		MaxContentWeight: mass("1", measurement.Kilogram),
		Stock:            knapsack.UnlimitedStock(),
	}
	for _, test := range []struct {
		name   string
		mutate func(*knapsack.ContainerTypeSpec)
	}{
		{"zero dimension", func(spec *knapsack.ContainerTypeSpec) { spec.InternalDimensions.Z = length("0", measurement.Metre) }},
		{"wrong dimension unit", func(spec *knapsack.ContainerTypeSpec) { spec.InternalDimensions.X = mass("1", measurement.Kilogram) }},
		{"zero content weight", func(spec *knapsack.ContainerTypeSpec) { spec.MaxContentWeight = mass("0", measurement.Kilogram) }},
		{"invalid external dimensions", func(spec *knapsack.ContainerTypeSpec) {
			value := knapsack.PhysicalDimensions{}
			spec.ExternalDimensions = &value
		}},
		{"negative tare", func(spec *knapsack.ContainerTypeSpec) {
			value := mass("-1", measurement.Kilogram)
			spec.TareWeight = &value
		}},
		{"zero gross weight", func(spec *knapsack.ContainerTypeSpec) {
			value := mass("0", measurement.Kilogram)
			spec.MaxGrossWeight = &value
		}},
		{"gross below tare", func(spec *knapsack.ContainerTypeSpec) {
			tare, gross := mass("2", measurement.Kilogram), mass("1", measurement.Kilogram)
			spec.TareWeight, spec.MaxGrossWeight = &tare, &gross
		}},
		{"invalid reserved region", func(spec *knapsack.ContainerTypeSpec) {
			spec.Reserved = []knapsack.ReservedRegion{{Origin: geometry.Point{X: -1}}}
		}},
		{"zero finite stock", func(spec *knapsack.ContainerTypeSpec) { spec.Stock = knapsack.FiniteStock(0) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := validContainer
			test.mutate(&spec)
			if _, err := knapsack.NewContainerType(spec); !errors.Is(err, knapsack.ErrInvalidContainer) {
				t.Fatalf("error = %v, want invalid container", err)
			}
		})
	}
}

func TestQuantityExpansionIsBoundedBeforeAllocation(t *testing.T) {
	t.Parallel()

	if _, err := knapsack.ExpandQuantity(itemSpec("item"), knapsack.DefaultLimits().MaxItems+1); !errors.Is(err, knapsack.ErrBudgetExhausted) {
		t.Fatalf("default expansion error = %v, want budget exhausted", err)
	}
	if _, err := knapsack.ExpandQuantityWithLimit(itemSpec("item"), 3, 2); !errors.Is(err, knapsack.ErrBudgetExhausted) {
		t.Fatalf("bounded expansion error = %v, want budget exhausted", err)
	}
	if _, err := knapsack.ExpandQuantityWithLimit(itemSpec("item"), 1, 0); !errors.Is(err, knapsack.ErrInvalidOptions) {
		t.Fatalf("zero limit error = %v, want invalid options", err)
	}
	items, err := knapsack.ExpandQuantityWithLimit(itemSpec("item"), 2, 2)
	if err != nil || len(items) != 2 {
		t.Fatalf("bounded expansion = %d, %v", len(items), err)
	}
}

func TestNormalizeUsesExactMeasurementLattice(t *testing.T) {
	t.Parallel()

	item, err := knapsack.NewItem(itemSpec("one"))
	if err != nil {
		t.Fatal(err)
	}
	container, err := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "box", InternalDimensions: knapsack.PhysicalDimensions{
			X: length("1", measurement.Metre), Y: length("1", measurement.Metre), Z: length("1", measurement.Metre),
		}, MaxContentWeight: mass("10", measurement.Kilogram), Stock: knapsack.UnlimitedStock(),
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{container}, knapsack.Resolution{
		Length: length("1", measurement.Centimetre), Mass: mass("1", measurement.Gram),
	}, knapsack.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	normalized := request.Normalized()
	if got, want := normalized.Items()[0].Dimensions, (geometry.Dimensions{X: 10, Y: 20, Z: 30}); got != want {
		t.Fatalf("dimensions = %+v, want %+v", got, want)
	}
	if got := normalized.Items()[0].Weight; got != 2000 {
		t.Fatalf("weight = %d, want 2000", got)
	}
	if got := normalized.Containers()[0].Dimensions.X; got != 100 {
		t.Fatalf("container X = %d, want 100", got)
	}
}

func TestNormalizeRejectsInexactAndDuplicateInput(t *testing.T) {
	t.Parallel()

	spec := itemSpec("duplicate")
	spec.Dimensions.X = length("1.5", measurement.Centimetre)
	item, _ := knapsack.NewItem(spec)
	container, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "box", InternalDimensions: knapsack.PhysicalDimensions{
			X: length("1", measurement.Metre), Y: length("1", measurement.Metre), Z: length("1", measurement.Metre),
		}, MaxContentWeight: mass("10", measurement.Kilogram), Stock: knapsack.FiniteStock(1),
	})
	_, err := knapsack.NewRequest([]knapsack.Item{item, item}, []knapsack.ContainerType{container}, knapsack.Resolution{
		Length: length("1", measurement.Centimetre), Mass: mass("1", measurement.Gram),
	}, knapsack.DefaultLimits())
	if !errors.Is(err, knapsack.ErrDuplicateID) {
		t.Fatalf("duplicate error = %v", err)
	}

	_, err = knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{container}, knapsack.Resolution{
		Length: length("1", measurement.Centimetre), Mass: mass("1", measurement.Gram),
	}, knapsack.DefaultLimits())
	if !errors.Is(err, knapsack.ErrInexactResolution) {
		t.Fatalf("inexact error = %v", err)
	}
}

func TestQuantityExpansionIsBoundedAndDeterministic(t *testing.T) {
	t.Parallel()

	items, err := knapsack.ExpandQuantity(itemSpec("widget"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].ID() != "widget#000001" || items[2].ID() != "widget#000003" {
		t.Fatalf("unexpected IDs: %q %q", items[0].ID(), items[2].ID())
	}
	if _, err := knapsack.ExpandQuantity(itemSpec("widget"), 0); !errors.Is(err, knapsack.ErrInvalidItem) {
		t.Fatalf("zero quantity: %v", err)
	}
}

func TestNormalizationEnforcesMemoryBudget(t *testing.T) {
	t.Parallel()
	item, _ := knapsack.NewItem(itemSpec("item"))
	container, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "box", InternalDimensions: knapsack.PhysicalDimensions{
			X: length("1", measurement.Metre), Y: length("1", measurement.Metre), Z: length("1", measurement.Metre),
		}, MaxContentWeight: mass("10", measurement.Kilogram), Stock: knapsack.UnlimitedStock(),
	})
	limits := knapsack.DefaultLimits()
	limits.MaxMemoryBytes = 1
	_, err := knapsack.NewRequest(
		[]knapsack.Item{item}, []knapsack.ContainerType{container},
		knapsack.Resolution{Length: length("1", measurement.Centimetre), Mass: mass("1", measurement.Gram)}, limits,
	)
	if !errors.Is(err, knapsack.ErrMemoryBudgetExhausted) || !errors.Is(err, knapsack.ErrBudgetExhausted) {
		t.Fatalf("error = %v", err)
	}
}
