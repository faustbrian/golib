package openinghours

import (
	"database/sql/driver"
	"testing"
	"time"
)

func TestSegmentAlgebraBoundaryMatrix(t *testing.T) {
	if normalizeSegments(nil) != nil {
		t.Fatal("nil segment normalization changed representation")
	}
	input := []segment{{start: 5, end: 7}, {start: 1, end: 3}, {start: 1, end: 2}, {start: 3, end: 6}}
	got := normalizeSegments(input)
	if len(got) != 1 || got[0] != (segment{start: 1, end: 7}) {
		t.Fatalf("normalizeSegments() = %#v", got)
	}
	if input[0] != (segment{start: 5, end: 7}) {
		t.Fatal("normalizeSegments mutated input")
	}

	intersections := []struct {
		left, right []segment
		want        []segment
	}{
		{nil, nil, nil},
		{[]segment{{0, 2}}, []segment{{2, 4}}, []segment{}},
		{[]segment{{0, 5}}, []segment{{1, 2}, {3, 6}}, []segment{{1, 2}, {3, 5}}},
		{[]segment{{1, 2}, {3, 6}}, []segment{{0, 5}}, []segment{{1, 2}, {3, 5}}},
	}
	for _, test := range intersections {
		result := intersectSegments(test.left, test.right)
		if len(result) != len(test.want) {
			t.Errorf("intersectSegments(%#v,%#v) = %#v", test.left, test.right, result)
			continue
		}
		for index := range result {
			if result[index] != test.want[index] {
				t.Errorf("intersection item %d = %#v, want %#v", index, result[index], test.want[index])
			}
		}
	}

	base := []segment{{start: 0, end: 10}}
	removals := []segment{{start: -1, end: 2}, {start: 4, end: 6}, {start: 8, end: 12}, {start: 20, end: 30}}
	result := subtractSegments(base, removals)
	if len(result) != 2 || result[0] != (segment{2, 4}) || result[1] != (segment{6, 8}) {
		t.Fatalf("subtractSegments() = %#v", result)
	}
}

func TestInstantRangeNormalizationAndClippingMatrix(t *testing.T) {
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if got := normalizeInstantRanges(nil); got == nil || len(got) != 0 {
		t.Fatalf("empty instant normalization = %#v", got)
	}
	input := []InstantRange{
		{Start: base.Add(4 * time.Hour), End: base.Add(6 * time.Hour)},
		{Start: base, End: base.Add(2 * time.Hour)},
		{Start: base, End: base.Add(time.Hour)},
		{Start: base.Add(2 * time.Hour), End: base.Add(5 * time.Hour)},
	}
	got := normalizeInstantRanges(input)
	if len(got) != 1 || !got[0].Start.Equal(base) || !got[0].End.Equal(base.Add(6*time.Hour)) {
		t.Fatalf("normalizeInstantRanges() = %#v", got)
	}
	if latestInstant(base, base.Add(time.Hour)) != base.Add(time.Hour) ||
		latestInstant(base.Add(time.Hour), base) != base.Add(time.Hour) ||
		earliestInstant(base, base.Add(time.Hour)) != base ||
		earliestInstant(base.Add(time.Hour), base) != base {
		t.Fatal("instant min/max helpers failed")
	}
}

