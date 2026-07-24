package postgres_test

import (
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/postgres"
	"github.com/jackc/pgx/v5/pgtype"
)

func FuzzInstantRangeSQLScan(f *testing.F) {
	f.Add("[\"2026-01-01T00:00:00Z\",\"2026-01-02T00:00:00Z\")")
	f.Add("empty")
	f.Add("bad")
	f.Fuzz(func(t *testing.T, input string) {
		var value postgres.InstantRange
		_ = value.Scan(input)
		_ = value.Scan([]byte(input))
	})
}

func FuzzDateRangeSQLScan(f *testing.F) {
	f.Add("[2026-01-01,2026-01-03)")
	f.Add("empty")
	f.Add(string([]byte{0xff}))
	f.Fuzz(func(t *testing.T, input string) {
		var value postgres.DateRange
		_ = value.Scan(input)
		_ = value.Scan([]byte(input))
	})
}

func FuzzInstantPGXMultirange(f *testing.F) {
	f.Add(int64(0), int64(1), int64(2), int64(3), uint8(0))
	f.Add(int64(3), int64(1), int64(-2), int64(4), uint8(255))
	f.Fuzz(func(t *testing.T, firstStart, firstEnd, secondStart, secondEnd int64, mode uint8) {
		bound := func(value uint8) pgtype.BoundType {
			if value%2 == 0 {
				return pgtype.Inclusive
			}
			return pgtype.Exclusive
		}
		atSecond := func(value int64) time.Time {
			return time.Unix(value%10_000_000, 0).UTC()
		}
		value := pgtype.Multirange[pgtype.Range[time.Time]]{
			{
				Lower: atSecond(firstStart), Upper: atSecond(firstEnd),
				LowerType: bound(mode), UpperType: bound(mode >> 1), Valid: true,
			},
			{
				Lower: atSecond(secondStart), Upper: atSecond(secondEnd),
				LowerType: bound(mode >> 2), UpperType: bound(mode >> 3), Valid: true,
			},
		}
		set, err := postgres.InstantSet(value, temporal.Limits{InputPeriods: 2, OutputPeriods: 2})
		if err != nil {
			return
		}
		roundTrip, err := postgres.InstantMultirangeValue(set)
		if err != nil {
			t.Fatalf("InstantMultirangeValue(): %v", err)
		}
		decoded, err := postgres.InstantSet(roundTrip, temporal.Limits{})
		if err != nil || !decoded.Equal(set) {
			t.Fatalf("multirange round trip = %+v, %v", decoded.Periods(), err)
		}
	})
}
