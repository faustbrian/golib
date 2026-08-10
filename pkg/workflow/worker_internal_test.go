package workflow

import (
	"testing"
	"time"
)

func TestWorkDecisionCompleteValidityRequiresEmptyMetadata(t *testing.T) {
	if !(WorkDecision{kind: WorkComplete}).Valid() {
		t.Fatal("plain completion was rejected")
	}
	if (WorkDecision{kind: WorkComplete, code: "unexpected"}).Valid() {
		t.Fatal("completion accepted a failure code")
	}
	if (WorkDecision{kind: WorkComplete, retryAt: time.Unix(1, 0)}).Valid() {
		t.Fatal("completion accepted a retry time")
	}
}

func TestFairLeaseOrderRoundRobinsTenantsWithoutReorderingWithinTenant(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	leases := []WorkLease{
		mustInternalWorkerLease(t, now, "work-1", "tenant-1"),
		mustInternalWorkerLease(t, now, "work-2", "tenant-1"),
		mustInternalWorkerLease(t, now, "work-3", "tenant-2"),
		mustInternalWorkerLease(t, now, "work-4", "tenant-1"),
	}
	ordered := fairLeaseOrder(leases)
	want := []string{"work-1", "work-3", "work-2", "work-4"}
	for index, id := range want {
		if ordered[index].Work().ID() != id {
			t.Fatalf("fair order[%d] = %s, want %s", index, ordered[index].Work().ID(), id)
		}
	}
	if fairLeaseOrder(nil) != nil {
		t.Fatal("nil fair order did not remain nil")
	}
	shortTenantFirst := []WorkLease{
		mustInternalWorkerLease(t, now, "short-1", "tenant-short"),
		mustInternalWorkerLease(t, now, "long-1", "tenant-long"),
		mustInternalWorkerLease(t, now, "long-2", "tenant-long"),
	}
	ordered = fairLeaseOrder(shortTenantFirst)
	want = []string{"short-1", "long-1", "long-2"}
	for index, id := range want {
		if ordered[index].Work().ID() != id {
			t.Fatalf("short-first order[%d] = %s, want %s", index, ordered[index].Work().ID(), id)
		}
	}
}

func mustInternalWorkerLease(t *testing.T, now time.Time, id, tenant string) WorkLease {
	t.Helper()
	work, err := NewPendingWork(PendingWorkSpec{
		ID: id, Kind: WorkActivity, InstanceID: "instance-" + id, Sequence: 1,
		AvailableAt: now, Deadline: now.Add(time.Hour), TenantID: tenant,
	})
	if err != nil {
		t.Fatalf("construct work: %v", err)
	}
	lease, err := NewWorkLease(WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct lease: %v", err)
	}
	return lease
}