func TestLocalAndInstantQueryFailureMatrix(t *testing.T) {
	schedule := internalSchedule(t, Config{})
	validDate := MustDate(2026, time.January, 1)
	validTime := internalTime(t, 12, 0)
	if _, err := schedule.IsOpenLocal(Date{}, validTime, RejectDST); !IsCode(err, CodeInvalidDate) {
		t.Fatalf("invalid date query error = %v", err)
	}
	if _, err := schedule.IsOpenLocal(validDate, LocalTime{nanosecond: -1}, RejectDST); !IsCode(err, CodeInvalidTime) {
		t.Fatalf("invalid time query error = %v", err)
	}
	if result, err := (Schedule{}).IsOpenLocal(validDate, validTime, RejectDST); err != nil || result.Open || result.Explanation.Rule != RuleNone {
		t.Fatalf("zero IsOpenLocal = %#v error=%v", result, err)
	}
	if _, err := schedule.IsOpenLocal(validDate, validTime, LocalResolutionPolicy(99)); !IsCode(err, CodeInvalidState) {
		t.Fatalf("invalid IsOpenLocal policy error = %v", err)
	}
	if result, err := (Schedule{}).IsOpen(time.Now()); err != nil || result.Open || result.Explanation.Rule != RuleNone {
		t.Fatalf("zero IsOpen = %#v error=%v", result, err)
	}

	if _, err := schedule.EffectiveRanges(Date{}); !IsCode(err, CodeInvalidDate) {
		t.Fatalf("invalid EffectiveRanges error = %v", err)
	}
	if ranges, err := (Schedule{}).EffectiveRanges(validDate); err != nil || len(ranges) != 0 {
		t.Fatalf("zero EffectiveRanges = %#v error=%v", ranges, err)
	}
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, interval := range [][2]time.Time{{start, start}, {start, start.Add(-time.Hour)}, {start, start.Add(maxSearchHorizon + time.Hour)}} {
		if _, err := schedule.EffectiveInstantRanges(interval[0], interval[1]); !IsCode(err, CodeInvalidInterval) {
			t.Errorf("invalid instant range %#v error = %v", interval, err)
		}
	}
	if ranges, err := (Schedule{}).EffectiveInstantRanges(start, start.Add(time.Hour)); err != nil || len(ranges) != 0 {
		t.Fatalf("zero instant ranges = %#v error=%v", ranges, err)
	}
	if duration, err := (Schedule{}).OpenDuration(start, start.Add(time.Hour)); err != nil || duration != 0 {
		t.Fatalf("zero OpenDuration = %s error=%v", duration, err)
	}
}

func TestEffectiveRangesFullDayAndConsecutiveInstantMerge(t *testing.T) {
	schedule := internalSchedule(t, Config{Weekly: map[time.Weekday]DayRule{
		time.Thursday: OpenAllDay(), time.Friday: OpenAllDay(),
	}})
	date := MustDate(2026, time.January, 1)
	ranges, err := schedule.EffectiveRanges(date)
	if err != nil || len(ranges) != 1 || ranges[0].Start != (LocalTime{}) ||
		ranges[0].End != (LocalTime{}) || !ranges[0].EndAtDayBoundary {
		t.Fatalf("full-day ranges = %#v error=%v", ranges, err)
	}
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	instantRanges, err := schedule.EffectiveInstantRanges(start, start.Add(48*time.Hour))
	if err != nil || len(instantRanges) != 1 || instantRanges[0].End.Sub(instantRanges[0].Start) != 48*time.Hour {
		t.Fatalf("consecutive instant ranges = %#v error=%v", instantRanges, err)
	}
}

func TestCompositionAndOverlayInternalBranches(t *testing.T) {
	date := MustDate(2026, time.January, 1)
	base := internalSchedule(t, Config{})
	invalid := Schedule{data: &scheduleData{
		timezone: "UTC", location: time.UTC, depth: 1,
		composition: &composition{operation: compositionOperation(99), left: base, right: base},
	}}
	segments, _, _, err := invalid.effectiveSegments(date)
	if err != nil || len(segments) != 0 {
		t.Fatalf("unknown composition result = %#v error=%v", segments, err)
	}

	start := MustDate(2026, time.January, 2)
	outOfRange := internalSchedule(t, Config{EffectiveStart: &start})
	if mask, err := outOfRange.overlayMask(date); err != nil || mask != nil {
		t.Fatalf("outside overlay mask = %#v error=%v", mask, err)
	}
	errorOutside := internalSchedule(t, Config{EffectiveStart: &start, OutsideEffective: OutsideError})
	if _, err := errorOutside.overlayMask(date); !IsCode(err, CodeOutsideEffectiveRange) {
		t.Fatalf("outside overlay error = %v", err)
	}
	composed, _ := base.Union(base)
	if mask, err := composed.overlayMask(date); err != nil || len(mask) != 1 || mask[0].end != nanosecondsPerDay {
		t.Fatalf("composition overlay mask = %#v error=%v", mask, err)
	}
	if mask, err := base.overlayMask(MustDate(1, time.January, 1)); err != nil || mask != nil {
		t.Fatalf("minimum-date overlay mask = %#v error=%v", mask, err)
	}
}

