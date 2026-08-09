package workflow_test

import (
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func FuzzWorkLeaseBoundaries(fuzz *testing.F) {
	now := time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
	fuzz.Add("worker-1", uint64(1), uint32(1), int64(time.Minute))
	fuzz.Add("", uint64(0), uint32(0), int64(0))
	fuzz.Fuzz(func(t *testing.T, owner string, token uint64, attempt uint32, leaseNanos int64) {
		work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
			ID: "work-1", Kind: workflow.WorkActivity, InstanceID: "instance-1", Sequence: 1,
			AvailableAt: now, Deadline: now.Add(time.Hour), Payload: []byte("bounded"),
		})
		if err != nil {
			t.Fatalf("construct seed work: %v", err)
		}
		lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
			Work: work, Owner: owner, Token: token, Attempt: attempt,
			ClaimedAt: now, ExpiresAt: now.Add(time.Duration(leaseNanos)),
		})
		if err == nil && (!lease.Valid() || lease.Work().ID() != "work-1") {
			t.Fatal("accepted lease is not stable and valid")
		}
	})
}
