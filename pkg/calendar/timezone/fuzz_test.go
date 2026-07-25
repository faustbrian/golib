package timezone_test

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
)

func FuzzLoadLocation(f *testing.F) {
	for _, seed := range []string{"UTC", "America/New_York", "Pacific/Apia", "../UTC", string([]byte{0xff})} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		location, err := calendartz.LoadLocation(name)
		if err == nil && (location == nil || location.String() != name) {
			t.Fatalf("location identity = %v for %q", location, name)
		}
	})
}

func FuzzResolveLocalDateTime(f *testing.F) {
	f.Add(2024, 11, 3, 1, 30, 0, 0, uint8(2), uint8(calendartz.Earlier))
	zones := []string{"UTC", "America/New_York", "Europe/Helsinki", "Pacific/Apia"}
	f.Fuzz(func(t *testing.T, year, month, day, hour, minute, second, nanosecond int, zoneIndex, rawPolicy uint8) {
		date, err := calendar.NewDate(year, time.Month(month), day)
		if err != nil {
			return
		}
		local, err := calendartz.NewLocalDateTime(date, hour, minute, second, nanosecond)
		if err != nil {
			return
		}
		location, err := calendartz.LoadLocation(zones[int(zoneIndex)%len(zones)])
		if err != nil {
			t.Fatal(err)
		}
		instant, err := calendartz.Resolve(local, location, calendartz.Choice(rawPolicy))
		if err != nil {
			return
		}
		roundTrip, err := calendartz.FromInstant(instant, location)
		if err != nil || roundTrip != local {
			t.Fatalf("local round trip = %#v, %v", roundTrip, err)
		}
	})
}
