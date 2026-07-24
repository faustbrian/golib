package window

import (
	"fmt"
	"math"
	"math/bits"
	"time"
)

type timeBucket struct {
	id       bucketID
	occupied bool
	snapshot Snapshot
}

type bucketID struct {
	negative bool
	high     uint64
	low      uint64
}

// Time retains aggregate records in a fixed number of duration buckets.
// Time is not safe for concurrent use; the owning breaker serializes access.
type Time struct {
	bucketDuration time.Duration
	buckets        []timeBucket
	lastBucketID   bucketID
	initialized    bool
}

// NewTime constructs a time-based window with fixed memory use.
func NewTime(bucketDuration time.Duration, bucketCount int) (*Time, error) {
	if bucketDuration <= 0 {
		return nil, fmt.Errorf("window: bucket duration must be greater than zero")
	}
	if bucketCount <= 0 {
		return nil, fmt.Errorf("window: bucket count must be greater than zero")
	}
	if bucketCount > MaxBucketCount {
		return nil, fmt.Errorf("window: bucket count must not exceed %d", MaxBucketCount)
	}
	if bucketDuration > time.Duration(1<<63-1)/time.Duration(bucketCount) {
		return nil, fmt.Errorf("window: rolling interval overflows time.Duration")
	}

	return &Time{
		bucketDuration: bucketDuration,
		buckets:        make([]timeBucket, bucketCount),
	}, nil
}

// Add records a completion in the bucket containing at. Timestamps older than
// the latest observed timestamp are clamped so clock movement cannot resurrect
// expired data.
func (w *Time) Add(at time.Time, record Record) error {
	if !valid(record) {
		return fmt.Errorf("window: unknown outcome class %d", record.Class)
	}

	id := w.observe(at)
	index := w.index(id)
	bucket := &w.buckets[index]
	if !bucket.occupied || bucket.id != id {
		*bucket = timeBucket{id: id, occupied: true}
	}
	bucket.snapshot.add(record)

	return nil
}

// Snapshot returns aggregates that have not expired as of at.
func (w *Time) Snapshot(at time.Time) Snapshot {
	current := w.observe(at)
	oldest := current.subtract(uint64(len(w.buckets) - 1))

	var snapshot Snapshot
	for i := range w.buckets {
		bucket := &w.buckets[i]
		if bucket.occupied && bucket.id.compare(oldest) >= 0 && bucket.id.compare(current) <= 0 {
			merge(&snapshot, bucket.snapshot)
		}
	}

	return snapshot
}

func (w *Time) observe(at time.Time) bucketID {
	id := bucketIDAt(at, w.bucketDuration)
	if !w.initialized || id.compare(w.lastBucketID) > 0 {
		w.lastBucketID = id
		w.initialized = true
	}
	return w.lastBucketID
}

func bucketIDAt(at time.Time, duration time.Duration) bucketID {
	const nanosecondsPerSecond = uint64(time.Second)

	if nanoseconds, ok := exactUnixNanoseconds(at); ok {
		bucket := nanoseconds / int64(duration)
		if nanoseconds < 0 && nanoseconds%int64(duration) != 0 {
			bucket--
		}
		return bucketIDFromInt64(bucket)
	}

	seconds := at.Unix()
	nanoseconds := uint64(at.Nanosecond())
	negative := seconds < 0
	magnitudeSeconds := uint64(seconds)
	if negative {
		magnitudeSeconds = uint64(-(seconds + 1)) + 1
	}
	high, low := bits.Mul64(magnitudeSeconds, nanosecondsPerSecond)
	if negative {
		var borrow uint64
		low, borrow = bits.Sub64(low, nanoseconds, 0)
		high -= borrow
	} else {
		var carry uint64
		low, carry = bits.Add64(low, nanoseconds, 0)
		high += carry
	}

	divisor := uint64(duration)
	quotientHigh := high / divisor
	quotientLow, remainder := bits.Div64(high%divisor, low, divisor)
	if negative && remainder != 0 {
		var carry uint64
		quotientLow, carry = bits.Add64(quotientLow, 1, 0)
		quotientHigh += carry
	}
	return bucketID{negative: negative, high: quotientHigh, low: quotientLow}
}

func bucketIDFromInt64(value int64) bucketID {
	if value >= 0 {
		return bucketID{low: uint64(value)}
	}
	return bucketID{negative: true, low: uint64(-(value + 1)) + 1}
}

func (id bucketID) compare(other bucketID) int {
	if id.negative != other.negative {
		if id.negative {
			return -1
		}
		return 1
	}
	comparison := compareMagnitude(id, other)
	if id.negative {
		return -comparison
	}
	return comparison
}

func compareMagnitude(left, right bucketID) int {
	if left.high < right.high || left.high == right.high && left.low < right.low {
		return -1
	}
	if left.high > right.high || left.high == right.high && left.low > right.low {
		return 1
	}
	return 0
}

func (id bucketID) subtract(value uint64) bucketID {
	if id.negative {
		low, carry := bits.Add64(id.low, value, 0)
		return bucketID{negative: true, high: id.high + carry, low: low}
	}
	if id.high == 0 && id.low < value {
		return bucketID{negative: true, low: value - id.low}
	}
	low, borrow := bits.Sub64(id.low, value, 0)
	return bucketID{high: id.high - borrow, low: low}
}

func exactUnixNanoseconds(at time.Time) (int64, bool) {
	const (
		minimumSecond     = int64(-9_223_372_037)
		minimumNanosecond = 145_224_192
		maximumSecond     = int64(9_223_372_036)
		maximumNanosecond = 854_775_807
	)

	seconds := at.Unix()
	nanoseconds := at.Nanosecond()
	switch {
	case seconds < minimumSecond || seconds > maximumSecond:
		return 0, false
	case seconds == minimumSecond:
		if nanoseconds < minimumNanosecond {
			return 0, false
		}
		return math.MinInt64 + int64(nanoseconds-minimumNanosecond), true
	case seconds == maximumSecond:
		if nanoseconds > maximumNanosecond {
			return 0, false
		}
		return math.MaxInt64 - int64(maximumNanosecond-nanoseconds), true
	default:
		return seconds*int64(time.Second) + int64(nanoseconds), true
	}
}

func (w *Time) index(id bucketID) int {
	divisor := uint64(len(w.buckets))
	_, remainder := bits.Div64(id.high%divisor, id.low, divisor)
	if id.negative && remainder != 0 {
		remainder = divisor - remainder
	}
	return int(remainder)
}

func merge(destination *Snapshot, source Snapshot) {
	destination.Classified += source.Classified
	destination.Successes += source.Successes
	destination.Failures += source.Failures
	destination.Ignored += source.Ignored
	destination.SlowSuccess += source.SlowSuccess
	destination.SlowFailure += source.SlowFailure
}
