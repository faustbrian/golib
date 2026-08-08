package encoding_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
	openinghoursencoding "github.com/faustbrian/golib/pkg/opening-hours/encoding"
)

func TestLocationCompatibilityFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/location.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Timezone string                                 `json:"timezone"`
		Days     map[string][]openinghoursencoding.Slot `json:"opening_hours"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	schedule, err := openinghoursencoding.ImportLocation(
		fixture.Timezone, fixture.Days, openinghoursencoding.DefaultImportLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	monday := openinghours.MustDate(2026, time.January, 5)
	for _, test := range []struct {
		hour int
		open bool
	}{{9, true}, {12, false}, {13, true}, {17, false}} {
		local, _ := openinghours.NewLocalTime(test.hour, 0, 0, 0)
		result, queryErr := schedule.IsOpenLocal(monday, local, openinghours.RejectDST)
		if queryErr != nil || result.Open != test.open {
			t.Fatalf("Monday %d = %#v error=%v", test.hour, result, queryErr)
		}
	}
}

func TestTrackAndPostalSharedLocationFixtures(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		date     openinghours.Date
		hour     int
		wantOpen bool
	}{
		{name: "track opening", file: "track.json", date: openinghours.MustDate(2026, time.January, 5), hour: 8, wantOpen: true},
		{name: "track closing", file: "track.json", date: openinghours.MustDate(2026, time.January, 5), hour: 16},
		{name: "postal owner day", file: "postal.json", date: openinghours.MustDate(2026, time.January, 4), hour: 23, wantOpen: true},
		{name: "postal spillover", file: "postal.json", date: openinghours.MustDate(2026, time.January, 5), hour: 1, wantOpen: true},
		{name: "postal closing", file: "postal.json", date: openinghours.MustDate(2026, time.January, 5), hour: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + test.file)
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Timezone string                                 `json:"timezone"`
				Days     map[string][]openinghoursencoding.Slot `json:"opening_hours"`
			}
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatal(err)
			}
			schedule, err := openinghoursencoding.ImportLocation(
				fixture.Timezone, fixture.Days, openinghoursencoding.DefaultImportLimits(),
			)
			if err != nil {
				t.Fatal(err)
			}
			local, _ := openinghours.NewLocalTime(test.hour, 0, 0, 0)
			result, err := schedule.IsOpenLocal(test.date, local, openinghours.RejectDST)
			if err != nil || result.Open != test.wantOpen {
				t.Fatalf("IsOpenLocal() = %#v, %v; want open=%t", result, err, test.wantOpen)
			}
		})
	}
}

func TestSpatieCompatibilityFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/spatie.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Timezone string              `json:"timezone"`
		Days     map[string][]string `json:"days"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	schedule, err := openinghoursencoding.ImportSpatie(
		fixture.Timezone, fixture.Days, openinghoursencoding.DefaultImportLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	saturday := openinghours.MustDate(2026, time.January, 10)
	local, _ := openinghours.NewLocalTime(12, 0, 0, 0)
	result, err := schedule.IsOpenLocal(saturday, local, openinghours.RejectDST)
	if err != nil || !result.Open {
		t.Fatalf("Saturday noon = %#v error=%v", result, err)
	}
}

func TestCanonicalWrappersAndImportFailures(t *testing.T) {
	schedule, _ := openinghours.NewSchedule(openinghours.Config{Timezone: "UTC"})
	data, err := openinghoursencoding.Marshal(schedule)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := openinghoursencoding.Unmarshal(data)
	if err != nil || !schedule.Equal(decoded) {
		t.Fatalf("canonical wrappers error=%v", err)
	}
	if _, err := openinghoursencoding.Unmarshal([]byte(`{}`)); err == nil {
		t.Fatal("Unmarshal accepted invalid bytes")
	}

	limits := openinghoursencoding.DefaultImportLimits()
	failures := []struct {
		name   string
		days   map[string][]openinghoursencoding.Slot
		limits openinghoursencoding.ImportLimits
	}{
		{"zero limits", map[string][]openinghoursencoding.Slot{}, openinghoursencoding.ImportLimits{}},
		{"too many days", map[string][]openinghoursencoding.Slot{"monday": nil, "tuesday": nil}, openinghoursencoding.ImportLimits{MaximumDays: 1, MaximumRangesPerDay: 1}},
		{"weekday", map[string][]openinghoursencoding.Slot{"nonday": nil}, limits},
		{"range count", map[string][]openinghoursencoding.Slot{"monday": {{From: "01:00", To: "02:00"}, {From: "03:00", To: "04:00"}}}, openinghoursencoding.ImportLimits{MaximumDays: 7, MaximumRangesPerDay: 1}},
		{"start", map[string][]openinghoursencoding.Slot{"monday": {{From: "bad", To: "02:00"}}}, limits},
		{"end", map[string][]openinghoursencoding.Slot{"monday": {{From: "01:00", To: "bad"}}}, limits},
		{"overlap", map[string][]openinghoursencoding.Slot{"monday": {{From: "01:00", To: "03:00"}, {From: "02:00", To: "04:00"}}}, limits},
	}
	for _, failure := range failures {
		t.Run(failure.name, func(t *testing.T) {
			if _, importErr := openinghoursencoding.ImportLocation("UTC", failure.days, failure.limits); importErr == nil {
				t.Fatal("ImportLocation succeeded")
			}
		})
	}
	if _, err := openinghoursencoding.ImportLocation("Bad/Zone", map[string][]openinghoursencoding.Slot{}, limits); err == nil {
		t.Fatal("ImportLocation accepted bad timezone")
	}
	if _, err := openinghoursencoding.ImportSpatie("UTC", map[string][]string{"monday": {"bad"}}, limits); err == nil {
		t.Fatal("ImportSpatie accepted bad range")
	}
	if text := (&openinghoursencoding.ImportError{Kind: "time"}).Error(); text != "opening hours import: time" {
		t.Fatalf("ImportError text = %q", text)
	}
}

