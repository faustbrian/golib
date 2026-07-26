package openinghours_test

import (
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func TestExactLocalConversionsDifferentialAgainstGoTimezone(t *testing.T) {
	zones := []string{
		"UTC",
		"Europe/Helsinki",
		"America/New_York",
		"Australia/Lord_Howe",
		"Pacific/Apia",
	}
	years := []int{1900, 1970, 2011, 2024, 2026}
	hours := []int{0, 1, 2, 3, 6, 12, 18, 23}

	for _, zone := range zones {
		location, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		schedule, err := openinghours.NewSchedule(openinghours.Config{Timezone: zone})
		if err != nil {
			t.Fatal(err)
		}
		for _, year := range years {
			for month := time.January; month <= time.December; month++ {
				for _, day := range []int{1, 8, 15, 22, 28} {
					date, err := openinghours.NewDate(year, month, day)
					if err != nil {
						continue
					}
					for _, hour := range hours {
						local, err := openinghours.NewLocalTime(hour, 17, 23, 456789)
						if err != nil {
							t.Fatal(err)
						}
						resolved, err := schedule.ResolveLocal(date, local, openinghours.RejectDST)
						if openinghours.IsCode(err, openinghours.CodeAmbiguousLocalTime) ||
							openinghours.IsCode(err, openinghours.CodeNonexistentLocalTime) {
							continue
						}
						if err != nil {
							t.Fatalf("ResolveLocal(%s %s %02d:17) error = %v", zone, date, hour, err)
						}
						expected := time.Date(year, month, day, hour, 17, 23, 456789, location)
						if resolved.Kind != openinghours.LocalExact || !resolved.Instant.Equal(expected) {
							t.Fatalf(
								"ResolveLocal(%s %s %02d:17) = %#v, Go=%s",
								zone, date, hour, resolved, expected,
							)
						}
					}
				}
			}
		}
	}
}
