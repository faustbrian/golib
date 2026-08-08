package notation

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func TestNotationCodecsAcceptExactByteLimits(t *testing.T) {
	t.Parallel()

	dateInput := "2026-01-01/2026-01-02"
	dateValue, err := ParseDate(dateInput, ISO8601, temporal.Limits{ParseBytes: len(dateInput)})
	if err != nil {
		t.Fatalf("ParseDate(exact limit): %v", err)
	}
	dateEncoded, err := FormatDate(dateValue, ISO8601, temporal.Limits{FormatBytes: len(dateInput)})
	if err != nil || dateEncoded != dateInput {
		t.Fatalf("FormatDate(exact limit) = %q, %v", dateEncoded, err)
	}

	durationInput := "PT1S"
	duration, err := ParseDuration(durationInput, temporal.Limits{ParseBytes: len(durationInput)})
	if err != nil {
		t.Fatalf("ParseDuration(exact limit): %v", err)
	}
	durationEncoded, err := FormatDuration(duration, temporal.Limits{FormatBytes: len(durationInput)})
	if err != nil || durationEncoded != durationInput {
		t.Fatalf("FormatDuration(exact limit) = %q, %v", durationEncoded, err)
	}

	instantInput := "2026-01-01T00:00:00Z/2026-01-02T00:00:00Z"
	instantValue, err := ParseInstant(instantInput, ISO8601, temporal.Limits{ParseBytes: len(instantInput)})
	if err != nil {
		t.Fatalf("ParseInstant(exact limit): %v", err)
	}
	instantEncoded, err := FormatInstant(instantValue, ISO8601, temporal.Limits{FormatBytes: len(instantInput)})
	if err != nil || instantEncoded != instantInput {
		t.Fatalf("FormatInstant(exact limit) = %q, %v", instantEncoded, err)
	}

	dailyInput := "08:00/17:00"
	daily, err := ParseDailyInterval(dailyInput, ISO8601, temporal.Limits{ParseBytes: len(dailyInput)})
	if err != nil {
		t.Fatalf("ParseDailyInterval(exact limit): %v", err)
	}
	dailyEncoded, err := FormatDailyInterval(daily, ISO8601, temporal.Limits{FormatBytes: len(dailyInput)})
	if err != nil || dailyEncoded != dailyInput {
		t.Fatalf("FormatDailyInterval(exact limit) = %q, %v", dailyEncoded, err)
	}
}

func TestDurationFractionsScaleEverySupportedPrecision(t *testing.T) {
	t.Parallel()

	digits := "123456789"
	for precision := 1; precision <= len(digits); precision++ {
		input := "PT0." + digits[:precision] + "S"
		value, err := ParseDuration(input, temporal.Limits{})
		if err != nil {
			t.Fatalf("ParseDuration(%q): %v", input, err)
		}
		want := time.Duration(123_456_789)
		for removed := precision; removed < len(digits); removed++ {
			want -= want % 10
			want /= 10
		}
		for padded := precision; padded < len(digits); padded++ {
			want *= 10
		}
		if value.Value() != want {
			t.Fatalf("ParseDuration(%q) = %v, want %v", input, value.Value(), want)
		}
	}
}

func TestDurationFormattingIsCanonicalForEveryComponentCombination(t *testing.T) {
	t.Parallel()

	tests := map[time.Duration]string{
		0:                         "PT0S",
		time.Nanosecond:           "PT0.000000001S",
		time.Second:               "PT1S",
		time.Minute:               "PT1M",
		time.Hour:                 "PT1H",
		24 * time.Hour:            "P1D",
		time.Hour + time.Minute:   "PT1H1M",
		time.Hour + time.Second:   "PT1H1S",
		time.Minute + time.Second: "PT1M1S",
		24*time.Hour + time.Hour + time.Minute + time.Second + 1: "P1DT1H1M1.000000001S",
		-(time.Hour + time.Minute + time.Second):                 "-PT1H1M1S",
	}
	for raw, want := range tests {
		got, err := FormatDuration(timeofday.NewDuration(raw), temporal.Limits{})
		if err != nil || got != want {
			t.Fatalf("FormatDuration(%v) = %q, %v; want %q", raw, got, err, want)
		}
	}
}

