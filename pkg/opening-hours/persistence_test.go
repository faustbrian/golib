package openinghours_test

import (
	"database/sql/driver"
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func TestSQLValueAndScanRoundTrip(t *testing.T) {
	schedule := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	value, err := schedule.Value()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := value.([]byte); !ok {
		t.Fatalf("Value() type = %T, want []byte", value)
	}

	var decoded openinghours.Schedule
	if err := decoded.Scan(value); err != nil {
		t.Fatal(err)
	}
	if !schedule.Equal(decoded) {
		t.Fatal("database/sql round trip changed schedule")
	}

	var _ driver.Valuer = schedule
}

func TestSQLScanCopiesBytesAndHandlesNull(t *testing.T) {
	schedule := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	encoded, _ := schedule.CanonicalJSON()
	var decoded openinghours.Schedule
	if err := decoded.Scan(encoded); err != nil {
		t.Fatal(err)
	}
	for index := range encoded {
		encoded[index] = 'x'
	}
	result, err := decoded.IsOpenLocal(
		openinghours.MustDate(2026, time.January, 5), mustTime(t, 10, 0),
		openinghours.RejectDST,
	)
	if err != nil || !result.Open {
		t.Fatalf("scanned value aliased bytes: result=%#v error=%v", result, err)
	}

	if err := decoded.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if decoded.Timezone() != "" {
		t.Fatalf("Scan(nil) timezone = %q, want zero schedule", decoded.Timezone())
	}
	if err := decoded.Scan(42); !openinghours.IsCode(err, openinghours.CodeInvalidEncoding) {
		t.Fatalf("Scan(int) error = %v, want invalid encoding", err)
	}
}
