package window

import (
	"math"
	"math/big"
	"testing"
	"time"
)

func TestConstructorsAcceptExactResourceBounds(t *testing.T) {
	t.Parallel()

	if _, err := NewCount(MaxCountSize); err != nil {
		t.Fatalf("NewCount(max) error = %v", err)
	}
	if _, err := NewTime(time.Nanosecond, MaxBucketCount); err != nil {
		t.Fatalf("NewTime(max buckets) error = %v", err)
	}
	maximumDuration := time.Duration(math.MaxInt64 / MaxBucketCount)
	if _, err := NewTime(maximumDuration, MaxBucketCount); err != nil {
		t.Fatalf("NewTime(maximum interval) error = %v", err)
	}
}

func TestBucketIDMatchesArbitraryPrecisionFloorDivision(t *testing.T) {
	t.Parallel()

	timestamps := []time.Time{
		time.Unix(-1, 500_000_000),
		time.Unix(0, 0),
		time.Date(1, time.January, 1, 0, 0, 0, 1, time.UTC),
		time.Date(9999, time.December, 31, 23, 59, 59, 999_999_999, time.UTC),
	}
	for _, duration := range []time.Duration{time.Nanosecond, 3 * time.Nanosecond, time.Second} {
		for _, at := range timestamps {
			got := bucketIDBigInt(bucketIDAt(at, duration))
			want := timestampBucketBigInt(at, duration)
			if got.Cmp(want) != 0 {
				t.Fatalf("bucketIDAt(%v, %v) = %v, want %v", at, duration, got, want)
			}
		}
	}
}

func TestBucketIDPrimitiveBoundaries(t *testing.T) {
	t.Parallel()

	for _, value := range []int64{math.MinInt64, -1, 0, 1, math.MaxInt64} {
		if got := bucketIDBigInt(bucketIDFromInt64(value)); got.Cmp(big.NewInt(value)) != 0 {
			t.Fatalf("bucketIDFromInt64(%d) = %v", value, got)
		}
	}

	tests := []struct {
		id    bucketID
		value uint64
	}{
		{id: bucketID{high: 1}, value: 1},
		{id: bucketID{low: 1}, value: 1},
		{id: bucketID{low: 1}, value: 2},
		{id: bucketID{negative: true, high: 1, low: math.MaxUint64}, value: 1},
	}
	for _, test := range tests {
		want := new(big.Int).Sub(bucketIDBigInt(test.id), new(big.Int).SetUint64(test.value))
		if got := bucketIDBigInt(test.id.subtract(test.value)); got.Cmp(want) != 0 {
			t.Fatalf("%v.subtract(%d) = %v, want %v", test.id, test.value, got, want)
		}
	}
	if got := (bucketID{low: 1}).subtract(1); got != (bucketID{}) {
		t.Fatalf("bucketID(1).subtract(1) = %+v, want canonical zero", got)
	}
}

func TestUnsignedWideArithmeticCarriesAndBorrows(t *testing.T) {
	t.Parallel()

	if high, low := add128(1, math.MaxUint64, 1); high != 2 || low != 0 {
		t.Fatalf("add128 carry = %d/%d, want 2/0", high, low)
	}
	if high, low := add128(1, 2, 3); high != 1 || low != 5 {
		t.Fatalf("add128 no carry = %d/%d, want 1/5", high, low)
	}
	if high, low := subtract128(1, 0, 1); high != 0 || low != math.MaxUint64 {
		t.Fatalf("subtract128 borrow = %d/%d, want 0/%d", high, low, uint64(math.MaxUint64))
	}
	if high, low := subtract128(1, 5, 3); high != 1 || low != 2 {
		t.Fatalf("subtract128 no borrow = %d/%d, want 1/2", high, low)
	}
}

func TestExactUnixNanosecondsAdjacentBoundaryArithmetic(t *testing.T) {
	t.Parallel()

	timestamps := []time.Time{
		time.Unix(-9_223_372_037, 145_224_191),
		time.Unix(-9_223_372_037, 145_224_192),
		time.Unix(-9_223_372_037, 145_224_193),
		time.Unix(9_223_372_036, 854_775_806),
		time.Unix(9_223_372_036, 854_775_807),
		time.Unix(9_223_372_036, 854_775_808),
	}
	for _, at := range timestamps {
		got, ok := exactUnixNanoseconds(at)
		wantBig := new(big.Int).Mul(big.NewInt(at.Unix()), big.NewInt(int64(time.Second)))
		wantBig.Add(wantBig, big.NewInt(int64(at.Nanosecond())))
		wantOK := wantBig.IsInt64()
		if ok != wantOK {
			t.Fatalf("exactUnixNanoseconds(%v) ok = %t, want %t", at, ok, wantOK)
		}
		if ok && got != wantBig.Int64() {
			t.Fatalf("exactUnixNanoseconds(%v) = %d, want %d", at, got, wantBig.Int64())
		}
	}
}

func TestMergeAddsEverySnapshotDimension(t *testing.T) {
	t.Parallel()

	destination := Snapshot{Classified: 1, Successes: 2, Failures: 3, Ignored: 4, SlowSuccess: 5, SlowFailure: 6}
	source := Snapshot{Classified: 10, Successes: 20, Failures: 30, Ignored: 40, SlowSuccess: 50, SlowFailure: 60}
	merge(&destination, source)
	want := Snapshot{Classified: 11, Successes: 22, Failures: 33, Ignored: 44, SlowSuccess: 55, SlowFailure: 66}
	if destination != want {
		t.Fatalf("merge() = %+v, want %+v", destination, want)
	}
}

func bucketIDBigInt(id bucketID) *big.Int {
	magnitude := new(big.Int).SetUint64(id.high)
	magnitude.Lsh(magnitude, 64)
	magnitude.Add(magnitude, new(big.Int).SetUint64(id.low))
	if id.negative {
		magnitude.Neg(magnitude)
	}
	return magnitude
}

func timestampBucketBigInt(at time.Time, duration time.Duration) *big.Int {
	nanoseconds := new(big.Int).Mul(big.NewInt(at.Unix()), big.NewInt(int64(time.Second)))
	nanoseconds.Add(nanoseconds, big.NewInt(int64(at.Nanosecond())))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(nanoseconds, big.NewInt(int64(duration)), remainder)
	if nanoseconds.Sign() < 0 && remainder.Sign() != 0 {
		quotient.Sub(quotient, big.NewInt(1))
	}
	return quotient
}
