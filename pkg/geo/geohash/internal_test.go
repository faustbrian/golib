package geohash

import "testing"

func TestIndexDimensionsAndBoundaryMapping(t *testing.T) {
	t.Parallel()

	columns, rows, width, height := dimensions(1)
	if columns != 8 || rows != 4 || width != 45 || height != 45 {
		t.Fatalf("dimensions(1) = %d, %d, %v, %v; want 8, 4, 45, 45",
			columns, rows, width, height)
	}
	columns, rows, width, height = dimensions(2)
	if columns != 32 || rows != 32 || width != 11.25 || height != 5.625 {
		t.Fatalf("dimensions(2) = %d, %d, %v, %v; want 32, 32, 11.25, 5.625",
			columns, rows, width, height)
	}

	for _, test := range []struct {
		longitude float64
		want      int64
	}{
		{longitude: -180, want: 0},
		{longitude: -135, want: 1},
		{longitude: 0, want: 4},
		{longitude: 180, want: 7},
		{longitude: 181, want: 7},
		{longitude: -181, want: 0},
	} {
		if got := longitudeIndex(test.longitude, 45, 8); got != test.want {
			t.Fatalf("longitudeIndex(%v) = %d, want %d", test.longitude, got, test.want)
		}
	}
	for _, test := range []struct {
		latitude float64
		want     int64
	}{
		{latitude: -90, want: 0},
		{latitude: -45, want: 1},
		{latitude: 0, want: 2},
		{latitude: 90, want: 3},
		{latitude: 91, want: 3},
		{latitude: -91, want: 0},
	} {
		if got := latitudeIndex(test.latitude, 45, 4); got != test.want {
			t.Fatalf("latitudeIndex(%v) = %d, want %d", test.latitude, got, test.want)
		}
	}
}

func TestLongitudeWrappingIsInclusiveAndSingleStepBounded(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		longitude float64
		want      float64
	}{
		{longitude: -180, want: -180},
		{longitude: 180, want: 180},
		{longitude: -181, want: 179},
		{longitude: 181, want: -179},
		{longitude: 0, want: 0},
	} {
		if got := wrapLongitude(test.longitude); got != test.want {
			t.Fatalf("wrapLongitude(%v) = %v, want %v", test.longitude, got, test.want)
		}
	}
}
