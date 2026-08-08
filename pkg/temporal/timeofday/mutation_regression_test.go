package timeofday

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

func mutationTime(t *testing.T, hour, minute, second, nanosecond, digits int) Time {
	t.Helper()
	value, err := New(hour, minute, second, nanosecond, digits)
	if err != nil {
		t.Fatalf("New(%d, %d, %d, %d, %d): %v", hour, minute, second, nanosecond, digits, err)
	}
	return value
}

func mutationInterval(t *testing.T, startHour, endHour int, bounds temporal.Bounds) Interval {
	t.Helper()
	value, err := Between(
		mutationTime(t, startHour, 0, 0, 0, 0),
		mutationTime(t, endHour, 0, 0, 0, 0),
		bounds,
	)
	if err != nil {
		t.Fatalf("Between(%d, %d, %v): %v", startHour, endHour, bounds, err)
	}
	return value
}

func requireDuration(t *testing.T, got Duration, err error, want time.Duration) {
	t.Helper()
	if err != nil || got.Value() != want {
		t.Fatalf("duration = %v, %v; want %v", got.Value(), err, want)
	}
}

func TestFromInstantAcceptsOnlyTheInclusivePrecisionDomain(t *testing.T) {
	t.Parallel()

	instant := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	for _, digits := range []int{0, 9} {
		value, date, err := FromInstant(instant, time.UTC, digits)
		if err != nil || value.FractionalDigits() != digits || date != calendar.MustDate(2026, time.January, 2) {
			t.Fatalf("FromInstant(digits=%d) = %v, %v, %v", digits, value, date, err)
		}
	}
	for _, digits := range []int{-1, 10} {
		if _, _, err := FromInstant(instant, time.UTC, digits); !errors.Is(err, temporal.ErrPrecision) {
			t.Fatalf("FromInstant(digits=%d) error = %v", digits, err)
		}
	}
}

func TestAroundUsesTheExactFullDayThreshold(t *testing.T) {
	t.Parallel()

	center := Noon()
	full, err := Around(center, 12*time.Hour, temporal.Closed)
	if err != nil || full.Kind() != FullDayKind {
		t.Fatalf("Around(12h) = %+v, %v", full, err)
	}
	partial, err := Around(center, 12*time.Hour-time.Nanosecond, temporal.Closed)
	wantStart, _ := center.Shift(-(12*time.Hour - time.Nanosecond), Wrap)
	if err != nil || partial.Kind() == FullDayKind || partial.Duration() != day-2*time.Nanosecond ||
		!partial.Start().Equal(wantStart) {
		t.Fatalf("Around(below 12h) = %+v, %v", partial, err)
	}
}

func TestDurationArithmeticPreservesExactIntegerBoundaries(t *testing.T) {
	t.Parallel()

	maximum := time.Duration(1<<63 - 1)
	minimum := time.Duration(-1 << 63)
	result, err := NewDuration(maximum - 1).Add(NewDuration(1))
	requireDuration(t, result, err, maximum)
	if _, err := NewDuration(maximum).Add(NewDuration(1)); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Add(positive overflow) error = %v", err)
	}
	result, err = NewDuration(minimum + 1).Add(NewDuration(-1))
	requireDuration(t, result, err, minimum)
	if _, err := NewDuration(minimum).Add(NewDuration(-1)); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Add(negative overflow) error = %v", err)
	}

	for _, value := range []time.Duration{0, 1, -1} {
		absolute, err := NewDuration(value).Abs()
		if err != nil || absolute.Value() != max(value, -value) {
			t.Fatalf("Abs(%v) = %v, %v", value, absolute.Value(), err)
		}
	}
	if _, err := NewDuration(minimum).Abs(); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Abs(minimum) error = %v", err)
	}

	point := NewDuration(5)
	result, err = point.Clamp(point, point)
	requireDuration(t, result, err, 5)
	result, err = NewDuration(4).Clamp(point, NewDuration(6))
	requireDuration(t, result, err, 5)
	result, err = NewDuration(6).Clamp(NewDuration(4), point)
	requireDuration(t, result, err, 5)
	if _, err := point.Clamp(NewDuration(6), point); !errors.Is(err, temporal.ErrStep) {
		t.Fatalf("Clamp(reversed) error = %v", err)
	}

	if _, err := NewDuration(minimum).Multiply(-1); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Multiply(minimum, -1) error = %v", err)
	}
	if _, _, err := NewDuration(minimum).Divide(-1); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Divide(minimum, -1) error = %v", err)
	}
	quotient, remainder, err := NewDuration(7).Divide(3)
	if err != nil || quotient.Value() != 2 || remainder != 1 {
		t.Fatalf("Divide(7, 3) = %v remainder %v, %v", quotient.Value(), remainder, err)
	}
}