func TestTransitionSearchFailureAndFilteringMatrix(t *testing.T) {
	instant := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	schedule := internalSchedule(t, Config{})
	for _, horizon := range []time.Duration{0, -time.Hour, maxSearchHorizon + time.Hour} {
		if _, err := schedule.NextTransition(instant, horizon); !IsCode(err, CodeInvalidHorizon) {
			t.Errorf("NextTransition horizon %s error = %v", horizon, err)
		}
		if _, err := schedule.PreviousTransition(instant, horizon); !IsCode(err, CodeInvalidHorizon) {
			t.Errorf("PreviousTransition horizon %s error = %v", horizon, err)
		}
		if _, err := schedule.NextOpening(instant, horizon); !IsCode(err, CodeInvalidHorizon) {
			t.Errorf("NextOpening horizon %s error = %v", horizon, err)
		}
	}
	if _, err := (Schedule{}).NextTransition(instant, time.Hour); !IsCode(err, CodeSearchExhausted) {
		t.Fatalf("zero NextTransition error = %v", err)
	}
	if _, err := (Schedule{}).PreviousTransition(instant, time.Hour); !IsCode(err, CodeSearchExhausted) {
		t.Fatalf("zero PreviousTransition error = %v", err)
	}
	if _, err := schedule.NextClosing(instant, time.Hour); !IsCode(err, CodeSearchExhausted) {
		t.Fatalf("empty NextClosing error = %v", err)
	}
}

func TestDSTResolutionFailureMatrixAndGapBoundary(t *testing.T) {
	date := MustDate(2026, time.January, 1)
	local := internalTime(t, 12, 0)
	schedule := internalSchedule(t, Config{Timezone: "Europe/Helsinki"})
	if _, err := schedule.ResolveLocal(Date{}, local, RejectDST); !IsCode(err, CodeInvalidDate) {
		t.Fatalf("invalid ResolveLocal date error = %v", err)
	}
	if _, err := schedule.ResolveLocal(date, LocalTime{nanosecond: -1}, RejectDST); !IsCode(err, CodeInvalidTime) {
		t.Fatalf("invalid ResolveLocal time error = %v", err)
	}
	if _, err := (Schedule{}).ResolveLocal(date, local, RejectDST); !IsCode(err, CodeInvalidTimezone) {
		t.Fatalf("zero ResolveLocal error = %v", err)
	}
	if _, err := schedule.ResolveLocal(date, local, LocalResolutionPolicy(99)); !IsCode(err, CodeInvalidState) {
		t.Fatalf("invalid ResolveLocal policy error = %v", err)
	}
	resolved, err := schedule.ResolveLocal(date, local, ShiftForward)
	if err != nil || resolved.Kind != LocalExact {
		t.Fatalf("exact ShiftForward = %#v error=%v", resolved, err)
	}

	gapDate := MustDate(2026, time.March, 29)
	gapTime := internalTime(t, 3, 30)
	boundary, err := schedule.resolveBoundary(gapDate, gapTime.nanosecond, true)
	if err != nil || !boundary.Equal(time.Date(2026, time.March, 29, 1, 30, 0, 0, time.UTC)) {
		t.Fatalf("gap boundary = %s error=%v", boundary, err)
	}
	if _, err := schedule.resolveBoundary(MustDate(9999, time.December, 31), nanosecondsPerDay, false); !IsCode(err, CodeInvalidDate) {
		t.Fatalf("overflow boundary error = %v", err)
	}
}

func TestObservationTransitionOutcomesAndBounds(t *testing.T) {
	rule := internalRule(t, internalRange(t, 9, 0, 10, 0))
	schedule := internalSchedule(t, Config{Weekly: map[time.Weekday]DayRule{time.Monday: rule}})
	instant := time.Date(2026, time.January, 5, 8, 0, 0, 0, time.UTC)
	var observation Observation
	transition, err := schedule.ObserveNextTransition(instant, 2*time.Hour, nil, func(value Observation) { observation = value })
	if err != nil || transition.Kind != TransitionOpen || observation.Operation != OperationNextTransition ||
		observation.Outcome != OutcomeFound || observation.SearchSteps != 1 {
		t.Fatalf("observed transition = %#v error=%v observation=%#v", transition, err, observation)
	}
	_, err = schedule.ObserveNextTransition(instant, 0, nil, func(value Observation) { observation = value })
	if !IsCode(err, CodeInvalidHorizon) || observation.Outcome != OutcomeError || observation.SearchSteps != 0 {
		t.Fatalf("observed failure error=%v observation=%#v", err, observation)
	}
	if boundedSearchSteps(maxSearchHorizon+time.Hour) != 367 {
		t.Fatal("bounded search steps did not clamp")
	}
}

