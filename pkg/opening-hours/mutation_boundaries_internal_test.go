package openinghours

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCompositionAndExceptionLimitBoundaries(t *testing.T) {
	base := internalSchedule(t, Config{})
	if _, err := (Schedule{}).Union(base); !IsCode(err, CodeTimezoneMismatch) {
		t.Fatalf("zero left composition error = %v", err)
	}
	if _, err := base.Union(Schedule{}); !IsCode(err, CodeTimezoneMismatch) {
		t.Fatalf("zero right composition error = %v", err)
	}
	nearLimit := Schedule{data: &scheduleData{timezone: "UTC", location: time.UTC, depth: MaxCompositionDepth - 1}}
	maximum, err := nearLimit.Union(base)
	if err != nil || maximum.data.depth != MaxCompositionDepth {
		t.Fatalf("maximum composition depth = %d, error=%v", maximum.data.depth, err)
	}
	if _, err := maximum.Union(base); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("composition beyond maximum error = %v", err)
	}

	date := MustDate(2026, time.January, 1)
	closure, err := NewException(ExceptionConfig{
		Date: date, Operation: ExceptionClose, Source: "source", Revision: "revision",
	})
	if err != nil {
		t.Fatal(err)
	}
	maximumName := strings.Repeat("n", maxProvenanceBytes)
	maximumSet := make([]Exception, maxExceptions)
	for index := range maximumSet {
		maximumSet[index] = closure
	}
	set, err := NewExceptionSet(maximumName, maximumSet)
	if err != nil || set.Name() != maximumName || len(set.Exceptions()) != maxExceptions {
		t.Fatalf("maximum exception set length = %d, error=%v", len(set.Exceptions()), err)
	}

	invalidItems := []Exception{
		{date: Date{}, source: "source", revision: "revision"},
		{date: date, source: "", revision: "revision"},
		{date: date, source: "source", revision: ""},
	}
	for _, item := range invalidItems {
		if _, err := NewExceptionSet("set", []Exception{item}); !IsCode(err, CodeInvalidState) {
			t.Errorf("invalid exception set item %#v error = %v", item, err)
		}
	}

	rangeSet, err := ExpandExceptionRange(ExceptionRangeConfig{
		Name: "single", Start: date, End: date, MaximumDates: maxExceptions,
		Operation: ExceptionClose, Source: "source", Revision: "revision",
	})
	if err != nil || len(rangeSet.Exceptions()) != 1 {
		t.Fatalf("maximum range limit single date = %#v, error=%v", rangeSet, err)
	}
	minimumSet, err := ExpandExceptionRange(ExceptionRangeConfig{
		Name: "single", Start: date, End: date, MaximumDates: 1,
		Operation: ExceptionClose, Source: "source", Revision: "minimum",
	})
	if err != nil || len(minimumSet.Exceptions()) != 1 {
		t.Fatalf("minimum positive range limit = %#v, error=%v", minimumSet, err)
	}
	for _, priority := range []int{-1_000_000, 1_000_000} {
		if _, err := NewException(ExceptionConfig{
			Date: date, Operation: ExceptionClose, Priority: priority,
			Source: "source", Revision: fmt.Sprintf("%d", priority),
		}); err != nil {
			t.Errorf("boundary priority %d error = %v", priority, err)
		}
	}

	nextDate := MustDate(2026, time.January, 2)
	differentDates := []Exception{
		{date: date, priority: 1, source: "a", revision: "1"},
		{date: nextDate, priority: 1, source: "b", revision: "1"},
	}
	if normalized, err := normalizeExceptions(differentDates, RejectAmbiguous); err != nil || len(normalized) != 2 {
		t.Fatalf("equal priorities on different dates = %#v, error=%v", normalized, err)
	}
	third := MustDate(2026, time.January, 3)
	ambiguousAfterDifferentDate := []Exception{
		{date: date, priority: 1, source: "a", revision: "1"},
		{date: third, priority: 1, source: "b", revision: "1"},
		{date: third, priority: 1, source: "c", revision: "1"},
	}
	if _, err := normalizeExceptions(ambiguousAfterDifferentDate, RejectAmbiguous); !IsCode(err, CodeAmbiguousException) {
		t.Fatalf("ambiguity after different date error = %v", err)
	}

	for _, test := range []struct {
		name   string
		config ExceptionRangeConfig
		code   Code
	}{
		{"invalid start", ExceptionRangeConfig{Name: "x", End: date, MaximumDates: 1}, CodeInvalidDate},
		{"invalid end", ExceptionRangeConfig{Name: "x", Start: date, MaximumDates: 1}, CodeInvalidDate},
		{"zero limit", ExceptionRangeConfig{Name: "x", Start: date, End: date}, CodeLimitExceeded},
		{"negative limit", ExceptionRangeConfig{Name: "x", Start: date, End: date, MaximumDates: -1}, CodeLimitExceeded},
	} {
		if _, err := ExpandExceptionRange(test.config); !IsCode(err, test.code) {
			t.Errorf("%s error = %v, want %s", test.name, err, test.code)
		}
	}
	if _, err := NewException(ExceptionConfig{
		Date: date, Operation: ExceptionClose, Priority: -1_000_001,
		Source: "source", Revision: "below",
	}); !IsCode(err, CodeInvalidState) {
		t.Fatalf("priority below minimum error = %v", err)
	}
}