func TestDurationRoundingDistinguishesSignsAndExactTies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value time.Duration
		unit  time.Duration
		mode  RoundingMode
		want  time.Duration
	}{
		{0, 2, RoundFloor, 0},
		{-1, 2, RoundFloor, -2},
		{0, 2, RoundCeil, 0},
		{1, 2, RoundCeil, 2},
		{1, 3, RoundNearest, 0},
		{2, 3, RoundNearest, 3},
		{2, 4, RoundNearest, 4},
		{-2, 4, RoundNearest, -4},
		{-1, 4, RoundNearest, 0},
	}
	for _, test := range tests {
		rounded, err := NewDuration(test.value).Round(test.unit, test.mode)
		if err != nil || rounded.Value() != test.want {
			t.Fatalf("Round(%v, %v, %v) = %v, %v; want %v", test.value, test.unit, test.mode, rounded.Value(), err, test.want)
		}
	}
}

func TestIntervalMembershipAndEqualityObserveEveryBoundary(t *testing.T) {
	t.Parallel()

	full := FullDay()
	for _, value := range []Time{Midnight(), EndOfDay()} {
		if !full.Includes(value) {
			t.Fatalf("FullDay excludes %v", value)
		}
	}
	for _, value := range []Time{{offset: -1}, {offset: day + 1}} {
		if full.Includes(value) {
			t.Fatalf("FullDay includes invalid offset %v", value.offset)
		}
	}

	start := mutationTime(t, 22, 0, 0, 0, 0)
	end := mutationTime(t, 2, 0, 0, 0, 0)
	for _, bounds := range temporal.AllBounds() {
		interval, err := Between(start, end, bounds)
		if err != nil {
			t.Fatalf("Between(circular, %v): %v", bounds, err)
		}
		if interval.Includes(start) != bounds.IncludesStart() || interval.Includes(end) != bounds.IncludesEnd() {
			t.Fatalf("circular membership for %v = start %v end %v", bounds, interval.Includes(start), interval.Includes(end))
		}
		if !interval.Includes(Midnight()) || interval.Includes(Noon()) {
			t.Fatalf("circular interior membership failed for %v", bounds)
		}
	}

	base := mutationInterval(t, 8, 10, temporal.Closed)
	variants := []Interval{
		{start: mutationTime(t, 7, 0, 0, 0, 0), end: base.end, bounds: base.bounds, kind: base.kind},
		{start: base.start, end: mutationTime(t, 11, 0, 0, 0, 0), bounds: base.bounds, kind: base.kind},
		{start: base.start, end: base.end, bounds: temporal.Open, kind: base.kind},
		{start: base.start, end: base.end, bounds: base.bounds, kind: Circular},
	}
	if !base.Equal(base) {
		t.Fatal("interval is not structurally equal to itself")
	}
	for _, variant := range variants {
		if base.Equal(variant) {
			t.Fatalf("Equal accepted variant %+v", variant)
		}
	}
}

