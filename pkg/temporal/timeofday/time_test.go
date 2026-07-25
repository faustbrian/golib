package timeofday_test

import (
	"errors"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func mustTime(t *testing.T, hour, minute, second, nanosecond, digits int) timeofday.Time {
	t.Helper()

	value, err := timeofday.New(hour, minute, second, nanosecond, digits)
	if err != nil {
		t.Fatalf("New(%d,%d,%d,%d,%d): %v", hour, minute, second, nanosecond, digits, err)
	}

	return value
}

func TestTimeConstructionAndNamedValues(t *testing.T) {
	t.Parallel()

	value := mustTime(t, 12, 34, 56, 123_000_000, 3)
	hour, minute, second, nanosecond := value.Components()
	if hour != 12 || minute != 34 || second != 56 || nanosecond != 123_000_000 {
		t.Fatalf("Components() = %d:%d:%d.%d", hour, minute, second, nanosecond)
	}
	if value.FractionalDigits() != 3 || !value.HasSeconds() {
		t.Fatalf("precision = %d, seconds = %v", value.FractionalDigits(), value.HasSeconds())
	}
	if !timeofday.Midnight().Equal(mustTime(t, 0, 0, 0, 0, 0)) {
		t.Fatal("Midnight() is not 00:00:00")
	}
	if !timeofday.Noon().Equal(mustTime(t, 12, 0, 0, 0, 0)) {
		t.Fatal("Noon() is not 12:00:00")
	}
	if !timeofday.EndOfDay().IsEndBoundary() {
		t.Fatal("EndOfDay() is not a distinct end boundary")
	}
	hour, minute, second, nanosecond = timeofday.EndOfDay().Components()
	if hour != 24 || minute != 0 || second != 0 || nanosecond != 0 {
		t.Fatalf("EndOfDay().Components() = %d:%d:%d.%d", hour, minute, second, nanosecond)
	}
	if timeofday.Noon().Offset() != 12*time.Hour {
		t.Fatalf("Noon().Offset() = %v", timeofday.Noon().Offset())
	}
	if timeofday.EndOfDay().Equal(timeofday.Midnight()) || timeofday.EndOfDay().Compare(timeofday.Midnight()) <= 0 {
		t.Fatal("24:00 silently equaled 00:00")
	}
}

func TestTimeRejectsInvalidComponentsAndPrecision(t *testing.T) {
	t.Parallel()

	tests := [][5]int{
		{-1, 0, 0, 0, 0},
		{24, 0, 0, 0, 0},
		{0, -1, 0, 0, 0},
		{0, 60, 0, 0, 0},
		{0, 0, -1, 0, 0},
		{0, 0, 60, 0, 0},
		{0, 0, 0, -1, 0},
		{0, 0, 0, 1_000_000_000, 0},
		{0, 0, 0, 1, -1},
		{0, 0, 0, 1, 10},
		{0, 0, 0, 123_000_000, 2},
	}
	for _, parts := range tests {
		if _, err := timeofday.New(parts[0], parts[1], parts[2], parts[3], parts[4]); !errors.Is(err, temporal.ErrInvalidTime) {
			t.Fatalf("New(%v) error = %v", parts, err)
		}
	}
}

func TestStrictTimeParsingAndRoundTrip(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"00:00",
		"12:34:56",
		"12:34:56.1",
		"12:34:56.123456789",
		"24:00",
	} {
		value, err := timeofday.Parse(input, temporal.Limits{})
		if err != nil {
			t.Fatalf("Parse(%q): %v", input, err)
		}
		if got := value.String(); got != input {
			t.Fatalf("Parse(%q).String() = %q", input, got)
		}
	}
}

