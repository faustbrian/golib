package bulkhead_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/bulkhead"
)

func TestImmediateAdmissionConservesCapacityAndRejectsOverflow(t *testing.T) {
	policy, err := bulkhead.New(bulkhead.Config{
		Resource:  "inventory-db",
		Capacity:  2,
		Admission: bulkhead.RejectImmediately{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	first, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	if got := policy.Snapshot(); got.ActiveWeight != 1 || got.Admissions != 1 {
		t.Fatalf("Snapshot() after first admission = %+v", got)
	}
	second, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
	if _, err := policy.Acquire(context.Background(), 1); !errors.Is(err, bulkhead.ErrRejected) {
		t.Fatalf("overflow Acquire() error = %v, want ErrRejected", err)
	}
	if got := policy.Snapshot(); got.Rejections != 1 || got.RejectionCounts[bulkhead.RejectionCapacity] != 1 {
		t.Fatalf("Snapshot() after rejection = %+v", got)
	}

	if err := first.Release(); err != nil {
		t.Fatalf("first Release() error = %v", err)
	}
	if got := policy.Snapshot(); got.ActiveWeight != 1 {
		t.Fatalf("Snapshot() after release = %+v", got)
	}
	replacement, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("replacement Acquire() error = %v", err)
	}
	if err := first.Release(); !errors.Is(err, bulkhead.ErrPermitReleased) {
		t.Fatalf("duplicate Release() error = %v, want ErrPermitReleased", err)
	}

	snapshot := policy.Snapshot()
	if snapshot.Capacity != 2 || snapshot.ActiveWeight != 2 || snapshot.AvailableWeight != 0 {
		t.Fatalf("Snapshot() capacity = %+v", snapshot)
	}

	if err := second.Release(); err != nil {
		t.Fatalf("second Release() error = %v", err)
	}
	if err := replacement.Release(); err != nil {
		t.Fatalf("replacement Release() error = %v", err)
	}
	if got := policy.Snapshot(); got.ActiveWeight != 0 || got.AvailableWeight != got.Capacity {
		t.Fatalf("final Snapshot() = %+v", got)
	}
}
