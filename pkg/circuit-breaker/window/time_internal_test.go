package window

import (
	"math"
	"testing"
	"time"
)

func TestBucketIDFloorDivision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		at       time.Time
		duration time.Duration
		want     int64
	}{
		{
			name:     "negative wide exact division",
			at:       time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC),
			duration: time.Hour,
			want:     -17_259_888,
		},
		{
			name:     "negative wide floor division",
			at:       time.Date(1, time.January, 1, 0, 0, 0, 1, time.UTC),
			duration: time.Hour,
			want:     -17_259_888,
		},
		{
			name:     "negative exact division",
			at:       time.Unix(-1, 0),
			duration: time.Second,
			want:     -1,
		},
		{
			name:     "negative floor division",
			at:       time.Unix(-1, 500*time.Millisecond.Nanoseconds()),
			duration: time.Second,
			want:     -1,
		},
		{
			name:     "positive division",
			at:       time.Unix(1, 500*time.Millisecond.Nanoseconds()),
			duration: time.Second,
			want:     1,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := bucketIDAt(test.at, test.duration); got != bucketIDFromInt64(test.want) {
				t.Fatalf("bucketIDAt(%v, %v) = %+v, want %d", test.at, test.duration, got, test.want)
			}
		})
	}
}

func TestBucketIDPreservesWideTimestampOrdering(t *testing.T) {
	t.Parallel()

	old := bucketIDAt(time.Date(9998, time.January, 1, 0, 0, 0, 0, time.UTC), time.Nanosecond)
	current := bucketIDAt(time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC), time.Nanosecond)
	if old.high == 0 || current.compare(old) <= 0 {
		t.Fatalf("wide positive IDs old/current = %+v/%+v", old, current)
	}
	ancient := bucketIDAt(time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC), time.Nanosecond)
	if !ancient.negative || ancient.high == 0 || ancient.compare(bucketID{}) >= 0 {
		t.Fatalf("wide negative ID = %+v", ancient)
	}
}

func TestBucketIDSubtractCrossesEpoch(t *testing.T) {
	t.Parallel()

	if got := (bucketID{low: 1}).subtract(2); got != (bucketID{negative: true, low: 1}) {
		t.Fatalf("bucketID(1).subtract(2) = %+v", got)
	}
}

func TestExactUnixNanosecondsBoundaries(t *testing.T) {
	t.Parallel()

	minimum := time.Unix(0, math.MinInt64)
	maximum := time.Unix(0, math.MaxInt64)
	for _, test := range []struct {
		name string
		at   time.Time
		want int64
		ok   bool
	}{
		{name: "minimum", at: minimum, want: math.MinInt64, ok: true},
		{name: "below minimum", at: minimum.Add(-time.Nanosecond), ok: false},
		{name: "maximum", at: maximum, want: math.MaxInt64, ok: true},
		{name: "above maximum", at: maximum.Add(time.Nanosecond), ok: false},
		{name: "epoch", at: time.Unix(0, 0), want: 0, ok: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := exactUnixNanoseconds(test.at)
			if got != test.want || ok != test.ok {
				t.Fatalf("exactUnixNanoseconds(%v) = %d, %t; want %d, %t", test.at, got, ok, test.want, test.ok)
			}
		})
	}
}
