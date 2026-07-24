//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	temporalpostgres "github.com/faustbrian/golib/pkg/temporal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPostgreSQLRangeAndMultirangeRoundTrips(t *testing.T) {
	dsn := os.Getenv("TEMPORAL_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("TEMPORAL_POSTGRES_DSN is required for integration tests")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect(): %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(ctx); err != nil {
			t.Errorf("connection.Close(): %v", err)
		}
	})

	base := time.Date(2026, time.January, 2, 3, 4, 5, 123456000, time.UTC)
	for _, bounds := range temporal.AllBounds() {
		period, _ := instant.New(base, base.Add(24*time.Hour), bounds)
		value, err := temporalpostgres.InstantRangeValue(period)
		if err != nil {
			t.Fatalf("InstantRangeValue(%v): %v", bounds, err)
		}
		var returned pgtype.Range[time.Time]
		if err := connection.QueryRow(ctx, "select $1::tstzrange", value).Scan(&returned); err != nil {
			t.Fatalf("tstzrange round trip %v: %v", bounds, err)
		}
		decoded, err := temporalpostgres.InstantPeriod(returned)
		if err != nil || !decoded.SetEqual(period) || decoded.Bounds() != bounds {
			t.Fatalf("tstzrange decoded %v = %+v, %v", bounds, decoded, err)
		}
	}

	first, _ := instant.Range(base, base.Add(time.Hour))
	second, _ := instant.Range(base.Add(2*time.Hour), base.Add(3*time.Hour))
	set, _ := instant.NewSet(temporal.Limits{}, first, second)
	multirange, _ := temporalpostgres.InstantMultirangeValue(set)
	var returnedMultirange pgtype.Multirange[pgtype.Range[time.Time]]
	if err := connection.QueryRow(ctx, "select $1::tstzmultirange", multirange).Scan(&returnedMultirange); err != nil {
		t.Fatalf("tstzmultirange round trip: %v", err)
	}
	decodedSet, err := temporalpostgres.InstantSet(returnedMultirange, temporal.Limits{})
	if err != nil || !decodedSet.Equal(set) {
		t.Fatalf("tstzmultirange decoded = %+v, %v", decodedSet.Periods(), err)
	}

	dateValue, _ := dateperiod.New(
		calendar.MustDate(2026, time.January, 2),
		calendar.MustDate(2026, time.January, 4),
		temporal.Closed,
	)
	wrapper, err := temporalpostgres.NewDateRange(dateValue)
	if err != nil {
		t.Fatalf("NewDateRange(): %v", err)
	}
	encoded, err := wrapper.Value()
	if err != nil {
		t.Fatalf("DateRange.Value(): %v", err)
	}
	var returnedText string
	if err := connection.QueryRow(ctx, "select ($1::text::daterange)::text", encoded).Scan(&returnedText); err != nil {
		t.Fatalf("daterange round trip: %v", err)
	}
	var decodedDate temporalpostgres.DateRange
	if err := decodedDate.Scan(returnedText); err != nil {
		t.Fatalf("DateRange.Scan(): %v", err)
	}
	period, valid := decodedDate.Period()
	if !valid || !period.SetEqual(dateValue) {
		t.Fatalf("daterange decoded = %+v, %v", period, valid)
	}
}