func TestIntervalExpansionGapSplitAndStepsUseExactLimits(t *testing.T) {
	t.Parallel()

	interval := mutationInterval(t, 8, 10, temporal.Closed)
	if _, err := interval.Expand(-2*time.Hour, 0); !errors.Is(err, temporal.ErrEmpty) {
		t.Fatalf("Expand(to zero) error = %v", err)
	}
	full, err := interval.Expand(22*time.Hour, 0)
	if err != nil || full.Kind() != FullDayKind {
		t.Fatalf("Expand(to full day) = %+v, %v", full, err)
	}

	left := mutationInterval(t, 1, 3, temporal.ClosedOpen)
	right := mutationInterval(t, 5, 7, temporal.OpenClosed)
	forward, err := left.Gap(right)
	if err != nil || !forward.Start().Equal(left.End()) || !forward.End().Equal(right.Start()) || forward.Bounds() != temporal.Closed {
		t.Fatalf("forward Gap() = %+v, %v", forward, err)
	}
	reverse, err := right.Gap(left)
	if err != nil || !reverse.Equal(forward) {
		t.Fatalf("reverse Gap() = %+v, %v", reverse, err)
	}
	abutting := mutationInterval(t, 3, 5, temporal.Closed)
	gap, err := left.Gap(abutting)
	if err != nil || gap.Kind() != CollapsedKind || !gap.Start().Equal(left.End()) {
		t.Fatalf("abutting Gap() = %+v, %v", gap, err)
	}

	long := mutationInterval(t, 0, 5, temporal.Closed)
	parts, err := long.Split(2*time.Hour, temporal.Limits{Steps: 3})
	if err != nil || len(parts) != 3 {
		t.Fatalf("Split(remainder) = %+v, %v", parts, err)
	}
	wantDurations := []time.Duration{2 * time.Hour, 2 * time.Hour, time.Hour}
	for index := range parts {
		if parts[index].Duration() != wantDurations[index] {
			t.Fatalf("part %d duration = %v, want %v", index, parts[index].Duration(), wantDurations[index])
		}
	}
	if _, err := long.Split(2*time.Hour, temporal.Limits{Steps: 2}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Split(over limit) error = %v", err)
	}
	exact, err := long.Split(5*time.Hour, temporal.Limits{Steps: 1})
	if err != nil || len(exact) != 1 || !exact[0].SetEqual(long) {
		t.Fatalf("Split(exact) = %+v, %v", exact, err)
	}

	steps, err := interval.Steps(time.Hour, temporal.Limits{Steps: 3})
	if err != nil || len(steps) != 3 || !steps[0].Equal(interval.Start()) || !steps[2].Equal(interval.End()) {
		t.Fatalf("Steps(exact limit) = %+v, %v", steps, err)
	}
	if _, err := interval.Steps(time.Hour, temporal.Limits{Steps: 2}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Steps(over limit) error = %v", err)
	} else {
		var limitError *temporal.LimitError
		if !errors.As(err, &limitError) || limitError.Value != 3 || limitError.Max != 2 {
			t.Fatalf("Steps(over limit) detail = %+v", err)
		}
	}
	openClosed := mutationInterval(t, 8, 9, temporal.OpenClosed)
	steps, err = openClosed.Steps(time.Hour, temporal.Limits{Steps: 1})
	if err != nil || len(steps) != 1 || !steps[0].Equal(openClosed.End()) {
		t.Fatalf("Steps(open-closed exact duration) = %+v, %v", steps, err)
	}
	open := mutationInterval(t, 8, 9, temporal.Open)
	steps, err = open.Steps(2*time.Hour, temporal.Limits{})
	if err != nil || len(steps) != 0 {
		t.Fatalf("Steps(first beyond duration) = %+v, %v", steps, err)
	}
}

