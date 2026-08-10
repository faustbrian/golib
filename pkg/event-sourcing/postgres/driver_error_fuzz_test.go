package postgres

import (
	"errors"
	"strings"
	"testing"
)

func FuzzDatabaseFailure(f *testing.F) {
	f.Add("driver detail with private data")
	f.Add("")
	f.Add(strings.Repeat("x", 64<<10))

	f.Fuzz(func(t *testing.T, diagnostic string) {
		if len(diagnostic) > 1<<20 {
			return
		}
		cause := driverDiagnosticError(diagnostic)
		err := databaseFailure(cause)
		if err.Error() != ErrDatabaseOperationFailed.Error() ||
			!errors.Is(err, ErrDatabaseOperationFailed) ||
			!errors.Is(err, cause) {
			t.Fatalf("database failure classification = %q", err)
		}
	})
}