func TestPersistenceInternalFailureMatrix(t *testing.T) {
	oversized := Schedule{data: &scheduleData{timezone: "UTC", metadata: Metadata{Label: string(make([]byte, MaxJSONBytes))}}}
	if value, err := oversized.Value(); value != nil || !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("oversized Value() = %v error=%v", value, err)
	}
	var target *Schedule
	if err := target.Scan(nil); !IsCode(err, CodeInvalidState) {
		t.Fatalf("nil Scan error = %v", err)
	}
	schedule := internalSchedule(t, Config{})
	data, _ := schedule.CanonicalJSON()
	var decoded Schedule
	if err := decoded.Scan(string(data)); err != nil || !schedule.Equal(decoded) {
		t.Fatalf("string Scan error=%v", err)
	}
	if err := decoded.Scan([]byte(`{}`)); err == nil {
		t.Fatal("Scan accepted invalid JSON")
	}
	var _ driver.Valuer = schedule
}

func TestMalformedCompositionPropagatesQueryErrors(t *testing.T) {
	date := MustDate(2026, time.January, 1)
	start := MustDate(2026, time.January, 2)
	failing := internalSchedule(t, Config{EffectiveStart: &start, OutsideEffective: OutsideError})
	valid := internalSchedule(t, Config{})
	compose := func(operation compositionOperation, left, right Schedule) Schedule {
		return Schedule{data: &scheduleData{
			timezone: "UTC", location: time.UTC, depth: 2,
			composition: &composition{operation: operation, left: left, right: right},
		}}
	}
	for _, schedule := range []Schedule{
		compose(compositionUnion, failing, valid),
		compose(compositionUnion, valid, failing),
		compose(compositionOverlay, valid, failing),
	} {
		if _, err := schedule.IsOpenLocal(date, LocalTime{}, RejectDST); !IsCode(err, CodeOutsideEffectiveRange) {
			t.Errorf("composition query error = %v", err)
		}
		if _, err := schedule.EffectiveRanges(date); !IsCode(err, CodeOutsideEffectiveRange) {
			t.Errorf("composition ranges error = %v", err)
		}
	}
	if segments, _, _, err := internalSchedule(t, Config{EffectiveStart: &start}).effectiveSegments(date); err != nil || segments != nil {
		t.Fatalf("outside effective segments = %#v, %v", segments, err)
	}
	if _, _, _, err := failing.effectiveSegments(date); !IsCode(err, CodeOutsideEffectiveRange) {
		t.Fatalf("failing effective segments error = %v", err)
	}

	var observed Observation
	if _, err := compose(compositionUnion, failing, valid).ObserveIsOpen(
		time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC),
		nil,
		func(value Observation) { observed = value },
	); !IsCode(err, CodeOutsideEffectiveRange) || observed.Outcome != OutcomeError {
		t.Fatalf("observed error = %#v, %v", observed, err)
	}
	open := internalSchedule(t, Config{Weekly: map[time.Weekday]DayRule{time.Thursday: OpenAllDay()}})
	if result, err := open.ObserveIsOpen(
		time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC),
		nil,
		func(value Observation) { observed = value },
	); err != nil || !result.Open || observed.Outcome != OutcomeOpen {
		t.Fatalf("observed open = %#v, %#v, %v", result, observed, err)
	}
	if result, err := valid.ObserveIsOpen(
		time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC),
		nil,
		func(value Observation) { observed = value },
	); err != nil || result.Open || observed.Outcome != OutcomeClosed {
		t.Fatalf("observed closed = %#v, %#v, %v", result, observed, err)
	}
}

