package postgres

import (
	"errors"
	"strings"
	"testing"
)

var databaseFailureSink error

func TestDatabaseFailureRedactsWithoutDiagnosticSizedAllocation(t *testing.T) {
	if databaseFailure(nil) != nil {
		t.Fatal("databaseFailure(nil) returned an error")
	}
	cause := driverDiagnosticError("driver detail with private data")
	err := databaseFailure(cause)
	assertDriverErrorRedacted(t, err, cause)
	if err.Error() != ErrDatabaseOperationFailed.Error() {
		t.Fatalf("database failure error = %q", err)
	}

	small := driverDiagnosticError("x")
	large := driverDiagnosticError(strings.Repeat("x", 1<<20))
	smallAllocations := testing.AllocsPerRun(100, func() {
		databaseFailureSink = databaseFailure(small)
	})
	largeAllocations := testing.AllocsPerRun(100, func() {
		databaseFailureSink = databaseFailure(large)
	})
	if smallAllocations > 2 || largeAllocations > 2 {
		t.Fatalf(
			"database failure allocations small/large = %.2f/%.2f",
			smallAllocations,
			largeAllocations,
		)
	}
}

type driverDiagnosticError string

func (err driverDiagnosticError) Error() string {
	return string(err)
}

var _ error = driverDiagnosticError("")

func TestDatabaseFailurePreservesStandardErrorCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("driver detail")
	assertDriverErrorRedacted(t, databaseFailure(cause), cause)
}