func TestSplitAndBracketHelpersRejectEveryAmbiguousBoundary(t *testing.T) {
	t.Parallel()

	if start, end, err := splitExactly("a/b", '/'); err != nil || start != "a" || end != "b" {
		t.Fatalf("splitExactly(valid) = %q, %q, %v", start, end, err)
	}
	for _, input := range []string{"/b", "a/", "a//b", "a/b/c", "ab"} {
		if _, _, err := splitExactly(input, '/'); !errors.Is(err, temporal.ErrParse) {
			t.Fatalf("splitExactly(%q) error = %v", input, err)
		}
	}

	for _, format := range []struct {
		bourbaki bool
		valid    [][2]byte
		invalid  [][2]byte
	}{
		{false, [][2]byte{{'[', ']'}, {'[', ')'}, {'(', ']'}, {'(', ')'}}, [][2]byte{{']', ']'}, {'[', '['}, {'x', ')'}, {'(', 'x'}}},
		{true, [][2]byte{{'[', ']'}, {'[', '['}, {']', ']'}, {']', '['}}, [][2]byte{{'(', ']'}, {'[', ')'}, {'x', '['}, {']', 'x'}}},
	} {
		for _, pair := range format.valid {
			if _, _, ok := bracketInclusion(pair[0], pair[1], format.bourbaki); !ok {
				t.Fatalf("bracketInclusion(%q,%q,%v) rejected valid pair", pair[0], pair[1], format.bourbaki)
			}
		}
		for _, pair := range format.invalid {
			if _, _, ok := bracketInclusion(pair[0], pair[1], format.bourbaki); ok {
				t.Fatalf("bracketInclusion(%q,%q,%v) accepted invalid pair", pair[0], pair[1], format.bourbaki)
			}
		}
	}
}

func TestFractionDigitsStopsAtTheRFC3339Suffix(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]int{
		"2026-01-01T00:00:00Z":          0,
		".123Z":                         3,
		"x.123":                         3,
		"2026-01-01T00:00:00.123+02:00": 3,
		"2026-01-01T00:00:00.Z":         0,
	} {
		if got := fractionDigits(input); got != want {
			t.Fatalf("fractionDigits(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestDailySpecialIntervalsRequireEveryDefiningCondition(t *testing.T) {
	t.Parallel()

	full, err := ParseDailyInterval("[00:00,24:00]", ISO80000, temporal.Limits{})
	if err != nil || full.Kind() != timeofday.FullDayKind {
		t.Fatalf("full day = %+v, %v", full, err)
	}
	collapsed, err := ParseDailyInterval("(08:00,08:00)", ISO80000, temporal.Limits{})
	if err != nil || collapsed.Kind() != timeofday.CollapsedKind {
		t.Fatalf("collapsed = %+v, %v", collapsed, err)
	}
	for _, input := range []string{
		"(00:00,24:00)",
		"[00:00,23:00]",
		"[01:00,24:00]",
	} {
		interval, err := ParseDailyInterval(input, ISO80000, temporal.Limits{})
		if err != nil || interval.Kind() == timeofday.FullDayKind {
			t.Fatalf("ParseDailyInterval(%q) = %+v, %v", input, interval, err)
		}
	}
	for _, input := range []string{"[08:00,08:00]", "[24:00,00:00)"} {
		if _, err := ParseDailyInterval(input, ISO80000, temporal.Limits{}); err == nil {
			t.Fatalf("ParseDailyInterval(%q) error = nil", input)
		}
	}
	circular, err := ParseDailyInterval("[23:00,00:00)", ISO80000, temporal.Limits{})
	if err != nil || circular.Kind() != timeofday.Circular {
		t.Fatalf("midnight-ending circular interval = %+v, %v", circular, err)
	}
}

func TestSplitBoundedAcceptsItsMinimumStructuralLength(t *testing.T) {
	t.Parallel()

	start, end, bounds, err := splitBounded("[a,b]", false)
	if err != nil || start != "a" || end != "b" || bounds != temporal.Closed {
		t.Fatalf("splitBounded(minimum) = %q, %q, %v, %v", start, end, bounds, err)
	}
}

func TestNotationFixturesConstructAllPublicValueKinds(t *testing.T) {
	t.Parallel()

	date, _ := dateperiod.New(calendar.MustDate(2026, 1, 1), calendar.MustDate(2026, 1, 2), temporal.ClosedOpen)
	if _, err := FormatDate(date, ISO8601, temporal.Limits{}); err != nil {
		t.Fatalf("FormatDate(): %v", err)
	}
	period, _ := instant.Range(time.Unix(0, 0).UTC(), time.Unix(1, 0).UTC())
	if _, err := FormatInstant(period, ISO8601, temporal.Limits{}); err != nil {
		t.Fatalf("FormatInstant(): %v", err)
	}
}
