package timezone_test

import (
	"testing"
	"time"

	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
)

func TestTimezoneConversionsDifferentialAgainstStandardLibrary(t *testing.T) {
	t.Parallel()

	zones := []string{
		"UTC",
		"America/New_York",
		"US/Eastern",
		"Europe/Dublin",
		"Africa/Monrovia",
		"Asia/Kathmandu",
		"Australia/Lord_Howe",
		"Pacific/Apia",
		"Pacific/Kwajalein",
	}
	for _, zone := range zones {
		zone := zone
		t.Run(zone, func(t *testing.T) {
			t.Parallel()
			location, err := calendartz.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			for year := 1900; year <= 2030; year++ {
				for month := time.January; month <= time.December; month++ {
					instant := time.Date(year, month, 15, 12, 34, 56, 789, time.UTC)
					assertStandardLibraryRoundTrip(t, instant, location)
				}
			}
		})
	}
}

func assertStandardLibraryRoundTrip(t *testing.T, instant time.Time, location *time.Location) {
	t.Helper()

	want := instant.In(location)
	local, err := calendartz.FromInstant(instant, location)
	if err != nil {
		t.Fatalf("FromInstant(%s, %s): %v", instant, location, err)
	}
	wantYear, wantMonth, wantDay := want.Date()
	if local.Date().Year() != wantYear || local.Date().Month() != wantMonth ||
		local.Date().Day() != wantDay || local.Hour() != want.Hour() ||
		local.Minute() != want.Minute() || local.Second() != want.Second() ||
		local.Nanosecond() != want.Nanosecond() {
		t.Fatalf("local differential mismatch: got %#v, want %s", local, want)
	}
	_, offset := want.Zone()
	resolved, err := calendartz.Resolve(local, location, calendartz.MatchOffset(offset))
	if err != nil || !resolved.Equal(instant) {
		t.Fatalf("Resolve(%#v, %s, %d) = %s, %v; want %s", local, location, offset, resolved, err, instant)
	}
}
