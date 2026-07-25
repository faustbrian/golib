package window_test

import (
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/circuit-breaker/window"
)

func FuzzCountMatchesBoundedReference(f *testing.F) {
	f.Add(uint8(3), []byte{0, 1, 2, 1, 0})
	f.Fuzz(func(t *testing.T, rawSize uint8, outcomes []byte) {
		size := int(rawSize%32) + 1
		if len(outcomes) > 4096 {
			t.Skip()
		}
		actual, err := window.NewCount(size)
		if err != nil {
			t.Fatalf("NewCount() error = %v", err)
		}
		var classified []window.Record
		var ignored uint64
		for _, raw := range outcomes {
			record := window.Record{Class: window.Class(raw % 3), Slow: raw&4 != 0}
			if err := actual.Add(record); err != nil {
				t.Fatalf("Add() error = %v", err)
			}
			if record.Class == window.Ignored {
				ignored++
				continue
			}
			classified = append(classified, record)
			if len(classified) > size {
				classified = classified[1:]
			}
		}
		want := window.Snapshot{Ignored: ignored}
		for _, record := range classified {
			want.Classified++
			if record.Class == window.Success {
				want.Successes++
				if record.Slow {
					want.SlowSuccess++
				}
			} else {
				want.Failures++
				if record.Slow {
					want.SlowFailure++
				}
			}
		}
		if got := actual.Snapshot(); got != want {
			t.Fatalf("Snapshot() = %+v, want %+v", got, want)
		}
	})
}

func FuzzTimeWindowTimestamps(f *testing.F) {
	f.Add(
		uint8(4),
		int64(time.Second),
		[]byte{0, 1, 2, 5},
		[]byte{
			0, 0, 0, 0, 0, 0, 0, 0,
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f,
			0, 0, 0, 0, 0, 0, 0, 0x80,
		},
	)
	f.Fuzz(func(t *testing.T, rawBuckets uint8, rawDuration int64, outcomes []byte, timestamps []byte) {
		buckets := int(rawBuckets%32) + 1
		duration := time.Duration(rawDuration % int64(time.Hour))
		if duration <= 0 {
			duration = time.Nanosecond
		}
		if len(outcomes) > 1024 || len(timestamps) > 8192 {
			t.Skip()
		}
		actual, err := window.NewTime(duration, buckets)
		if err != nil {
			t.Fatalf("NewTime() error = %v", err)
		}
		reference := newTimeReference(duration, buckets)
		for index, raw := range outcomes {
			seconds := int64(index)
			if len(timestamps) >= 8 {
				start := (index * 8) % (len(timestamps) - 7)
				seconds = int64(binary.LittleEndian.Uint64(timestamps[start : start+8]))
			}
			at := time.Unix(seconds, int64(raw)*3_906_250)
			record := window.Record{Class: window.Class(raw % 3), Slow: raw&4 != 0}
			if raw&8 == 0 {
				if err := actual.Add(at, record); err != nil {
					t.Fatalf("Add() error = %v", err)
				}
				reference.add(at, record)
			}
			if got, want := actual.Snapshot(at), reference.snapshot(at); got != want {
				t.Fatalf("operation %d at %v: Snapshot() = %+v, want %+v", index, at, got, want)
			}
		}
	})
}

type referenceTimeBucket struct {
	id       *big.Int
	snapshot window.Snapshot
}

type timeReference struct {
	duration *big.Int
	count    int
	last     *big.Int
	buckets  map[string]referenceTimeBucket
}

func newTimeReference(duration time.Duration, count int) *timeReference {
	return &timeReference{
		duration: big.NewInt(int64(duration)),
		count:    count,
		buckets:  make(map[string]referenceTimeBucket),
	}
}

func (r *timeReference) add(at time.Time, record window.Record) {
	id := r.observe(at)
	key := id.String()
	bucket := r.buckets[key]
	if bucket.id == nil {
		bucket.id = new(big.Int).Set(id)
	}
	addReferenceRecord(&bucket.snapshot, record)
	r.buckets[key] = bucket
}

func (r *timeReference) snapshot(at time.Time) window.Snapshot {
	current := r.observe(at)
	oldest := new(big.Int).Sub(current, big.NewInt(int64(r.count-1)))
	var result window.Snapshot
	for _, bucket := range r.buckets {
		if bucket.id.Cmp(oldest) >= 0 && bucket.id.Cmp(current) <= 0 {
			mergeReferenceSnapshot(&result, bucket.snapshot)
		}
	}
	return result
}

func (r *timeReference) observe(at time.Time) *big.Int {
	id := referenceBucketID(at, r.duration)
	if r.last == nil || id.Cmp(r.last) > 0 {
		r.last = new(big.Int).Set(id)
	}
	return new(big.Int).Set(r.last)
}

func referenceBucketID(at time.Time, duration *big.Int) *big.Int {
	nanoseconds := new(big.Int).Mul(big.NewInt(at.Unix()), big.NewInt(int64(time.Second)))
	nanoseconds.Add(nanoseconds, big.NewInt(int64(at.Nanosecond())))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(nanoseconds, duration, remainder)
	if nanoseconds.Sign() < 0 && remainder.Sign() != 0 {
		quotient.Sub(quotient, big.NewInt(1))
	}
	return quotient
}

func addReferenceRecord(snapshot *window.Snapshot, record window.Record) {
	if record.Class == window.Ignored {
		snapshot.Ignored++
		return
	}
	snapshot.Classified++
	if record.Class == window.Success {
		snapshot.Successes++
		if record.Slow {
			snapshot.SlowSuccess++
		}
		return
	}
	snapshot.Failures++
	if record.Slow {
		snapshot.SlowFailure++
	}
}

func mergeReferenceSnapshot(destination *window.Snapshot, source window.Snapshot) {
	destination.Classified += source.Classified
	destination.Successes += source.Successes
	destination.Failures += source.Failures
	destination.Ignored += source.Ignored
	destination.SlowSuccess += source.SlowSuccess
	destination.SlowFailure += source.SlowFailure
}