func TestMetadataSummaryAndValueBoundaries(t *testing.T) {
	maximum := strings.Repeat("m", maxMetadataBytes)
	for _, metadata := range []Metadata{
		{Label: maximum}, {Source: maximum}, {Revision: maximum},
	} {
		if _, err := validateMetadata(metadata); err != nil {
			t.Errorf("maximum metadata %#v error = %v", metadata, err)
		}
	}
	invalidUTF8 := string([]byte{0xff})
	for _, metadata := range []Metadata{
		{Label: invalidUTF8}, {Source: invalidUTF8}, {Revision: invalidUTF8},
	} {
		if _, err := validateMetadata(metadata); !IsCode(err, CodeLimitExceeded) {
			t.Errorf("invalid UTF-8 metadata %#v error = %v", metadata, err)
		}
	}

	if summary, err := boundedHumanSummary(strings.Repeat("s", MaxHumanSummaryBytes)); err != nil || len(summary) != MaxHumanSummaryBytes {
		t.Fatalf("maximum summary length = %d, error=%v", len(summary), err)
	}
	if _, err := boundedHumanSummary(strings.Repeat("s", MaxHumanSummaryBytes+1)); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("oversized summary error = %v", err)
	}

	left := internalSchedule(t, Config{})
	right := internalSchedule(t, Config{Weekly: map[time.Weekday]DayRule{time.Thursday: OpenAllDay()}})
	if left.SemanticallyEqual(right) {
		t.Fatal("semantically different schedules compare equal")
	}
	if (Range{}).Overnight() {
		t.Fatal("equal endpoints must not be overnight")
	}
}