func TestInternalRuleAndSegmentCoverageMatrix(t *testing.T) {
	a := segment{start: 1, end: 3}
	b := segment{start: 1, end: 4}
	if got := normalizeSegments([]segment{b, a, b}); len(got) != 1 || got[0] != b {
		t.Fatalf("equal-start normalization = %#v", got)
	}
	for _, comparison := range []struct {
		left, right segment
		want        int
	}{
		{segment{start: 1}, segment{start: 2}, -1},
		{segment{start: 2}, segment{start: 1}, 1},
		{segment{start: 1, end: 2}, segment{start: 1, end: 3}, -1},
		{segment{start: 1, end: 3}, segment{start: 1, end: 2}, 1},
		{segment{start: 1, end: 2}, segment{start: 1, end: 2}, 0},
	} {
		if got := compareSegment(comparison.left, comparison.right); got != comparison.want {
			t.Errorf("compareSegment(%#v, %#v) = %d", comparison.left, comparison.right, got)
		}
	}
	replaced := applyExceptions([]segment{{start: 0, end: 1}}, []Exception{{
		operation: ExceptionReplace,
		rule:      DayRule{state: DayOpenRanges, ranges: []Range{{start: LocalTime{nanosecond: 2}, end: LocalTime{nanosecond: 3}}}},
	}})
	if len(replaced) != 1 || replaced[0] != (segment{start: 2, end: 3}) {
		t.Fatalf("replacement segments = %#v", replaced)
	}

	date := MustDate(2026, time.January, 1)
	if _, err := (Schedule{}).Ranges(Date{}); !IsCode(err, CodeInvalidDate) {
		t.Fatalf("invalid Ranges error = %v", err)
	}
	inherited := internalSchedule(t, Config{})
	if result, err := inherited.Ranges(date); err != nil || result.State != DayClosed {
		t.Fatalf("inherited ranges = %#v, %v", result, err)
	}

	start0 := internalTime(t, 0, 0)
	start1 := internalTime(t, 1, 0)
	start22 := internalTime(t, 22, 0)
	end23 := internalTime(t, 23, 0)
	end1 := internalTime(t, 1, 0)
	if _, err := OpenRanges([]Range{{start: start0, end: end23}, {start: start22, end: end1}}, MergeOverlap); !IsCode(err, CodeDayBoundaryOverflow) {
		t.Fatalf("overlap overflow error = %v", err)
	}
	if _, err := OpenRanges([]Range{{start: start1, end: end23}, {start: start22, end: end1}}, MergeOverlap); !IsCode(err, CodeDayBoundaryOverflow) {
		t.Fatalf("exact shifted day error = %v", err)
	}
}

func TestRangeCardinalityAndCivilBoundaryLimits(t *testing.T) {
	date := MustDate(2026, time.January, 1)
	fragments := make([]Range, maxOutputRanges+1)
	for index := range fragments {
		fragments[index] = Range{
			start: LocalTime{nanosecond: int64(index * 2)},
			end:   LocalTime{nanosecond: int64(index*2 + 1)},
		}
	}
	fragmented := Schedule{data: &scheduleData{
		timezone: "UTC", location: time.UTC, depth: 1,
		weekly: [7]DayRule{time.Thursday: {state: DayOpenRanges, ranges: fragments}},
	}}
	if _, err := fragmented.EffectiveRanges(date); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("daily fragmentation error = %v", err)
	}
	dayStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := fragmented.EffectiveInstantRanges(dayStart, dayStart.Add(time.Minute)); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("instant fragmentation error = %v", err)
	}
	effectiveStart := MustDate(2026, time.January, 2)
	failing := internalSchedule(t, Config{EffectiveStart: &effectiveStart, OutsideEffective: OutsideError})
	if _, err := failing.EffectiveInstantRanges(dayStart, dayStart.Add(time.Hour)); !IsCode(err, CodeOutsideEffectiveRange) {
		t.Fatalf("instant effective-range error = %v", err)
	}

	maximum := MustDate(9999, time.December, 31)
	maximumStart := time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)
	empty := internalSchedule(t, Config{})
	if ranges, err := empty.EffectiveInstantRanges(maximumStart, maximumStart.Add(time.Hour)); err != nil || len(ranges) != 0 {
		t.Fatalf("maximum-date empty ranges = %#v, %v", ranges, err)
	}
	full := internalSchedule(t, Config{Weekly: map[time.Weekday]DayRule{maximum.Weekday(): OpenAllDay()}})
	if _, err := full.EffectiveInstantRanges(maximumStart, maximumStart.Add(time.Hour)); !IsCode(err, CodeInvalidDate) {
		t.Fatalf("maximum-date boundary error = %v", err)
	}
	if _, err := full.resolveBoundary(maximum, nanosecondsPerDay, false); !IsCode(err, CodeInvalidDate) {
		t.Fatalf("maximum boundary error = %v", err)
	}
	if _, err := empty.OpenDuration(dayStart, dayStart); !IsCode(err, CodeInvalidInterval) {
		t.Fatalf("OpenDuration invalid interval error = %v", err)
	}
	if _, err := empty.resolveBoundary(date, -1, true); !IsCode(err, CodeInvalidTime) {
		t.Fatalf("invalid resolved boundary error = %v", err)
	}

	base := time.Unix(0, 0)
	normalized := normalizeInstantRanges([]InstantRange{
		{Start: base, End: base.Add(time.Second)},
		{Start: base.Add(2 * time.Second), End: base.Add(3 * time.Second)},
	})
	if len(normalized) != 2 {
		t.Fatalf("disjoint instant normalization = %#v", normalized)
	}
}

