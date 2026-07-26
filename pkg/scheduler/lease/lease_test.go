package lease_test

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
)

func TestLeaseExpiryBoundary(t *testing.T) {
	t.Parallel()

	expires := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	owned := lease.Lease{ExpiresAt: expires}
	if owned.Expired(expires.Add(-time.Nanosecond)) {
		t.Fatal("lease expired before boundary")
	}
	if !owned.Expired(expires) || !owned.Expired(expires.Add(time.Nanosecond)) {
		t.Fatal("lease did not expire at boundary")
	}
}
