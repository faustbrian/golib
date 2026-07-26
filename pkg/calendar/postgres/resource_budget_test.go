package postgres_test

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendarpg "github.com/faustbrian/golib/pkg/calendar/postgres"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	budgetPGXBytes []byte
	budgetPGXErr   error
)

func TestPGXBinaryEncodingAllocationBudget(t *testing.T) {
	codec := pgtype.NewMap()
	value := calendarpg.NewDate(calendar.MustDate(2024, time.February, 29))
	if allocations := testing.AllocsPerRun(1_000, func() {
		budgetPGXBytes, budgetPGXErr = codec.Encode(
			pgtype.DateOID, pgtype.BinaryFormatCode, value, nil,
		)
	}); allocations > 2 {
		t.Fatalf("pgx binary encode allocations = %.0f, budget 2", allocations)
	}
}