func TestTimeParsingRejectsMalformedAndHostileInput(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"",
		"1:00",
		"aa:00",
		"00:aa",
		"00:00 ",
		"00:00:aa",
		"00:00:00x",
		"00:00:00.a",
		"00:00:00.",
		"00:00:00.1234567890",
		"24:00:00",
		"24:01",
		"23:59:60",
		string([]byte{0xff}),
	} {
		if _, err := timeofday.Parse(input, temporal.Limits{}); err == nil {
			t.Fatalf("Parse(%q) error = nil", input)
		}
	}
	if _, err := timeofday.Parse("12:34:56", temporal.Limits{ParseBytes: 4}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Parse(limit) error = %v", err)
	}
	if _, err := timeofday.Parse("12:34:56", temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Parse(invalid limits) error = %v", err)
	}
}

func TestTimeComparisonClampAndDifference(t *testing.T) {
	t.Parallel()

	early := mustTime(t, 8, 0, 0, 0, 0)
	middle := mustTime(t, 12, 0, 0, 0, 0)
	late := mustTime(t, 18, 0, 0, 0, 0)
	if early.Compare(middle) >= 0 || late.Compare(middle) <= 0 || middle.Compare(middle) != 0 {
		t.Fatal("Compare() ordering failed")
	}
	if got, err := early.Clamp(middle, late); err != nil || !got.Equal(middle) {
		t.Fatalf("early.Clamp() = %v, %v", got, err)
	}
	if got, err := late.Clamp(early, middle); err != nil || !got.Equal(middle) {
		t.Fatalf("late.Clamp() = %v, %v", got, err)
	}
	if got, err := middle.Clamp(early, late); err != nil || !got.Equal(middle) {
		t.Fatalf("middle.Clamp() = %v, %v", got, err)
	}
	if _, err := middle.Clamp(late, early); !errors.Is(err, temporal.ErrInvalidTime) {
		t.Fatalf("Clamp(reversed) error = %v", err)
	}
	if got := early.Difference(late); got != 10*time.Hour {
		t.Fatalf("Difference() = %v", got)
	}
	if got := early.CircularDistance(late); got != 10*time.Hour {
		t.Fatalf("CircularDistance() = %v", got)
	}
	if got := mustTime(t, 23, 0, 0, 0, 0).CircularDistance(mustTime(t, 1, 0, 0, 0, 0)); got != 2*time.Hour {
		t.Fatalf("midnight CircularDistance() = %v", got)
	}
}

func TestShiftRequiresExplicitWrappingPolicy(t *testing.T) {
	t.Parallel()

	value := mustTime(t, 23, 0, 0, 0, 0)
	wrapped, err := value.Shift(2*time.Hour, timeofday.Wrap)
	if err != nil || !wrapped.Equal(mustTime(t, 1, 0, 0, 0, 0)) {
		t.Fatalf("Shift(Wrap) = %v, %v", wrapped, err)
	}
	if _, err := value.Shift(2*time.Hour, timeofday.RejectOverflow); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Shift(RejectOverflow) error = %v", err)
	}
	withinDay, err := timeofday.Noon().Shift(time.Hour, timeofday.RejectOverflow)
	if err != nil || !withinDay.Equal(mustTime(t, 13, 0, 0, 0, 0)) {
		t.Fatalf("Shift(RejectOverflow) = %v, %v", withinDay, err)
	}
	if _, err := timeofday.EndOfDay().Shift(time.Duration(1<<63-1), timeofday.RejectOverflow); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Shift(arithmetic overflow) error = %v", err)
	}
	backward, err := mustTime(t, 1, 0, 0, 0, 0).Shift(-2*time.Hour, timeofday.Wrap)
	if err != nil || !backward.Equal(value) {
		t.Fatalf("backward Shift(Wrap) = %v, %v", backward, err)
	}
	if _, err := value.Shift(time.Hour, timeofday.WrapPolicy(255)); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Shift(policy) error = %v", err)
	}
	if got, err := timeofday.EndOfDay().Shift(0, timeofday.Wrap); err != nil || !got.IsEndBoundary() {
		t.Fatalf("EndOfDay().Shift(0) = %v, %v", got, err)
	}
}
