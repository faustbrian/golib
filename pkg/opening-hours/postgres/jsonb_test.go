package postgres_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
	"github.com/faustbrian/golib/pkg/opening-hours/postgres"
)

func testSchedule(t testing.TB) openinghours.Schedule {
	t.Helper()
	start, _ := openinghours.NewLocalTime(9, 0, 0, 0)
	end, _ := openinghours.NewLocalTime(12, 0, 0, 0)
	item, _ := openinghours.NewRange(start, end)
	rule, _ := openinghours.OpenRanges([]openinghours.Range{item}, openinghours.RejectOverlap)
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Weekly: map[time.Weekday]openinghours.DayRule{time.Monday: rule},
	})
	if err != nil {
		t.Fatal(err)
	}

	return schedule
}

func TestPGXNativeJSONBRoundTrip(t *testing.T) {
	want := testSchedule(t)
	typeMap := pgtype.NewMap()
	encodePlan := typeMap.PlanEncode(pgtype.JSONBOID, pgtype.TextFormatCode, want)
	if encodePlan == nil {
		t.Fatal("pgx did not find a native JSONB encode plan")
	}
	encoded, err := encodePlan.Encode(want, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got openinghours.Schedule
	scanPlan := typeMap.PlanScan(pgtype.JSONBOID, pgtype.TextFormatCode, &got)
	if scanPlan == nil {
		t.Fatal("pgx did not find a native JSONB scan plan")
	}
	if err := scanPlan.Scan(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !want.Equal(got) {
		t.Fatal("pgx round trip changed schedule")
	}
}

func TestNullableJSONB(t *testing.T) {
	want := testSchedule(t)
	value := postgres.New(want)
	driverValue, err := value.Value()
	if err != nil || driverValue == nil {
		t.Fatalf("Value() = %v, error=%v", driverValue, err)
	}

	var decoded postgres.JSONB
	if err := decoded.Scan(driverValue); err != nil {
		t.Fatal(err)
	}
	got, valid := decoded.Get()
	if !valid || !want.Equal(got) {
		t.Fatalf("Get() valid=%t schedule=%v", valid, got)
	}
	if err := decoded.Scan(nil); err != nil {
		t.Fatal(err)
	}
	_, valid = decoded.Get()
	if valid {
		t.Fatal("Scan(nil) remained valid")
	}
	driverValue, err = decoded.Value()
	if err != nil || driverValue != nil {
		t.Fatalf("invalid Value() = %v error=%v", driverValue, err)
	}
	if err := decoded.Scan(42); err == nil {
		t.Fatal("Scan accepted unsupported value")
	}
	var pointer *postgres.JSONB
	if err := pointer.Scan(driverValue); err == nil {
		t.Fatal("nil JSONB receiver succeeded")
	}
}
