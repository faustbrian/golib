package encoding

import (
	"strings"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

// Slot is a strict structured local-time interval.
type Slot struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ImportLimits makes structured input cardinality explicit.
type ImportLimits struct {
	MaximumDays         int
	MaximumRangesPerDay int
}

// DefaultImportLimits returns the package-safe structured import limits.
func DefaultImportLimits() ImportLimits {
	return ImportLimits{MaximumDays: 7, MaximumRangesPerDay: 64}
}

// ImportLocation imports Location's weekday-keyed {from,to} slot structure.
// A present empty/nil day is closed; an absent day remains inherited.
func ImportLocation(timezone string, days map[string][]Slot, limits ImportLimits) (openinghours.Schedule, error) {
	if limits.MaximumDays <= 0 || limits.MaximumDays > 7 ||
		limits.MaximumRangesPerDay <= 0 || limits.MaximumRangesPerDay > 64 ||
		len(days) > limits.MaximumDays {
		return openinghours.Schedule{}, &ImportError{Kind: "limit"}
	}
	weekly := make(map[time.Weekday]openinghours.DayRule, len(days))
	for name, slots := range days {
		weekday, ok := importWeekday(name)
		if !ok {
			return openinghours.Schedule{}, &ImportError{Kind: "weekday"}
		}
		if len(slots) > limits.MaximumRangesPerDay {
			return openinghours.Schedule{}, &ImportError{Kind: "limit"}
		}
		if len(slots) == 0 {
			weekly[weekday] = openinghours.Closed()
			continue
		}
		ranges := make([]openinghours.Range, 0, len(slots))
		for _, slot := range slots {
			item, err := importRange(slot.From, slot.To)
			if err != nil {
				return openinghours.Schedule{}, err
			}
			ranges = append(ranges, item)
		}
		rule, err := openinghours.OpenRanges(ranges, openinghours.RejectOverlapAndAdjacent)
		if err != nil {
			return openinghours.Schedule{}, err
		}
		weekly[weekday] = rule
	}

	return openinghours.NewSchedule(openinghours.Config{Timezone: timezone, Weekly: weekly})
}

// ImportSpatie imports strict Spatie weekday arrays. Carrier prose and variable
// formats are intentionally excluded; only lossless HH:MM-HH:MM values pass.
func ImportSpatie(timezone string, days map[string][]string, limits ImportLimits) (openinghours.Schedule, error) {
	structured := make(map[string][]Slot, len(days))
	for day, ranges := range days {
		slots := make([]Slot, 0, len(ranges))
		for _, item := range ranges {
			from, to, found := strings.Cut(item, "-")
			if !found || strings.Contains(to, "-") {
				return openinghours.Schedule{}, &ImportError{Kind: "range"}
			}
			slots = append(slots, Slot{From: from, To: to})
		}
		structured[day] = slots
	}

	return ImportLocation(timezone, structured, limits)
}

// ImportError is bounded and never includes source payload data.
type ImportError struct{ Kind string }

func (e *ImportError) Error() string { return "opening hours import: " + e.Kind }

func importRange(from, to string) (openinghours.Range, error) {
	start, err := importTime(from)
	if err != nil {
		return openinghours.Range{}, err
	}
	end, err := importTime(to)
	if err != nil {
		return openinghours.Range{}, err
	}

	return openinghours.NewRange(start, end)
}

func importTime(input string) (openinghours.LocalTime, error) {
	parsed, err := time.Parse("15:04", input)
	if err != nil || parsed.Format("15:04") != input {
		return openinghours.LocalTime{}, &ImportError{Kind: "time"}
	}

	return openinghours.NewLocalTime(parsed.Hour(), parsed.Minute(), 0, 0)
}

func importWeekday(input string) (time.Weekday, bool) {
	for weekday, name := range []string{
		"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday",
	} {
		if input == name {
			return time.Weekday(weekday), true
		}
	}

	return 0, false
}
