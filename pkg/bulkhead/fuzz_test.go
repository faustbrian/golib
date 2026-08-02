package bulkhead_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/bulkhead"
)

func FuzzConfigurationNeverPanics(fuzz *testing.F) {
	fuzz.Add("database", int64(1), 1, int64(time.Millisecond))
	fuzz.Add("", int64(0), 0, int64(0))
	fuzz.Fuzz(func(t *testing.T, resource string, capacity int64, queue int, waitNanos int64) {
		policy, err := bulkhead.New(bulkhead.Config{
			Resource: resource,
			Capacity: capacity,
			Admission: bulkhead.Wait{
				MaxQueued: queue,
				MaxWait:   time.Duration(waitNanos),
			},
		})
		if err != nil {
			if !errors.Is(err, bulkhead.ErrInvalidConfig) {
				t.Fatalf("New() error = %v", err)
			}
			return
		}
		snapshot := policy.Snapshot()
		if snapshot.Capacity <= 0 || len(snapshot.Resource) == 0 ||
			len(snapshot.Resource) > bulkhead.MaxResourceBytes {
			t.Fatalf("accepted unbounded configuration: %+v", snapshot)
		}
	})
}

func FuzzPermitHistoriesConserveCapacity(fuzz *testing.F) {
	fuzz.Add([]byte{0, 1, 2, 3, 4, 5})
	fuzz.Add([]byte{255, 0, 255})
	fuzz.Fuzz(func(t *testing.T, history []byte) {
		if len(history) > 256 {
			history = history[:256]
		}
		policy := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 4})
		var permits []*bulkhead.Permit
		for _, action := range history {
			if action%3 == 0 && len(permits) != 0 {
				index := int(action) % len(permits)
				_ = permits[index].Release()
				permits[index] = permits[len(permits)-1]
				permits = permits[:len(permits)-1]
			} else {
				weight := int64(action%4 + 1)
				permit, err := policy.Acquire(context.Background(), weight)
				if err == nil {
					permits = append(permits, permit)
				} else if !errors.Is(err, bulkhead.ErrRejected) {
					t.Fatalf("Acquire() error = %v", err)
				}
			}
			snapshot := policy.Snapshot()
			if snapshot.ActiveWeight < 0 || snapshot.AvailableWeight < 0 ||
				snapshot.ActiveWeight+snapshot.AvailableWeight != snapshot.Capacity {
				t.Fatalf("capacity not conserved: %+v", snapshot)
			}
		}
		for _, permit := range permits {
			_ = permit.Release()
		}
	})
}