func TestDailySegmentQueriesAndAlgebraUseExactEndpoints(t *testing.T) {
	t.Parallel()

	set := IntervalSet{segments: []dailySegment{
		{start: time.Hour, end: 2 * time.Hour, includeStart: true, includeEnd: true},
		{start: 4 * time.Hour, end: 5 * time.Hour, includeStart: false, includeEnd: false},
	}}
	for _, test := range []struct {
		value time.Duration
		index int
		found bool
	}{
		{0, 0, false},
		{time.Hour, 0, true},
		{2 * time.Hour, 0, true},
		{3 * time.Hour, 1, false},
		{4 * time.Hour, 1, false},
		{5 * time.Hour, 1, false},
		{6 * time.Hour, 2, false},
	} {
		index, found := set.Search(timeFromOffset(test.value))
		if index != test.index || found != test.found {
			t.Fatalf("Search(%v) = %d, %v; want %d, %v", test.value, index, found, test.index, test.found)
		}
	}

	left := IntervalSet{segments: []dailySegment{
		{start: 0, end: 3 * time.Hour, includeStart: true, includeEnd: true},
		{start: 5 * time.Hour, end: 8 * time.Hour, includeStart: true, includeEnd: true},
	}}
	right := IntervalSet{segments: []dailySegment{
		{start: time.Hour, end: 2 * time.Hour, includeStart: true, includeEnd: true},
		{start: 4 * time.Hour, end: 6 * time.Hour, includeStart: true, includeEnd: true},
		{start: 7 * time.Hour, end: 9 * time.Hour, includeStart: true, includeEnd: true},
	}}
	intersection, err := left.Intersect(right)
	if err != nil || len(intersection.segments) != 3 {
		t.Fatalf("Intersect() = %+v, %v", intersection.segments, err)
	}
	want := []dailySegment{
		{start: time.Hour, end: 2 * time.Hour, includeStart: true, includeEnd: true},
		{start: 5 * time.Hour, end: 6 * time.Hour, includeStart: true, includeEnd: true},
		{start: 7 * time.Hour, end: 8 * time.Hour, includeStart: true, includeEnd: true},
	}
	for index := range want {
		if intersection.segments[index] != want[index] {
			t.Fatalf("intersection %d = %+v, want %+v", index, intersection.segments[index], want[index])
		}
	}

	limited := left
	limited.limits = temporal.Limits{OutputPeriods: 2}.Resolve()
	if _, err := limited.Intersect(right); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Intersect(over limit) error = %v", err)
	}
	exactLimit := left
	exactLimit.limits = temporal.Limits{OutputPeriods: 3}.Resolve()
	if result, err := exactLimit.Intersect(right); err != nil || len(result.segments) != 3 {
		t.Fatalf("Intersect(exact limit) = %+v, %v", result.segments, err)
	}

	removed := IntervalSet{segments: []dailySegment{{start: time.Hour, end: 2 * time.Hour, includeStart: true, includeEnd: true}}}
	subtractExact := IntervalSet{
		segments: []dailySegment{{start: 0, end: 3 * time.Hour, includeStart: true, includeEnd: true}},
		limits:   temporal.Limits{OutputPeriods: 2}.Resolve(),
	}
	if result, err := subtractExact.Subtract(removed); err != nil || len(result.segments) != 2 {
		t.Fatalf("Subtract(exact limit) = %+v, %v", result.segments, err)
	}
	subtractOver := subtractExact
	subtractOver.limits = temporal.Limits{OutputPeriods: 1}.Resolve()
	if _, err := subtractOver.Subtract(removed); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Subtract(over limit) error = %v", err)
	}

	fragments := subtractDaily(
		dailySegment{start: 0, end: 4, includeStart: true, includeEnd: true},
		dailySegment{start: 0, end: 4, includeStart: false, includeEnd: false},
	)
	if len(fragments) != 2 || fragments[0] != (dailySegment{start: 0, end: 0, includeStart: true, includeEnd: true}) ||
		fragments[1] != (dailySegment{start: 4, end: 4, includeStart: true, includeEnd: true}) {
		t.Fatalf("subtractDaily(open interior) = %+v", fragments)
	}
}

