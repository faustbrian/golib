package verify_test

import (
	"math"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/measurement"
)

func FuzzSuppliedPlan(f *testing.F) {
	f.Add(int64(0), int64(2))
	f.Fuzz(func(t *testing.T, x, weight int64) {
		request := request(t)
		spec := validPlan(t).Spec()
		spec.Placements[1].Origin.X = x
		spec.Placements[1].Weight = weight
		plan, err := knapsack.NewPlan(spec)
		if err != nil {
			return
		}
		result := verify.Plan(request, plan, verify.RequireAll())
		if result.Valid() {
			boxA, _ := geometry.NewCuboid(spec.Placements[0].Origin, spec.Placements[0].Dimensions)
			boxB, _ := geometry.NewCuboid(spec.Placements[1].Origin, spec.Placements[1].Dimensions)
			if boxA.Intersects(boxB) {
				t.Fatal("overlap accepted")
			}
		}
	})
}

func FuzzPhysicalConstraints(f *testing.F) {
	f.Add(int64(0), uint32(1_000_000), false, int64(1), uint32(0), uint32(1_000_000))
	f.Add(int64(1), uint32(1), true, int64(0), uint32(500_000), uint32(500_000))
	f.Fuzz(func(t *testing.T, topX int64, supportPPM uint32, fragile bool, maximumLoad int64, centerMinimum, centerMaximum uint32) {
		if topX < 0 || topX > 2 || supportPPM > 1_000_000 || maximumLoad < 0 || maximumLoad > math.MaxInt32 {
			return
		}
		centerMinimum %= 1_000_001
		centerMaximum %= 1_000_001
		if centerMinimum > centerMaximum {
			centerMinimum, centerMaximum = centerMaximum, centerMinimum
		}
		base, top := unitSpec("base"), unitSpec("top")
		base.FragileTop = fragile
		maximum := q(maximumLoad, measurement.Kilogram)
		base.MaxSupportedWeight = &maximum
		top.MinimumSupportPPM = supportPPM
		box := containerSpec(2, 2, 1)
		box.CenterOfGravity = &knapsack.CenterOfGravityBounds{
			MinXPPM: centerMinimum, MaxXPPM: centerMaximum,
			MinYPPM: 0, MaxYPPM: 1_000_000,
			MinZPPM: 0, MaxZPPM: 1_000_000,
		}
		request := normalizedRequest(t, []knapsack.ItemSpec{base, top}, box)
		containers := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
		supporters := []string(nil)
		if topX == 0 {
			supporters = []string{"base"}
		}
		placements := []knapsack.Placement{
			{ItemID: "base", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1},
			{ItemID: "top", ContainerID: "box#1", Origin: geometry.Point{X: topX, Z: 1}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1, SupporterIDs: supporters},
		}
		result := verify.Plan(request, planWithStatistics(t, request, containers, placements, nil), verify.RequireAll())
		if result.Valid() && topX == 0 && (fragile || maximumLoad < 1) {
			t.Fatal("invalid stacked placement accepted")
		}
		if result.Valid() && topX > 0 && supportPPM > 0 {
			t.Fatal("unsupported placement accepted")
		}
		centerPPM := uint32((topX + 1) * 250_000)
		if result.Valid() && (centerPPM < centerMinimum || centerPPM > centerMaximum) {
			t.Fatal("out-of-bounds center of gravity accepted")
		}
	})
}
