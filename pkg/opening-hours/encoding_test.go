package openinghours_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func TestCanonicalJSONIsStableAndRoundTrips(t *testing.T) {
	monday, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 13, 0, 17, 0), mustRange(t, 9, 0, 12, 0),
	}, openinghours.RejectOverlap)
	date := openinghours.MustDate(2026, time.December, 24)
	closure, _ := openinghours.NewException(openinghours.ExceptionConfig{
		Date: date, Operation: openinghours.ExceptionClose,
		Priority: 100, Source: "holiday", Revision: "2026",
	})
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone:   "Europe/Helsinki",
		Weekly:     map[time.Weekday]openinghours.DayRule{time.Monday: monday},
		Exceptions: []openinghours.Exception{closure},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := schedule.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	second, err := schedule.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical output changed:\n%s\n%s", first, second)
	}
	decoded, err := openinghours.ParseJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	if !schedule.Equal(decoded) || schedule.Hash() != decoded.Hash() {
		t.Fatalf("round trip changed schedule: %s", first)
	}

	marshaled, err := json.Marshal(schedule)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, marshaled) {
		t.Fatalf("MarshalJSON() = %s, want %s", marshaled, first)
	}
}

func TestScheduleCompareUsesCanonicalOrdering(t *testing.T) {
	first := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	second := scheduleWithMonday(t, mustRange(t, 10, 0, 12, 0))

	forward, err := first.Compare(second)
	if err != nil || forward >= 0 {
		t.Fatalf("first.Compare(second) = %d, %v", forward, err)
	}
	reverse, err := second.Compare(first)
	if err != nil || reverse <= 0 {
		t.Fatalf("second.Compare(first) = %d, %v", reverse, err)
	}
	equal, err := first.Compare(first)
	if err != nil || equal != 0 {
		t.Fatalf("first.Compare(first) = %d, %v", equal, err)
	}
}

func TestStrictJSONRejectsHostileStructure(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"duplicate", []byte(`{"version":1,"version":1,"timezone":"UTC","weekly":[],"exceptions":[]}`)},
		{"unknown", []byte(`{"version":1,"timezone":"UTC","weekly":[],"exceptions":[],"unknown":true}`)},
		{"trailing", []byte(`{"version":1,"timezone":"UTC","weekly":[],"exceptions":[]} true`)},
		{"invalid utf8", append([]byte(`{"version":1,"timezone":"`), 0xff)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := openinghours.ParseJSON(test.data)
			if !openinghours.IsCode(err, openinghours.CodeInvalidEncoding) {
				t.Fatalf("ParseJSON() error = %v, want invalid encoding", err)
			}
		})
	}
}

func TestCanonicalJSONPreservesComposition(t *testing.T) {
	left := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	right := scheduleWithMonday(t, mustRange(t, 11, 0, 14, 0))
	combined, err := left.Intersection(right)
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := combined.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := openinghours.ParseJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	date := openinghours.MustDate(2026, time.January, 5)
	for _, hour := range []int{10, 11, 13} {
		want, _ := combined.IsOpenLocal(date, mustTime(t, hour, 0), openinghours.RejectDST)
		got, queryErr := decoded.IsOpenLocal(date, mustTime(t, hour, 0), openinghours.RejectDST)
		if queryErr != nil || got.Open != want.Open {
			t.Fatalf("decoded at %d = %#v, error=%v, want %#v", hour, got, queryErr, want)
		}
	}
}