func TestSegmentAndScheduleBoundaryMatrix(t *testing.T) {
	adjacent := normalizeSegments([]segment{{start: 1, end: 2}, {start: 2, end: 3}})
	if len(adjacent) != 1 || adjacent[0] != (segment{start: 1, end: 3}) {
		t.Fatalf("adjacent normalization = %#v", adjacent)
	}
	intersection := intersectSegments(
		[]segment{{start: 0, end: 2}, {start: 3, end: 5}},
		[]segment{{start: 1, end: 2}, {start: 4, end: 6}},
	)
	if len(intersection) != 2 || intersection[0] != (segment{start: 1, end: 2}) ||
		intersection[1] != (segment{start: 4, end: 5}) {
		t.Fatalf("equal-end intersection = %#v", intersection)
	}
	for _, test := range []struct {
		name     string
		removal  segment
		expected []segment
	}{
		{"same start", segment{start: 1, end: 2}, []segment{{start: 2, end: 3}}},
		{"same end", segment{start: 2, end: 3}, []segment{{start: 1, end: 2}}},
		{"touch left", segment{start: 0, end: 1}, []segment{{start: 1, end: 3}}},
		{"touch right", segment{start: 3, end: 4}, []segment{{start: 1, end: 3}}},
	} {
		got := subtractSegments([]segment{{start: 1, end: 3}}, []segment{test.removal})
		if len(got) != len(test.expected) || (len(got) == 1 && got[0] != test.expected[0]) {
			t.Errorf("%s subtraction = %#v", test.name, got)
		}
	}
	if clipped := clipSegments([]segment{{start: 0, end: 1}}, 1, 2, 0); len(clipped) != 0 {
		t.Fatalf("zero-width clip = %#v", clipped)
	}

	maximumRanges := make([]Range, maxRangesPerDay)
	for index := range maximumRanges {
		maximumRanges[index] = Range{
			start: LocalTime{nanosecond: int64(index * 2)},
			end:   LocalTime{nanosecond: int64(index*2 + 1)},
		}
	}
	if rule, err := OpenRanges(maximumRanges, RejectOverlap); err != nil || len(rule.ranges) != maxRangesPerDay {
		t.Fatalf("maximum daily ranges = %d, error=%v", len(rule.ranges), err)
	}
	invalidStart := Range{start: LocalTime{nanosecond: -1}, end: LocalTime{nanosecond: 1}}
	invalidEnd := Range{start: LocalTime{}, end: LocalTime{nanosecond: nanosecondsPerDay}}
	for _, item := range []Range{invalidStart, invalidEnd} {
		if _, err := OpenRanges([]Range{item}, RejectOverlap); !IsCode(err, CodeInvalidRange) {
			t.Errorf("invalid stored range %#v error = %v", item, err)
		}
	}

	contained := []Range{
		{start: LocalTime{nanosecond: 1}, end: LocalTime{nanosecond: 5}},
		{start: LocalTime{nanosecond: 2}, end: LocalTime{nanosecond: 4}},
		{start: LocalTime{nanosecond: 6}, end: LocalTime{nanosecond: 7}},
	}
	if rule, err := OpenRanges(contained, MergeOverlap); err != nil || len(rule.ranges) != 2 {
		t.Fatalf("contained overlap with successor = %#v, error=%v", rule.ranges, err)
	}
	mergedAdjacent := []Range{
		{start: LocalTime{nanosecond: 1}, end: LocalTime{nanosecond: 2}},
		{start: LocalTime{nanosecond: 2}, end: LocalTime{nanosecond: 3}},
		{start: LocalTime{nanosecond: 4}, end: LocalTime{nanosecond: 5}},
	}
	if rule, err := OpenRanges(mergedAdjacent, MergeAdjacent); err != nil || len(rule.ranges) != 2 || rule.ranges[0].end.nanosecond != 3 {
		t.Fatalf("adjacent merge with successor = %#v, error=%v", rule.ranges, err)
	}
	overnight := Range{
		start: LocalTime{nanosecond: int64(23 * time.Hour)},
		end:   LocalTime{nanosecond: 0},
	}
	if rule, err := OpenRanges([]Range{overnight}, RejectOverlap); err != nil ||
		len(rule.ranges) != 1 || rule.ranges[0].end.nanosecond != 0 {
		t.Fatalf("midnight-ending overnight range = %#v, error=%v", rule.ranges, err)
	}

	date := MustDate(2026, time.January, 1)
	equalBounds, err := NewSchedule(Config{Timezone: "UTC", EffectiveStart: &date, EffectiveEnd: &date})
	if err != nil || equalBounds.data.effectiveStart != date || equalBounds.data.effectiveEnd != date {
		t.Fatalf("equal effective bounds = %#v, error=%v", equalBounds, err)
	}
	validSet, err := NewExceptionSet("set", []Exception{{
		date: date, operation: ExceptionClose, source: "source", revision: "revision",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSchedule(Config{Timezone: "UTC", ExceptionSets: []ExceptionSet{validSet, {}}}); !IsCode(err, CodeInvalidState) {
		t.Fatalf("invalid trailing exception set error = %v", err)
	}
	if _, err := NewSchedule(Config{
		Timezone: "UTC", ExceptionSets: []ExceptionSet{{name: "empty"}},
	}); !IsCode(err, CodeInvalidState) {
		t.Fatalf("named empty exception set error = %v", err)
	}
	maximumExceptions := make([]Exception, maxExceptions)
	for index := range maximumExceptions {
		maximumExceptions[index] = Exception{
			date: date, operation: ExceptionClose, priority: index,
			source: "source", revision: fmt.Sprintf("revision-%d", index),
		}
	}
	if schedule, err := NewSchedule(Config{Timezone: "UTC", Exceptions: maximumExceptions}); err != nil ||
		len(schedule.data.exceptions) != maxExceptions {
		t.Fatalf("maximum schedule exceptions = %d, error=%v", len(schedule.data.exceptions), err)
	}
	maximumExceptionSet := ExceptionSet{name: "maximum", exceptions: maximumExceptions}
	if schedule, err := NewSchedule(Config{Timezone: "UTC", ExceptionSets: []ExceptionSet{maximumExceptionSet}}); err != nil ||
		len(schedule.data.exceptions) != maxExceptions {
		t.Fatalf("maximum exception-set expansion = %d, error=%v", len(schedule.data.exceptions), err)
	}
}

func TestQueryExplanationAndOverlayBoundaries(t *testing.T) {
	monday := MustDate(2026, time.January, 5)
	tuesday := MustDate(2026, time.January, 6)
	overnight := internalRule(t, internalRange(t, 22, 0, 2, 0))
	current := internalRule(t, internalRange(t, 3, 0, 4, 0))
	schedule := internalSchedule(t, Config{Weekly: map[time.Weekday]DayRule{
		time.Monday:  overnight,
		time.Tuesday: current,
	}})

	closedAfterSpill, err := schedule.IsOpenLocal(tuesday, internalTime(t, 2, 30), RejectDST)
	if err != nil || closedAfterSpill.Open || closedAfterSpill.Explanation.Rule != RuleWeekly {
		t.Fatalf("closed point after spill = %#v, error=%v", closedAfterSpill, err)
	}
	currentOpening, err := schedule.IsOpenLocal(tuesday, internalTime(t, 3, 30), RejectDST)
	if err != nil || !currentOpening.Open || currentOpening.Explanation.Rule != RuleWeekly {
		t.Fatalf("current opening after spill = %#v, error=%v", currentOpening, err)
	}

	spillOnly := internalSchedule(t, Config{Weekly: map[time.Weekday]DayRule{time.Monday: overnight}})
	mask, err := spillOnly.overlayMask(tuesday)
	wantEnd := int64(2 * time.Hour)
	if err != nil || len(mask) != 1 || mask[0] != (segment{start: 0, end: wantEnd}) {
		t.Fatalf("overnight overlay mask = %#v, error=%v", mask, err)
	}
	if mask, err := schedule.overlayMask(monday); err != nil || len(mask) != 1 || mask[0].end != nanosecondsPerDay {
		t.Fatalf("explicit current-day overlay mask = %#v, error=%v", mask, err)
	}

	earliest := MustDate(1, time.January, 1)
	earliestRule := internalRule(t, internalRange(t, 3, 0, 4, 0))
	earliestSchedule := internalSchedule(t, Config{Weekly: map[time.Weekday]DayRule{
		earliest.Weekday(): earliestRule,
	}})
	earliestOpening, err := earliestSchedule.IsOpenLocal(earliest, internalTime(t, 3, 30), RejectDST)
	if err != nil || !earliestOpening.Open || earliestOpening.Explanation.Rule != RuleWeekly {
		t.Fatalf("earliest-date opening = %#v, error=%v", earliestOpening, err)
	}
}

func TestEffectiveRangeAndSearchBoundaries(t *testing.T) {
	date := MustDate(2026, time.January, 1)
	fragments := make([]Range, maxOutputRanges)
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
	daily, err := fragmented.EffectiveRanges(date)
	if err != nil || len(daily) != maxOutputRanges {
		t.Fatalf("maximum effective ranges = %d, error=%v", len(daily), err)
	}
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	instantRanges, err := fragmented.EffectiveInstantRanges(start, start.Add(time.Second))
	if err != nil || len(instantRanges) != maxOutputRanges {
		t.Fatalf("maximum instant ranges = %d, error=%v", len(instantRanges), err)
	}

	empty := internalSchedule(t, Config{})
	if ranges, err := empty.EffectiveInstantRanges(start, start.Add(maxSearchHorizon)); err != nil || len(ranges) != 0 {
		t.Fatalf("maximum instant horizon = %#v, error=%v", ranges, err)
	}
	for _, horizon := range []time.Duration{time.Nanosecond, maxSearchHorizon} {
		if _, err := empty.NextTransition(start, horizon); !IsCode(err, CodeSearchExhausted) {
			t.Errorf("valid next horizon %s error = %v", horizon, err)
		}
		if _, err := empty.PreviousTransition(start, horizon); !IsCode(err, CodeSearchExhausted) {
			t.Errorf("valid previous horizon %s error = %v", horizon, err)
		}
		if _, err := empty.NextOpening(start, horizon); !IsCode(err, CodeSearchExhausted) {
			t.Errorf("valid typed horizon %s error = %v", horizon, err)
		}
	}

	rule := internalRule(t, internalRange(t, 9, 0, 10, 0))
	timed := internalSchedule(t, Config{Weekly: map[time.Weekday]DayRule{time.Thursday: rule}})
	before := time.Date(2026, time.January, 1, 8, 0, 0, 0, time.UTC)
	atDeadline, err := timed.NextTransition(before, time.Hour)
	if err != nil || !atDeadline.Instant.Equal(before.Add(time.Hour)) {
		t.Fatalf("transition at deadline = %#v, error=%v", atDeadline, err)
	}
	if _, err := timed.NextTransition(before.Add(time.Hour), 30*time.Minute); !IsCode(err, CodeSearchExhausted) {
		t.Fatalf("transition at cursor error = %v", err)
	}
	deadline := before.Add(time.Hour)
	if advanced := advanceTransitionCursor(before, Transition{Instant: before}, deadline); !advanced.Equal(deadline) {
		t.Fatalf("non-advancing typed transition cursor = %v", advanced)
	}
	advanced := advanceTransitionCursor(before, Transition{Instant: before.Add(time.Nanosecond)}, deadline)
	if !advanced.Equal(before.Add(time.Nanosecond)) {
		t.Fatalf("advanced typed transition cursor = %v", advanced)
	}
	previousDayRule := internalRule(t, internalRange(t, 20, 0, 21, 0))
	previousDay := internalSchedule(t, Config{Weekly: map[time.Weekday]DayRule{time.Wednesday: previousDayRule}})
	previous, err := previousDay.PreviousTransition(
		time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC), 24*time.Hour,
	)
	if err != nil || previous.Kind != TransitionClose || previous.Instant.Day() != 31 || previous.Instant.Hour() != 21 {
		t.Fatalf("previous-day transition = %#v, error=%v", previous, err)
	}
	earliestDate := MustDate(1, time.January, 1)
	earliestRule := internalRule(t, internalRange(t, 0, 30, 1, 0))
	earliestSchedule := internalSchedule(t, Config{Weekly: map[time.Weekday]DayRule{
		earliestDate.Weekday(): earliestRule,
	}})
	earliestPrevious, err := earliestSchedule.PreviousTransition(
		time.Date(1, time.January, 1, 1, 30, 0, 0, time.UTC), 2*time.Hour,
	)
	if err != nil || earliestPrevious.Kind != TransitionClose ||
		!earliestPrevious.Instant.Equal(time.Date(1, time.January, 1, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("year-one previous transition = %#v, error=%v", earliestPrevious, err)
	}

	effectiveEnd := date
	outside := internalSchedule(t, Config{
		Weekly:       map[time.Weekday]DayRule{time.Thursday: OpenAllDay(), time.Friday: OpenAllDay()},
		EffectiveEnd: &effectiveEnd, OutsideEffective: OutsideError,
	})
	dayEnd := start.Add(24 * time.Hour)
	if ranges, err := outside.EffectiveInstantRanges(start, dayEnd); err != nil || len(ranges) != 1 ||
		!ranges[0].Start.Equal(start) || !ranges[0].End.Equal(dayEnd) {
		t.Fatalf("exclusive midnight range = %#v, error=%v", ranges, err)
	}
}

func TestWireBoundaryMatrix(t *testing.T) {
	base := internalSchedule(t, Config{})
	baseline := Schedule{data: &scheduleData{timezone: "UTC", location: time.UTC}}
	baselineJSON, err := baseline.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	exactMaximum := Schedule{data: &scheduleData{
		timezone: "UTC", location: time.UTC,
		metadata: Metadata{Label: strings.Repeat("x", MaxJSONBytes-len(baselineJSON))},
	}}
	encoded, err := exactMaximum.CanonicalJSON()
	if err != nil || len(encoded) != MaxJSONBytes {
		t.Fatalf("maximum canonical JSON length = %d, error=%v", len(encoded), err)
	}
	exactMaximum.data.metadata.Label += "x"
	if _, err := exactMaximum.CanonicalJSON(); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("oversized canonical JSON error = %v", err)
	}
	if exactMaximum.Equal(base) || base.Equal(exactMaximum) {
		t.Fatal("canonical equality accepted an unencodable operand")
	}

	zeroJSON := mustCanonicalJSON(t, Schedule{})
	padded := append(append([]byte(nil), zeroJSON...), []byte(strings.Repeat(" ", MaxJSONBytes-len(zeroJSON)))...)
	if _, err := ParseJSON(padded); err != nil {
		t.Fatalf("maximum parse input error = %v", err)
	}
	if _, err := ParseJSON(append(padded, ' ')); !IsCode(err, CodeInvalidEncoding) {
		t.Fatalf("oversized parse input error = %v", err)
	}
	if _, err := ParseJSON([]byte{0xff}); !IsCode(err, CodeInvalidEncoding) {
		t.Fatalf("invalid UTF-8 parse error = %v", err)
	}
	if _, err := ParseJSON([]byte("{")); !IsCode(err, CodeInvalidEncoding) {
		t.Fatalf("malformed parse error = %v", err)
	}

	wire := base.toWire()
	wire.Version = wireVersion
	if _, err := scheduleFromWire(wire, MaxCompositionDepth); err != nil {
		t.Fatalf("wire at maximum depth error = %v", err)
	}
	if _, err := scheduleFromWire(wire, MaxCompositionDepth+1); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("wire beyond maximum depth error = %v", err)
	}
	start := MustDate(2026, time.January, 1)
	end := MustDate(2026, time.January, 2)
	startOnly := internalSchedule(t, Config{EffectiveStart: &start}).toWire()
	endOnly := internalSchedule(t, Config{EffectiveEnd: &end}).toWire()
	if startOnly.Effective == nil || startOnly.Effective.Start == "" || startOnly.Effective.End != "" ||
		endOnly.Effective == nil || endOnly.Effective.Start != "" || endOnly.Effective.End == "" {
		t.Fatalf("one-sided effective wires = %#v and %#v", startOnly.Effective, endOnly.Effective)
	}

	composition := wireSchedule{
		Version: wireVersion, Timezone: "UTC", OutsideEffective: "closed",
		Composition: &wireComposition{Operation: "union", Left: base.toWire(), Right: base.toWire()},
	}
	withWeekly := composition
	withWeekly.Weekly = []wireWeekday{{Weekday: "monday", Rule: wireRule{State: "closed", Ranges: []wireRange{}}}}
	if _, err := scheduleFromWire(withWeekly, 1); !IsCode(err, CodeInvalidEncoding) {
		t.Fatalf("composition with weekly rules error = %v", err)
	}
	withException := composition
	withException.Exceptions = []wireException{{}}
	if _, err := scheduleFromWire(withException, 1); !IsCode(err, CodeInvalidEncoding) {
		t.Fatalf("composition with exceptions error = %v", err)
	}
	leftMismatch := composition
	leftMismatch.Composition.Left.Timezone = "Europe/Helsinki"
	if _, err := scheduleFromWire(leftMismatch, 1); !IsCode(err, CodeTimezoneMismatch) ||
		err.Error() != "openinghours: parse json: timezone_mismatch" {
		t.Fatalf("left composition timezone mismatch error = %v", err)
	}
	rightMismatch := composition
	rightMismatch.Composition.Right.Timezone = "Europe/Helsinki"
	if _, err := scheduleFromWire(rightMismatch, 1); !IsCode(err, CodeTimezoneMismatch) ||
		err.Error() != "openinghours: parse json: timezone_mismatch" {
		t.Fatalf("right composition timezone mismatch error = %v", err)
	}

	maximumSet := strings.Repeat("s", maxProvenanceBytes)
	date := MustDate(2026, time.January, 1)
	closure, _ := NewException(ExceptionConfig{
		Date: date, Operation: ExceptionClose, Source: "source", Revision: "revision",
	})
	closure.set = maximumSet
	schedule := internalSchedule(t, Config{Exceptions: []Exception{closure}})
	roundTrip, err := ParseJSON(mustCanonicalJSON(t, schedule))
	if err != nil || !schedule.Equal(roundTrip) {
		t.Fatalf("maximum wire exception set round trip error=%v equal=%t", err, schedule.Equal(roundTrip))
	}

	parsed, err := parseLocalTime("23:58:57.123456789")
	if err != nil || parsed.Hour() != 23 || parsed.Minute() != 58 || parsed.Second() != 57 || parsed.Nanosecond() != 123456789 {
		t.Fatalf("full local-time parse = %#v, error=%v", parsed, err)
	}
	if _, err := parseLocalTime("23:58:57,123456789"); !IsCode(err, CodeInvalidTime) {
		t.Fatalf("noncanonical fractional separator error = %v", err)
	}
	if !base.Equal(base) || base.Equal(schedule) {
		t.Fatal("wire equality did not distinguish equal and different schedules")
	}

	exactDepth := []byte("0")
	for range maxJSONDepth {
		exactDepth = append([]byte("["), append(exactDepth, ']')...)
	}
	if err := validateJSON(exactDepth); err != nil {
		t.Fatalf("JSON at maximum depth error = %v", err)
	}
	beyondDepth := append([]byte("["), append(exactDepth, ']')...)
	if err := validateJSON(beyondDepth); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("JSON beyond maximum depth error = %v", err)
	}
	exactObjectDepth := []byte("0")
	for range maxJSONDepth {
		exactObjectDepth = append([]byte("{\"x\":"), append(exactObjectDepth, '}')...)
	}
	if err := validateJSON(exactObjectDepth); err != nil {
		t.Fatalf("object JSON at maximum depth error = %v", err)
	}
	beyondObjectDepth := append([]byte("{\"x\":"), append(exactObjectDepth, '}')...)
	if err := validateJSON(beyondObjectDepth); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("object JSON beyond maximum depth error = %v", err)
	}

	validNested := nestedCompositionWire(base.toWire(), MaxCompositionDepth)
	if _, err := scheduleFromWire(validNested, 1); err != nil {
		t.Fatalf("maximum nested composition error = %v", err)
	}
	invalidNested := nestedCompositionWire(base.toWire(), MaxCompositionDepth+1)
	if _, err := scheduleFromWire(invalidNested, 1); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("over-depth nested composition error = %v", err)
	}
}

func nestedCompositionWire(base wireSchedule, depth int) wireSchedule {
	result := base
	for range depth - 1 {
		result = wireSchedule{
			Version: wireVersion, Timezone: "UTC", OutsideEffective: "closed",
			Composition: &wireComposition{Operation: "union", Left: result, Right: base},
		}
	}

	return result
}

func mustCanonicalJSON(t *testing.T, schedule Schedule) []byte {
	t.Helper()
	encoded, err := schedule.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
