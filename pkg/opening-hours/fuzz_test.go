package openinghours_test

import (
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func FuzzParseJSON(f *testing.F) {
	schedule, _ := openinghours.NewSchedule(openinghours.Config{Timezone: "UTC"})
	canonical, _ := schedule.CanonicalJSON()
	f.Add(canonical)
	f.Add([]byte(`{"version":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		parsed, err := openinghours.ParseJSON(data)
		if err != nil {
			return
		}
		encoded, err := parsed.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		reparsed, err := openinghours.ParseJSON(encoded)
		if err != nil || !parsed.Equal(reparsed) {
			t.Fatalf("accepted schedule did not round trip: %v", err)
		}
	})
}

func FuzzTextAndSQLScan(f *testing.F) {
	schedule, _ := openinghours.NewSchedule(openinghours.Config{Timezone: "UTC"})
	canonical, _ := schedule.MarshalText()
	f.Add(canonical)
	f.Add([]byte("not-json"))
	f.Fuzz(func(t *testing.T, data []byte) {
		var fromText, fromBytes, fromString openinghours.Schedule
		textErr := fromText.UnmarshalText(data)
		bytesErr := fromBytes.Scan(data)
		stringErr := fromString.Scan(string(data))
		if (textErr == nil) != (bytesErr == nil) || (textErr == nil) != (stringErr == nil) {
			t.Fatalf("text/SQL acceptance differs: text=%v bytes=%v string=%v", textErr, bytesErr, stringErr)
		}
		if textErr == nil && (!fromText.Equal(fromBytes) || !fromText.Equal(fromString)) {
			t.Fatal("text/SQL decoding changed the accepted schedule")
		}
	})
}

func FuzzRangeConstruction(f *testing.F) {
	f.Add(9, 0, 17, 0)
	f.Add(22, 0, 2, 0)
	f.Fuzz(func(t *testing.T, startHour, startMinute, endHour, endMinute int) {
		start, startErr := openinghours.NewLocalTime(startHour, startMinute, 0, 0)
		end, endErr := openinghours.NewLocalTime(endHour, endMinute, 0, 0)
		if startErr != nil || endErr != nil {
			return
		}
		item, err := openinghours.NewRange(start, end)
		if start == end {
			if !openinghours.IsCode(err, openinghours.CodeInvalidRange) {
				t.Fatalf("equal endpoints error = %v", err)
			}
			return
		}
		if err != nil || item.Start() != start || item.End() != end {
			t.Fatalf("range = %#v error=%v", item, err)
		}
	})
}

func FuzzLocalResolution(f *testing.F) {
	f.Add(2026, int(time.March), 29, 3, 30)
	f.Fuzz(func(t *testing.T, year, month, day, hour, minute int) {
		date, dateErr := openinghours.NewDate(year, time.Month(month), day)
		local, timeErr := openinghours.NewLocalTime(hour, minute, 0, 0)
		if dateErr != nil || timeErr != nil {
			return
		}
		schedule, err := openinghours.NewSchedule(openinghours.Config{Timezone: "Europe/Helsinki"})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = schedule.ResolveLocal(date, local, openinghours.RejectDST)
		_, _ = schedule.ResolveLocal(date, local, openinghours.ShiftForward)
	})
}

func FuzzScheduleConstruction(f *testing.F) {
	f.Add(uint8(time.Monday), uint8(openinghours.DayClosed), uint8(openinghours.RejectOverlap))
	f.Fuzz(func(t *testing.T, weekday, state, policy uint8) {
		day := time.Weekday(weekday)
		var rule openinghours.DayRule
		switch openinghours.DayState(state % 4) {
		case openinghours.DayInherited:
			rule = openinghours.Inherited()
		case openinghours.DayOpenRanges:
			rule, _ = openinghours.OpenRanges(
				[]openinghours.Range{mustRange(t, 9, 0, 17, 0)},
				openinghours.OverlapPolicy(policy%4),
			)
		case openinghours.DayOpenAllDay:
			rule = openinghours.OpenAllDay()
		case openinghours.DayClosed:
			rule = openinghours.Closed()
		}
		_, _ = openinghours.NewSchedule(openinghours.Config{
			Timezone: "UTC", Weekly: map[time.Weekday]openinghours.DayRule{day: rule},
		})
	})
}

func FuzzCompositionAndBoundedSearch(f *testing.F) {
	f.Add(uint8(9), uint8(17), uint8(24))
	f.Fuzz(func(t *testing.T, startHour, endHour, horizonHours uint8) {
		start := int(startHour % 24)
		end := int(endHour % 24)
		if start == end {
			return
		}
		left := scheduleWithMonday(t, mustRange(t, start, 0, end, 0))
		right := scheduleWithMonday(t, mustRange(t, 12, 0, 13, 0))
		combined, err := left.Union(right)
		if err != nil {
			t.Fatal(err)
		}
		horizon := time.Duration(horizonHours%48+1) * time.Hour
		_, _ = combined.NextTransition(
			time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC), horizon,
		)
	})
}