func TestDailySegmentNormalizationAndOrderingObserveEveryTieBreaker(t *testing.T) {
	t.Parallel()

	fullSet := IntervalSet{segments: FullDay().segments()}
	intervals := fullSet.Intervals()
	if len(intervals) != 1 || intervals[0].Kind() != FullDayKind {
		t.Fatalf("Intervals(full day) = %+v", intervals)
	}

	segments := []dailySegment{
		{start: 4, end: 5, includeStart: true, includeEnd: true},
		{start: 0, end: 0, includeStart: false, includeEnd: false},
		{start: 2, end: 3, includeStart: true, includeEnd: true},
	}
	normalized, err := newIntervalSetFromSegments(temporal.Limits{}.Resolve(), segments)
	if err != nil || len(normalized.segments) != 2 || normalized.segments[0].start != 2 || normalized.segments[1].start != 4 {
		t.Fatalf("newIntervalSetFromSegments() = %+v, %v", normalized.segments, err)
	}
	if _, err := newIntervalSetFromSegments(
		temporal.Limits{OutputPeriods: 1}.Resolve(),
		[]dailySegment{{start: 0, end: 1, includeStart: true, includeEnd: true}, {start: 3, end: 4, includeStart: true, includeEnd: true}},
	); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("newIntervalSetFromSegments(limit) error = %v", err)
	} else {
		var limitError *temporal.LimitError
		if !errors.As(err, &limitError) || limitError.Value != 2 || limitError.Max != 1 {
			t.Fatalf("newIntervalSetFromSegments(limit) detail = %+v", err)
		}
	}

	base := dailySegment{start: 1, end: 3, includeStart: true, includeEnd: true}
	comparisons := []struct {
		left  dailySegment
		right dailySegment
		want  int
	}{
		{base, base, 0},
		{dailySegment{start: 0}, base, -1},
		{base, dailySegment{start: 2}, -1},
		{base, dailySegment{start: 1, end: 3, includeStart: false, includeEnd: true}, -1},
		{dailySegment{start: 1, end: 2, includeStart: true}, base, -1},
		{base, dailySegment{start: 1, end: 3, includeStart: true, includeEnd: false}, -1},
	}
	for _, test := range comparisons {
		if got := compareDailySegments(test.left, test.right); got != test.want {
			t.Fatalf("compareDailySegments(%+v, %+v) = %d, want %d", test.left, test.right, got, test.want)
		}
	}

	for _, test := range []struct {
		value   dailySegment
		removed dailySegment
		want    []dailySegment
	}{
		{
			dailySegment{start: 0, end: 4, includeStart: true, includeEnd: true},
			dailySegment{start: 0, end: 2, includeStart: true, includeEnd: true},
			[]dailySegment{{start: 2, end: 4, includeStart: false, includeEnd: true}},
		},
		{
			dailySegment{start: 0, end: 4, includeStart: true, includeEnd: true},
			dailySegment{start: 2, end: 4, includeStart: true, includeEnd: true},
			[]dailySegment{{start: 0, end: 2, includeStart: true, includeEnd: false}},
		},
	} {
		got := subtractDaily(test.value, test.removed)
		if len(got) != len(test.want) {
			t.Fatalf("subtractDaily() count = %d, want %d", len(got), len(test.want))
		}
		for index := range got {
			if got[index] != test.want[index] {
				t.Fatalf("subtractDaily() %d = %+v, want %+v", index, got[index], test.want[index])
			}
		}
	}
	dayRounded, err := Noon().Round(day, RoundNearest)
	if err != nil || !dayRounded.IsEndBoundary() {
		t.Fatalf("Round(day) = %v, %v", dayRounded, err)
	}
	if _, err := Noon().Round(day+1, RoundNearest); !errors.Is(err, temporal.ErrStep) {
		t.Fatalf("Round(over day) error = %v", err)
	}
}

func TestTimeOffsetParsingComponentsAndClampingUseExactBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		offset time.Duration
		digits int
		want   string
	}{
		{0, 0, "00:00:00"},
		{day, 0, "24:00"},
		{time.Hour + 2*time.Minute + 3*time.Second + 4, 9, "01:02:03.000000004"},
	} {
		value, err := FromOffset(test.offset, test.digits)
		if err != nil || value.String() != test.want {
			t.Fatalf("FromOffset(%v, %d) = %v, %v; want %q", test.offset, test.digits, value, err, test.want)
		}
	}
	for _, test := range []struct {
		offset time.Duration
		digits int
		err    error
	}{
		{-1, 0, temporal.ErrInvalidTime},
		{day + 1, 0, temporal.ErrInvalidTime},
		{0, -1, temporal.ErrPrecision},
		{0, 10, temporal.ErrPrecision},
		{1, 8, temporal.ErrPrecision},
	} {
		if _, err := FromOffset(test.offset, test.digits); !errors.Is(err, test.err) {
			t.Fatalf("FromOffset(%v, %d) error = %v", test.offset, test.digits, err)
		}
	}

	parsed, err := Parse("12:34", temporal.Limits{ParseBytes: 5})
	if err != nil || parsed.String() != "12:34" {
		t.Fatalf("Parse(exact byte limit) = %v, %v", parsed, err)
	}
	for _, input := range []string{"01:02:03.1", "01:02:03.123456789"} {
		parsed, err := Parse(input, temporal.Limits{})
		if err != nil || parsed.String() != input {
			t.Fatalf("Parse(%q) = %v, %v", input, parsed, err)
		}
	}
	if _, err := Parse("12:34x56", temporal.Limits{}); !errors.Is(err, temporal.ErrParse) {
		t.Fatalf("Parse(invalid seconds separator) error = %v", err)
	}

	value := mutationTime(t, 1, 2, 3, 4, 9)
	hour, minute, second, nanosecond := value.Components()
	if hour != 1 || minute != 2 || second != 3 || nanosecond != 4 {
		t.Fatalf("Components() = %d:%d:%d.%d", hour, minute, second, nanosecond)
	}
	minimum := mutationTime(t, 1, 0, 0, 0, 0)
	maximum := mutationTime(t, 2, 0, 0, 0, 0)
	for _, endpoint := range []Time{minimum, maximum} {
		clamped, err := endpoint.Clamp(minimum, maximum)
		if err != nil || !clamped.Equal(endpoint) {
			t.Fatalf("Clamp(endpoint %v) = %v, %v", endpoint, clamped, err)
		}
	}
	if clamped, err := minimum.Clamp(minimum, minimum); err != nil || !clamped.Equal(minimum) {
		t.Fatalf("Clamp(singleton) = %v, %v", clamped, err)
	}
}