func TestImportLocationLimitBoundaries(t *testing.T) {
	validLimits := openinghoursencoding.DefaultImportLimits()
	for _, test := range []struct {
		name   string
		limits openinghoursencoding.ImportLimits
	}{
		{name: "negative maximum days", limits: openinghoursencoding.ImportLimits{MaximumDays: -1, MaximumRangesPerDay: 64}},
		{name: "zero maximum days", limits: openinghoursencoding.ImportLimits{MaximumDays: 0, MaximumRangesPerDay: 64}},
		{name: "maximum days above package bound", limits: openinghoursencoding.ImportLimits{MaximumDays: 8, MaximumRangesPerDay: 64}},
		{name: "negative maximum ranges", limits: openinghoursencoding.ImportLimits{MaximumDays: 7, MaximumRangesPerDay: -1}},
		{name: "zero maximum ranges", limits: openinghoursencoding.ImportLimits{MaximumDays: 7, MaximumRangesPerDay: 0}},
		{name: "maximum ranges above package bound", limits: openinghoursencoding.ImportLimits{MaximumDays: 7, MaximumRangesPerDay: 65}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := openinghoursencoding.ImportLocation("UTC", nil, test.limits)
			assertImportErrorKind(t, err, "limit")
		})
	}

	sevenDays := map[string][]openinghoursencoding.Slot{
		"sunday": nil, "monday": nil, "tuesday": nil, "wednesday": nil,
		"thursday": nil, "friday": nil, "saturday": nil,
	}
	if _, err := openinghoursencoding.ImportLocation("UTC", sevenDays, validLimits); err != nil {
		t.Fatalf("ImportLocation rejected exact maximum day count: %v", err)
	}

	sixtyFourRanges := make([]openinghoursencoding.Slot, 64)
	for index := range sixtyFourRanges {
		startMinute := index * 20
		endMinute := startMinute + 10
		sixtyFourRanges[index] = openinghoursencoding.Slot{
			From: fmt.Sprintf("%02d:%02d", startMinute/60, startMinute%60),
			To:   fmt.Sprintf("%02d:%02d", endMinute/60, endMinute%60),
		}
	}
	if _, err := openinghoursencoding.ImportLocation(
		"UTC",
		map[string][]openinghoursencoding.Slot{"monday": sixtyFourRanges},
		validLimits,
	); err != nil {
		t.Fatalf("ImportLocation rejected exact maximum range count: %v", err)
	}
}

func TestImportSpatieRejectsAmbiguousSeparatorsAsRanges(t *testing.T) {
	limits := openinghoursencoding.DefaultImportLimits()
	for _, input := range []string{"01:00", "01:00-02:00-03:00"} {
		t.Run(input, func(t *testing.T) {
			_, err := openinghoursencoding.ImportSpatie(
				"UTC",
				map[string][]string{"monday": {input}},
				limits,
			)
			assertImportErrorKind(t, err, "range")
		})
	}
}

func assertImportErrorKind(t *testing.T, err error, want string) {
	t.Helper()
	var importErr *openinghoursencoding.ImportError
	if !errors.As(err, &importErr) {
		t.Fatalf("error = %v; want ImportError kind %q", err, want)
	}
	if importErr.Kind != want {
		t.Fatalf("ImportError kind = %q; want %q", importErr.Kind, want)
	}
}
