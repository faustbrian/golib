package geotest_test

import (
	"math"
	"testing"

	"github.com/faustbrian/golib/pkg/geo/geotest"
)

func TestToleranceUsesAbsoluteAndRelativeBounds(t *testing.T) {
	t.Parallel()

	tolerance, err := geotest.NewTolerance(1e-9, 1e-6)
	if err != nil {
		t.Fatalf("NewTolerance() error = %v", err)
	}
	if !tolerance.Within(0, 5e-10) {
		t.Fatal("absolute tolerance rejected a near-zero value")
	}
	if !tolerance.Within(1_000_000, 1_000_000.5) {
		t.Fatal("relative tolerance rejected a scaled value")
	}
	if tolerance.Within(1, 2) {
		t.Fatal("tolerance accepted values outside both bounds")
	}
	if tolerance.Within(math.NaN(), math.NaN()) {
		t.Fatal("tolerance accepted NaN")
	}
}

func TestToleranceRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	for _, values := range [][2]float64{{-1, 0}, {0, -1}, {math.Inf(1), 0}} {
		if _, err := geotest.NewTolerance(values[0], values[1]); err == nil {
			t.Fatalf("NewTolerance(%v, %v) succeeded", values[0], values[1])
		}
	}
}

func TestWGS84VectorsAreReturnedAsAnOwnedCopy(t *testing.T) {
	t.Parallel()

	vectors := geotest.WGS84InverseVectors()
	if len(vectors) == 0 {
		t.Fatal("WGS84InverseVectors() returned no vectors")
	}
	firstName := vectors[0].Name
	vectors[0].Name = "changed"
	if geotest.WGS84InverseVectors()[0].Name != firstName {
		t.Fatal("WGS84InverseVectors() exposed package state")
	}
}

func TestWGS84DistanceVectorsAreReturnedAsAnOwnedCopy(t *testing.T) {
	vectors := geotest.WGS84DistanceVectors()
	if len(vectors) == 0 {
		t.Fatal("WGS84DistanceVectors() returned no cases")
	}
	vectors[0].Name = "changed"
	if geotest.WGS84DistanceVectors()[0].Name == "changed" {
		t.Fatal("WGS84DistanceVectors() returned aliased storage")
	}
}

func TestPolygonVectorsAreReturnedAsDeepOwnedCopies(t *testing.T) {
	t.Parallel()

	first := geotest.PolygonLocationVectors()
	second := geotest.PolygonLocationVectors()
	if len(first) == 0 || len(first[0].Exterior) == 0 || len(first[0].Probes) == 0 {
		t.Fatal("PolygonLocationVectors() returned incomplete data")
	}
	first[0].Exterior[0][0] = -999
	first[0].Probes[0].Longitude = -999
	if second[0].Exterior[0][0] == -999 || second[0].Probes[0].Longitude == -999 {
		t.Fatal("PolygonLocationVectors() returned aliased data")
	}
}

func TestAssertCloseReportsOnlyFailures(t *testing.T) {
	t.Parallel()

	tolerance, err := geotest.NewTolerance(0.1, 0)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &testingRecorder{}
	geotest.AssertClose(recorder, "distance", 1, 1.05, tolerance)
	if recorder.errors != 0 || recorder.helpers != 1 {
		t.Fatalf("passing assertion recorded helpers=%d errors=%d",
			recorder.helpers, recorder.errors)
	}
	geotest.AssertClose(recorder, "distance", 1, 2, tolerance)
	if recorder.errors != 1 || recorder.helpers != 2 {
		t.Fatalf("failing assertion recorded helpers=%d errors=%d",
			recorder.helpers, recorder.errors)
	}
}

type testingRecorder struct {
	helpers int
	errors  int
}

func (recorder *testingRecorder) Helper() { recorder.helpers++ }

func (recorder *testingRecorder) Errorf(string, ...any) { recorder.errors++ }