func TestTransitionSearchEdgeMatrix(t *testing.T) {
	maximumStart := time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)
	empty := internalSchedule(t, Config{})
	if _, err := empty.NextTransition(maximumStart, time.Hour); !IsCode(err, CodeSearchExhausted) {
		t.Fatalf("maximum next transition error = %v", err)
	}
	if _, err := empty.PreviousTransition(time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC), time.Hour); !IsCode(err, CodeSearchExhausted) {
		t.Fatalf("minimum previous transition error = %v", err)
	}

	date := MustDate(2026, time.January, 1)
	weekly := make(map[time.Weekday]DayRule, 7)
	for weekday := time.Sunday; weekday <= time.Saturday; weekday++ {
		weekly[weekday] = OpenAllDay()
	}
	full := internalSchedule(t, Config{Weekly: weekly})
	instant := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	if _, err := full.NextTransition(instant, 36*time.Hour); !IsCode(err, CodeSearchExhausted) {
		t.Fatalf("continuous next transition error = %v", err)
	}
	if _, err := full.PreviousTransition(instant.Add(24*time.Hour), 36*time.Hour); !IsCode(err, CodeSearchExhausted) {
		t.Fatalf("continuous previous transition error = %v", err)
	}

	opening := internalTime(t, 9, 0)
	closing := internalTime(t, 10, 0)
	rangeValue, _ := NewRange(opening, closing)
	rule, _ := OpenRanges([]Range{rangeValue}, RejectOverlap)
	timed := internalSchedule(t, Config{Weekly: map[time.Weekday]DayRule{date.Weekday(): rule}})
	before := time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC)
	if _, err := timed.NextOpening(before, time.Hour); !IsCode(err, CodeSearchExhausted) {
		t.Fatalf("typed transition deadline error = %v", err)
	}
	if transition, err := timed.PreviousTransition(
		time.Date(2026, time.January, 1, 10, 30, 0, 0, time.UTC), 2*time.Hour,
	); err != nil || transition.Kind != TransitionClose {
		t.Fatalf("previous closing = %#v, %v", transition, err)
	}

	end := date
	outside := internalSchedule(t, Config{
		Weekly:       map[time.Weekday]DayRule{date.Weekday(): OpenAllDay()},
		EffectiveEnd: &end, OutsideEffective: OutsideError,
	})
	if _, err := outside.NextTransition(instant, 24*time.Hour); !IsCode(err, CodeOutsideEffectiveRange) {
		t.Fatalf("next transition query propagation = %v", err)
	}
	if _, err := outside.PreviousTransition(instant.Add(24*time.Hour), 24*time.Hour); !IsCode(err, CodeOutsideEffectiveRange) {
		t.Fatalf("previous transition query propagation = %v", err)
	}

	if _, err := empty.IsOpen(time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)); !IsCode(err, CodeInvalidDate) {
		t.Fatalf("out-of-domain instant error = %v", err)
	}
}
