package postgres_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func FuzzPGXJSONBCodec(f *testing.F) {
	schedule := testSchedule(f)
	canonical, _ := schedule.CanonicalJSON()
	f.Add(canonical)
	f.Add([]byte(`{"version":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		typeMap := pgtype.NewMap()
		var decoded openinghours.Schedule
		scanPlan := typeMap.PlanScan(pgtype.JSONBOID, pgtype.TextFormatCode, &decoded)
		if scanPlan == nil {
			t.Fatal("pgx did not find a JSONB scan plan")
		}
		if err := scanPlan.Scan(data, &decoded); err != nil {
			return
		}
		encodePlan := typeMap.PlanEncode(pgtype.JSONBOID, pgtype.TextFormatCode, decoded)
		if encodePlan == nil {
			t.Fatal("pgx did not find a JSONB encode plan")
		}
		encoded, err := encodePlan.Encode(decoded, nil)
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip openinghours.Schedule
		if err := scanPlan.Scan(encoded, &roundTrip); err != nil || !decoded.Equal(roundTrip) {
			t.Fatalf("pgx accepted value did not round trip: %v", err)
		}
	})
}