func TestTimeShiftDistanceRoundAndDigitParsingUseExactBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		base     Time
		duration time.Duration
		policy   WrapPolicy
		want     Time
	}{
		{Midnight(), day, Wrap, Midnight()},
		{Midnight(), -day, Wrap, Midnight()},
		{EndOfDay(), time.Hour, Wrap, mutationTime(t, 1, 0, 0, 0, 0)},
		{Midnight(), day, RejectOverflow, EndOfDay()},
		{EndOfDay(), -day, RejectOverflow, Midnight()},
	} {
		shifted, err := test.base.Shift(test.duration, test.policy)
		if err != nil || !shifted.Equal(test.want) {
			t.Fatalf("Shift(%v, %v, %v) = %v, %v; want %v", test.base, test.duration, test.policy, shifted, err, test.want)
		}
	}
	if _, err := Midnight().Shift(day+1, RejectOverflow); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Shift(beyond end) error = %v", err)
	}
	if _, err := Midnight().Shift(-1, RejectOverflow); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Shift(before start) error = %v", err)
	}

	if distance := Noon().CircularDistance(Noon()); distance != 0 {
		t.Fatalf("CircularDistance(equal) = %v", distance)
	}
	if distance := Midnight().CircularDistance(Noon()); distance != 12*time.Hour {
		t.Fatalf("CircularDistance(half day) = %v", distance)
	}

	for _, test := range []struct {
		value Time
		mode  RoundingMode
		want  Time
	}{
		{mutationTime(t, 1, 0, 0, 0, 0), RoundFloor, mutationTime(t, 1, 0, 0, 0, 0)},
		{mutationTime(t, 1, 29, 59, 0, 0), RoundNearest, mutationTime(t, 1, 0, 0, 0, 0)},
		{mutationTime(t, 1, 30, 0, 0, 0), RoundNearest, mutationTime(t, 2, 0, 0, 0, 0)},
		{mutationTime(t, 23, 1, 0, 0, 0), RoundCeil, EndOfDay()},
	} {
		rounded, err := test.value.Round(time.Hour, test.mode)
		if err != nil || !rounded.Equal(test.want) {
			t.Fatalf("Round(%v, %v) = %v, %v; want %v", test.value, test.mode, rounded, err, test.want)
		}
	}

	for _, test := range []struct {
		value string
		want  int
		ok    bool
	}{
		{"00", 0, true},
		{"09", 9, true},
		{"90", 90, true},
		{"0", 0, false},
		{"000", 0, false},
		{"/0", 0, false},
		{":0", 0, false},
		{"0/", 0, false},
		{"0:", 0, false},
	} {
		got, ok := twoDigits(test.value)
		if got != test.want || ok != test.ok {
			t.Fatalf("twoDigits(%q) = %d, %v; want %d, %v", test.value, got, ok, test.want, test.ok)
		}
	}
}

func TestCollapsedIntervalToInstantRemainsAnExplicitEmptyPeriod(t *testing.T) {
	t.Parallel()

	interval := Collapsed(Noon())
	period, err := interval.ToInstant(calendar.MustDate(2026, time.January, 2), time.UTC, calendartz.Reject)
	if err != nil || !period.IsEmpty() || period.Bounds() != temporal.Open {
		t.Fatalf("Collapsed.ToInstant() = %+v, %v", period, err)
	}
}

func TestNonCollapsedIntervalToInstantPreservesBounds(t *testing.T) {
	t.Parallel()

	interval := mutationInterval(t, 8, 10, temporal.Closed)
	period, err := interval.ToInstant(calendar.MustDate(2026, time.January, 2), time.UTC, calendartz.Reject)
	if err != nil || period.Bounds() != temporal.Closed {
		t.Fatalf("Interval.ToInstant() = %+v, %v; want closed bounds", period, err)
	}
}
