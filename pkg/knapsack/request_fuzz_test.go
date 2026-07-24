package knapsack_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

func FuzzItemContainerValidation(f *testing.F) {
	f.Add(int64(1), int64(1), uint32(0), uint32(1_000_000))
	f.Add(int64(0), int64(-1), uint32(1_000_001), uint32(0))
	f.Fuzz(func(t *testing.T, dimension, weight int64, centerMinimum, centerMaximum uint32) {
		if dimension < -1_000_000 || dimension > 1_000_000 || weight < -1_000_000 || weight > 1_000_000 {
			return
		}
		quantity := func(value int64, unit measurement.Unit) measurement.Quantity {
			return measurement.MustNew(decimal.New(value), unit)
		}
		dimensions := knapsack.PhysicalDimensions{
			X: quantity(dimension, measurement.Metre),
			Y: quantity(1, measurement.Metre),
			Z: quantity(1, measurement.Metre),
		}
		_, itemErr := knapsack.NewItem(knapsack.ItemSpec{
			ID: "item", Dimensions: dimensions, Weight: quantity(weight, measurement.Kilogram),
			Orientations: []geometry.Orientation{geometry.OrientationXYZ},
		})
		_, containerErr := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
			ID: "box", InternalDimensions: dimensions,
			MaxContentWeight: quantity(weight, measurement.Kilogram), Stock: knapsack.FiniteStock(1),
			CenterOfGravity: &knapsack.CenterOfGravityBounds{MinXPPM: centerMinimum, MaxXPPM: centerMaximum},
		})
		itemValid := dimension > 0 && weight > 0
		containerValid := itemValid && centerMaximum <= 1_000_000 && centerMinimum <= centerMaximum
		if itemValid != (itemErr == nil) || containerValid != (containerErr == nil) {
			t.Fatalf("dimension=%d weight=%d item=%v container=%v", dimension, weight, itemErr, containerErr)
		}
	})
}

func TestFuzzRegressionContainerBoundsDoNotChangeItemValidity(t *testing.T) {
	t.Parallel()

	quantity := func(value int64, unit measurement.Unit) measurement.Quantity {
		return measurement.MustNew(decimal.New(value), unit)
	}
	dimensions := knapsack.PhysicalDimensions{
		X: quantity(1, measurement.Metre),
		Y: quantity(1, measurement.Metre),
		Z: quantity(1, measurement.Metre),
	}
	if _, err := knapsack.NewItem(knapsack.ItemSpec{
		ID: "item", Dimensions: dimensions, Weight: quantity(1, measurement.Kilogram),
		Orientations: []geometry.Orientation{geometry.OrientationXYZ},
	}); err != nil {
		t.Fatalf("valid item rejected by an unrelated container policy: %v", err)
	}
	if _, err := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "box", InternalDimensions: dimensions,
		MaxContentWeight: quantity(1, measurement.Kilogram), Stock: knapsack.FiniteStock(1),
		CenterOfGravity: &knapsack.CenterOfGravityBounds{MaxXPPM: 1_000_023},
	}); !errors.Is(err, knapsack.ErrInvalidContainer) {
		t.Fatalf("invalid center-of-gravity maximum error = %v", err)
	}
}

func FuzzLatticeNormalization(f *testing.F) {
	f.Add(int64(6), int64(2))
	f.Add(int64(5), int64(2))
	f.Fuzz(func(t *testing.T, length, resolution int64) {
		if length <= 0 || resolution <= 0 || length > 1_000_000 || resolution > 1_000_000 {
			return
		}
		quantity := func(value int64, unit measurement.Unit) measurement.Quantity {
			return measurement.MustNew(decimal.New(value), unit)
		}
		dimensions := knapsack.PhysicalDimensions{
			X: quantity(length, measurement.Metre),
			Y: quantity(resolution, measurement.Metre),
			Z: quantity(resolution, measurement.Metre),
		}
		item, itemErr := knapsack.NewItem(knapsack.ItemSpec{
			ID: "item", Dimensions: dimensions, Weight: quantity(1, measurement.Kilogram),
			Orientations: []geometry.Orientation{geometry.OrientationXYZ},
		})
		box, boxErr := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
			ID: "box", InternalDimensions: dimensions,
			MaxContentWeight: quantity(1, measurement.Kilogram), Stock: knapsack.FiniteStock(1),
		})
		if itemErr != nil || boxErr != nil {
			t.Fatalf("valid physical values rejected: %v %v", itemErr, boxErr)
		}
		request, err := knapsack.NewRequest(
			[]knapsack.Item{item}, []knapsack.ContainerType{box},
			knapsack.Resolution{Length: quantity(resolution, measurement.Metre), Mass: quantity(1, measurement.Kilogram)},
			knapsack.DefaultLimits(),
		)
		if length%resolution != 0 {
			if !errors.Is(err, knapsack.ErrInexactResolution) {
				t.Fatalf("inexact ratio error = %v", err)
			}
			return
		}
		if err != nil || request.Normalized().Items()[0].Dimensions.X != length/resolution {
			t.Fatalf("normalized request=%+v error=%v", request.Normalized().Items(), err)
		}
	})
}
