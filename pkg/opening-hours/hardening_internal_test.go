package openinghours

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func internalTime(t *testing.T, hour, minute int) LocalTime {
	t.Helper()
	value, err := NewLocalTime(hour, minute, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func internalRange(t *testing.T, startHour, startMinute, endHour, endMinute int) Range {
	t.Helper()
	value, err := NewRange(
		internalTime(t, startHour, startMinute), internalTime(t, endHour, endMinute),
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func internalRule(t *testing.T, ranges ...Range) DayRule {
	t.Helper()
	value, err := OpenRanges(ranges, RejectOverlap)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func internalSchedule(t *testing.T, config Config) Schedule {
	t.Helper()
	if config.Timezone == "" {
		config.Timezone = "UTC"
	}
	value, err := NewSchedule(config)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestTypedErrorAndValueAccessors(t *testing.T) {
	err := newError("test", CodeInvalidState)
	if err.Error() != "openinghours: test: invalid_state" || !IsCode(err, CodeInvalidState) ||
		IsCode(errors.New("other"), CodeInvalidState) {
		t.Fatalf("typed error contract failed: %v", err)
	}

	date := MustDate(2026, time.February, 3)
	if date.Year() != 2026 || date.Month() != time.February || date.Day() != 3 {
		t.Fatalf("date accessors = %d %s %d", date.Year(), date.Month(), date.Day())
	}
	deferred := func() (panicked bool) {
		defer func() { panicked = recover() != nil }()
		_ = MustDate(2026, time.February, 30)
		return false
	}()
	if !deferred {
		t.Fatal("MustDate did not panic for an impossible date")
	}

	for _, input := range []struct{ hour, minute, second, nano int }{
		{-1, 0, 0, 0}, {24, 0, 0, 0}, {0, -1, 0, 0}, {0, 60, 0, 0},
		{0, 0, -1, 0}, {0, 0, 60, 0}, {0, 0, 0, -1}, {0, 0, 0, int(time.Second)},
	} {
		if _, invalid := NewLocalTime(input.hour, input.minute, input.second, input.nano); !IsCode(invalid, CodeInvalidTime) {
			t.Errorf("NewLocalTime(%+v) error = %v", input, invalid)
		}
	}
	for _, input := range []struct {
		year  int
		month time.Month
		day   int
	}{{0, time.January, 1}, {10000, time.January, 1}, {2026, 0, 1}, {2026, 13, 1}, {2026, time.January, 0}, {2026, time.April, 31}} {
		if _, invalid := NewDate(input.year, input.month, input.day); !IsCode(invalid, CodeInvalidDate) {
			t.Errorf("NewDate(%+v) error = %v", input, invalid)
		}
	}
	if _, invalid := addDate(Date{}, 1); !IsCode(invalid, CodeInvalidDate) {
		t.Fatalf("zero addDays error = %v", invalid)
	}
	if _, invalid := addDate(MustDate(9999, time.December, 31), 1); !IsCode(invalid, CodeInvalidDate) {
		t.Fatalf("overflow addDays error = %v", invalid)
	}
}

func TestRangeValidationAndOrderingMatrix(t *testing.T) {
	invalidTimes := []LocalTime{{nanosecond: -1}, {nanosecond: nanosecondsPerDay}}
	for _, invalidTime := range invalidTimes {
		if _, err := NewRange(invalidTime, LocalTime{}); !IsCode(err, CodeInvalidRange) {
			t.Errorf("NewRange(%v) error = %v", invalidTime, err)
		}
	}
	if _, err := OpenRanges(nil, RejectOverlap); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("empty OpenRanges error = %v", err)
	}
	tooMany := make([]Range, maxRangesPerDay+1)
	if _, err := OpenRanges(tooMany, RejectOverlap); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("large OpenRanges error = %v", err)
	}
	if _, err := OpenRanges([]Range{internalRange(t, 9, 0, 10, 0)}, OverlapPolicy(99)); !IsCode(err, CodeInvalidState) {
		t.Fatalf("policy error = %v", err)
	}
	if _, err := OpenRanges([]Range{{}}, RejectOverlap); !IsCode(err, CodeInvalidRange) {
		t.Fatalf("invalid stored range error = %v", err)
	}

	a := internalRange(t, 9, 0, 10, 0)
	b := internalRange(t, 9, 0, 11, 0)
	c := internalRange(t, 10, 0, 11, 0)
	if compareRange(a, c) >= 0 || compareRange(c, a) <= 0 || compareRange(a, b) >= 0 ||
		compareRange(b, a) <= 0 || compareRange(a, a) != 0 {
		t.Fatal("range comparison is not a total canonical order")
	}
	merged, err := OpenRanges([]Range{a, b}, MergeOverlap)
	if err != nil || len(merged.ranges) != 1 || merged.ranges[0] != b {
		t.Fatalf("contained overlap merge = %#v error=%v", merged, err)
	}
	separate, err := OpenRanges([]Range{a, c}, MergeOverlap)
	if err != nil || len(separate.ranges) != 2 {
		t.Fatalf("MergeOverlap adjacency = %#v error=%v", separate, err)
	}
}

func TestScheduleConstructorRejectsEveryInvalidBoundary(t *testing.T) {
	validDate := MustDate(2026, time.January, 1)
	invalidDate := Date{}
	tests := []struct {
		name   string
		config Config
		code   Code
	}{
		{"empty zone", Config{}, CodeInvalidTimezone},
		{"unknown zone", Config{Timezone: "Not/AZone"}, CodeInvalidTimezone},
		{"metadata", Config{Timezone: "UTC", Metadata: Metadata{Label: strings.Repeat("x", maxMetadataBytes+1)}}, CodeLimitExceeded},
		{"outside policy", Config{Timezone: "UTC", OutsideEffective: OutsideEffectivePolicy(9)}, CodeInvalidState},
		{"start", Config{Timezone: "UTC", EffectiveStart: &invalidDate}, CodeInvalidDate},
		{"end", Config{Timezone: "UTC", EffectiveEnd: &invalidDate}, CodeInvalidDate},
		{"reversed", Config{Timezone: "UTC", EffectiveStart: &validDate, EffectiveEnd: datePointer(MustDate(2025, time.December, 31))}, CodeInvalidInterval},
		{"weekday", Config{Timezone: "UTC", Weekly: map[time.Weekday]DayRule{time.Weekday(9): Closed()}}, CodeInvalidWeekday},
		{"state", Config{Timezone: "UTC", Weekly: map[time.Weekday]DayRule{time.Monday: {state: DayState(9)}}}, CodeInvalidState},
		{"empty set", Config{Timezone: "UTC", ExceptionSets: []ExceptionSet{{}}}, CodeInvalidState},
		{"conflict policy", Config{Timezone: "UTC", ConflictPolicy: ConflictPolicy(9)}, CodeInvalidState},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewSchedule(test.config)
			if !IsCode(err, test.code) {
				t.Fatalf("NewSchedule() error = %v, want %s", err, test.code)
			}
		})
	}

	oversizedSet := ExceptionSet{name: "set", exceptions: make([]Exception, maxExceptions+1)}
	_, err := NewSchedule(Config{Timezone: "UTC", ExceptionSets: []ExceptionSet{oversizedSet}})
	if !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("oversized set error = %v", err)
	}
}

func datePointer(date Date) *Date { return &date }

func TestExceptionValidationOrderingAndAccessors(t *testing.T) {
	date := MustDate(2026, time.January, 2)
	rule := internalRule(t, internalRange(t, 9, 0, 12, 0))
	valid, err := NewException(ExceptionConfig{
		Date: date, Operation: ExceptionAdd, Rule: rule,
		Priority: 7, Source: "source", Revision: "revision",
	})
	if err != nil {
		t.Fatal(err)
	}
	if valid.Date() != date || valid.Operation() != ExceptionAdd || valid.Priority() != 7 ||
		valid.Source() != "source" || valid.Revision() != "revision" || valid.Rule().state != DayOpenRanges {
		t.Fatalf("exception accessors = %#v", valid)
	}
	detached := valid.Rule()
	detached.ranges[0] = internalRange(t, 13, 0, 14, 0)
	if valid.rule.ranges[0] == detached.ranges[0] {
		t.Fatal("exception Rule accessor aliased internal ranges")
	}

	invalidCases := []struct {
		config ExceptionConfig
		code   Code
	}{
		{ExceptionConfig{}, CodeInvalidDate},
		{ExceptionConfig{Date: date, Operation: ExceptionOperation(9), Source: "s", Revision: "r"}, CodeInvalidState},
		{ExceptionConfig{Date: date, Operation: ExceptionClose, Priority: 1_000_001, Source: "s", Revision: "r"}, CodeInvalidState},
		{ExceptionConfig{Date: date, Operation: ExceptionClose, Source: strings.Repeat("s", maxProvenanceBytes+1), Revision: "r"}, CodeLimitExceeded},
		{ExceptionConfig{Date: date, Operation: ExceptionClose, Source: "s"}, CodeInvalidState},
		{ExceptionConfig{Date: date, Operation: ExceptionClose, Rule: rule, Source: "s", Revision: "r"}, CodeInvalidState},
		{ExceptionConfig{Date: date, Operation: ExceptionAdd, Rule: Closed(), Source: "s", Revision: "r"}, CodeInvalidState},
	}
	for _, test := range invalidCases {
		if _, invalid := NewException(test.config); !IsCode(invalid, test.code) {
			t.Errorf("NewException(%#v) error = %v, want %s", test.config, invalid, test.code)
		}
	}

	operations := []ExceptionOperation{ExceptionReplace, ExceptionAdd, ExceptionSubtract, ExceptionClose}
	for index, operation := range operations {
		config := ExceptionConfig{Date: date, Operation: operation, Rule: rule, Priority: index, Source: "s", Revision: string(rune('a' + index))}
		if operation == ExceptionClose {
			config.Rule = DayRule{}
		}
		if _, err := NewException(config); err != nil {
			t.Errorf("operation %v rejected: %v", operation, err)
		}
	}
}

func TestExceptionSetAndExpansionInvalidMatrix(t *testing.T) {
	date := MustDate(2026, time.January, 1)
	closure, _ := NewException(ExceptionConfig{Date: date, Operation: ExceptionClose, Source: "s", Revision: "r"})
	for _, name := range []string{"", strings.Repeat("x", maxProvenanceBytes+1), string([]byte{0xff})} {
		if _, err := NewExceptionSet(name, []Exception{closure}); !IsCode(err, CodeInvalidState) {
			t.Errorf("set name %q error = %v", name, err)
		}
	}
	if _, err := NewExceptionSet("set", nil); !IsCode(err, CodeInvalidState) {
		t.Fatalf("empty set error = %v", err)
	}
	if _, err := NewExceptionSet("set", make([]Exception, maxExceptions+1)); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("large set error = %v", err)
	}
	if _, err := NewExceptionSet("set", []Exception{{}}); !IsCode(err, CodeInvalidState) {
		t.Fatalf("invalid item error = %v", err)
	}

	set, err := NewExceptionSet("set", []Exception{closure})
	if err != nil || set.Name() != "set" {
		t.Fatalf("valid set = %#v error=%v", set, err)
	}
	items := set.Exceptions()
	if len(items) != 1 {
		t.Fatalf("set exceptions length = %d", len(items))
	}
	items[0].set = "changed"
	if set.exceptions[0].set != "set" {
		t.Fatal("set Exceptions accessor aliased internal values")
	}

	for _, config := range []ExceptionRangeConfig{
		{},
		{Name: "x", Start: date, End: MustDate(2025, time.December, 31), MaximumDates: 1},
		{Name: "x", Start: date, End: date, MaximumDates: 0},
		{Name: "x", Start: date, End: date, MaximumDates: maxExceptions + 1},
		{Name: "x", Start: date, End: date, MaximumDates: 1, Operation: ExceptionAdd, Source: "s", Revision: "r"},
	} {
		_, expansionErr := ExpandExceptionRange(config)
		if expansionErr == nil {
			t.Errorf("ExpandExceptionRange(%#v) succeeded unexpectedly", config)
		}
	}
}

func TestExceptionCanonicalComparisonAndNormalization(t *testing.T) {
	date1 := MustDate(2026, time.January, 1)
	date2 := MustDate(2026, time.January, 2)
	makeException := func(date Date, priority int, source, revision string, operation ExceptionOperation) Exception {
		return Exception{date: date, priority: priority, source: source, revision: revision, operation: operation}
	}
	ordered := []Exception{
		makeException(date1, 1, "a", "a", ExceptionAdd),
		makeException(date1, 2, "a", "a", ExceptionAdd),
		makeException(date1, 2, "b", "a", ExceptionAdd),
		makeException(date1, 2, "b", "b", ExceptionAdd),
		makeException(date1, 2, "b", "b", ExceptionSubtract),
		makeException(date2, 1, "a", "a", ExceptionAdd),
	}
	for index := range len(ordered) - 1 {
		if compareException(ordered[index], ordered[index+1]) >= 0 || compareException(ordered[index+1], ordered[index]) <= 0 {
			t.Fatalf("exceptions %d and %d are not ordered", index, index+1)
		}
	}
	if compareException(ordered[0], ordered[0]) != 0 {
		t.Fatal("exception comparison is not reflexive")
	}
	if normalized, err := normalizeExceptions([]Exception{ordered[0], ordered[len(ordered)-1]}, ResolveCanonical); err != nil || len(normalized) != 2 {
		t.Fatalf("different-date normalization = %#v, %v", normalized, err)
	}
	if _, err := normalizeExceptions(make([]Exception, maxExceptions+1), ResolveCanonical); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("exception limit error = %v", err)
	}
	duplicate := []Exception{
		makeException(date1, 1, "s", "r", ExceptionAdd),
		makeException(date1, 2, "s", "r", ExceptionSubtract),
	}
	if _, err := normalizeExceptions(duplicate, ResolveCanonical); !IsCode(err, CodeDuplicateRevision) {
		t.Fatalf("duplicate revision error = %v", err)
	}
	if got := (Schedule{}).exceptionsFor(date1); got != nil {
		t.Fatalf("zero exceptionsFor = %#v", got)
	}
	schedule := Schedule{data: &scheduleData{exceptions: ordered}}
	if got := schedule.exceptionsFor(MustDate(2027, time.January, 1)); got != nil {
		t.Fatalf("missing exceptionsFor = %#v", got)
	}
	if got := schedule.exceptionsFor(date1); len(got) != 5 {
		t.Fatalf("exceptionsFor date1 length = %d", len(got))
	}
}

func TestMetadataEffectiveAndOversizedCanonicalBehavior(t *testing.T) {
	invalidUTF8 := string([]byte{0xff})
	if _, err := validateMetadata(Metadata{Source: invalidUTF8}); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("invalid metadata error = %v", err)
	}
	var zero Schedule
	if zero.Metadata() != (Metadata{}) || zero.Revision() != "" {
		t.Fatal("zero metadata was not empty")
	}
	if inside, err := zero.withinEffective(MustDate(2026, time.January, 1)); err != nil || inside {
		t.Fatalf("zero withinEffective = %t, %v", inside, err)
	}

	oversized := Schedule{data: &scheduleData{
		timezone: "UTC", metadata: Metadata{Label: strings.Repeat("x", MaxJSONBytes)}, depth: 1,
	}}
	if _, err := oversized.CanonicalJSON(); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("oversized canonical error = %v", err)
	}
	if oversized.Hash() != ([32]byte{}) || oversized.String() != "<invalid opening-hours schedule>" || oversized.Equal(Schedule{}) {
		t.Fatal("oversized hash/string/equality did not fail closed")
	}
	if _, err := oversized.Compare(Schedule{}); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("oversized comparison error = %v", err)
	}
	if _, err := (Schedule{}).Compare(oversized); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("right oversized comparison error = %v", err)
	}
	oversizedSummary := Schedule{data: &scheduleData{
		timezone: strings.Repeat("x", MaxHumanSummaryBytes), depth: 1,
	}}
	if _, err := oversizedSummary.HumanSummary(); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("oversized human summary error = %v", err)
	}
}

func TestWireNamesParsingAndTextInterfaces(t *testing.T) {
	states := map[DayState]string{
		DayInherited: "inherited", DayOpenRanges: "ranges", DayOpenAllDay: "all_day", DayClosed: "closed",
	}
	for state, name := range states {
		if dayStateName(state) != name {
			t.Errorf("dayStateName(%v) = %q", state, dayStateName(state))
		}
	}
	if dayStateName(DayState(99)) != "inherited" || ruleSegments(DayRule{state: DayState(99)}) != nil {
		t.Fatal("unknown day states did not fail closed")
	}
	operations := map[ExceptionOperation]string{
		ExceptionReplace: "replace", ExceptionAdd: "add", ExceptionSubtract: "subtract", ExceptionClose: "close",
	}
	for operation, name := range operations {
		if exceptionName(operation) != name {
			t.Errorf("exceptionName(%v) = %q", operation, exceptionName(operation))
		}
		parsed, ok := parseExceptionOperation(name)
		if !ok || parsed != operation {
			t.Errorf("parseExceptionOperation(%q) = %v,%t", name, parsed, ok)
		}
	}
	if exceptionName(ExceptionOperation(99)) != "replace" || compositionName(compositionOperation(99)) != "union" {
		t.Fatal("unknown wire enum names did not use stable defaults")
	}
	if _, ok := parseExceptionOperation("invalid"); ok {
		t.Fatal("invalid exception operation accepted")
	}
	compositionNames := map[compositionOperation]string{
		compositionUnion: "union", compositionIntersection: "intersection",
		compositionSubtract: "subtract", compositionOverlay: "overlay",
	}
	for operation, name := range compositionNames {
		if compositionName(operation) != name {
			t.Errorf("compositionName(%v) = %q", operation, compositionName(operation))
		}
	}
	for weekday, name := range weekdayNames {
		parsed, ok := parseWeekday(name)
		if !ok || parsed != time.Weekday(weekday) {
			t.Errorf("parseWeekday(%q) = %v,%t", name, parsed, ok)
		}
	}
	if _, ok := parseWeekday("nonday"); ok {
		t.Fatal("invalid weekday accepted")
	}

	precise, _ := NewLocalTime(1, 2, 3, 120_000_000)
	if formatLocalTime(precise) != "01:02:03.12" {
		t.Fatalf("precise format = %q", formatLocalTime(precise))
	}
	if _, err := parseLocalTime("1:02"); !IsCode(err, CodeInvalidTime) {
		t.Fatalf("invalid local wire error = %v", err)
	}
	if _, err := parseDate("2026-1-01"); !IsCode(err, CodeInvalidDate) {
		t.Fatalf("invalid date wire error = %v", err)
	}

	schedule := internalSchedule(t, Config{Metadata: Metadata{Revision: "1"}})
	text, err := schedule.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Schedule
	if err := decoded.UnmarshalText(text); err != nil || !schedule.Equal(decoded) {
		t.Fatalf("text round trip error=%v equal=%t", err, schedule.Equal(decoded))
	}
	var jsonDecoded Schedule
	if err := json.Unmarshal(text, &jsonDecoded); err != nil || !schedule.Equal(jsonDecoded) {
		t.Fatalf("JSON unmarshal error=%v", err)
	}
	if jsonDecoded.String() != string(text) {
		t.Fatalf("String() = %q, want %q", jsonDecoded.String(), text)
	}
	var nilSchedule *Schedule
	if err := nilSchedule.UnmarshalJSON(text); !IsCode(err, CodeInvalidState) {
		t.Fatalf("nil UnmarshalJSON error = %v", err)
	}
	if err := decoded.UnmarshalJSON([]byte(`{}`)); err == nil {
		t.Fatal("invalid UnmarshalJSON succeeded")
	}
}

func TestStrictWireInternalFailureMatrix(t *testing.T) {
	emptyRule := wireRule{State: "closed", Ranges: []wireRange{}}
	base := wireSchedule{Version: wireVersion, Timezone: "UTC", Weekly: []wireWeekday{}, Exceptions: []wireException{}, OutsideEffective: "closed"}
	tests := []struct {
		name string
		wire wireSchedule
		code Code
	}{
		{"version", wireSchedule{Version: 9}, CodeUnsupportedVersion},
		{"depth", base, CodeLimitExceeded},
		{"bad weekday", withWeekly(base, wireWeekday{Weekday: "bad", Rule: emptyRule}), CodeInvalidEncoding},
		{"duplicate weekday", withWeekly(base, wireWeekday{Weekday: "monday", Rule: emptyRule}, wireWeekday{Weekday: "monday", Rule: emptyRule}), CodeInvalidEncoding},
		{"outside", withOutside(base, "maybe"), CodeInvalidEncoding},
		{"effective start", withEffective(base, &wireEffective{Start: "bad"}), CodeInvalidDate},
		{"effective end", withEffective(base, &wireEffective{End: "bad"}), CodeInvalidDate},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			depth := 1
			if test.name == "depth" {
				depth = MaxCompositionDepth + 1
			}
			_, err := scheduleFromWire(test.wire, depth)
			if !IsCode(err, test.code) {
				t.Fatalf("scheduleFromWire error = %v, want %s", err, test.code)
			}
		})
	}

	for _, rule := range []wireRule{
		{State: "closed"},
		{State: "inherited", Ranges: []wireRange{{Start: "01:00:00", End: "02:00:00"}}},
		{State: "closed", Ranges: []wireRange{{Start: "01:00:00", End: "02:00:00"}}},
		{State: "all_day", Ranges: []wireRange{{Start: "01:00:00", End: "02:00:00"}}},
		{State: "ranges", Ranges: []wireRange{{Start: "bad", End: "02:00:00"}}},
		{State: "ranges", Ranges: []wireRange{{Start: "01:00:00", End: "bad"}}},
		{State: "ranges", Ranges: []wireRange{{Start: "01:00:00", End: "01:00:00"}}},
		{State: "invalid", Ranges: []wireRange{}},
	} {
		if _, err := ruleFromWire(rule); err == nil {
			t.Errorf("ruleFromWire(%#v) succeeded", rule)
		}
	}
	for _, rule := range []wireRule{
		{State: "inherited", Ranges: []wireRange{}},
		{State: "closed", Ranges: []wireRange{}},
		{State: "all_day", Ranges: []wireRange{}},
		{State: "ranges", Ranges: []wireRange{{Start: "01:00:00", End: "02:00:00"}}},
	} {
		if _, err := ruleFromWire(rule); err != nil {
			t.Errorf("ruleFromWire(%#v) error = %v", rule, err)
		}
	}

	malformed := []string{"", `]`, `{"a"`, `{"a":]}`, strings.Repeat("[", maxJSONDepth+2) + strings.Repeat("]", maxJSONDepth+2)}
	for _, input := range malformed {
		if validateJSON([]byte(input)) == nil {
			t.Errorf("validateJSON(%q) succeeded", input)
		}
	}
	decoder := json.NewDecoder(bytes.NewBufferString(`true false`))
	if err := validateJSONValue(decoder, 0); err != nil {
		t.Fatal(err)
	}
	if err := requireEOF(decoder); err == nil {
		t.Fatal("requireEOF accepted trailing token")
	}
	brokenEOF := json.NewDecoder(bytes.NewBufferString(`true "`))
	_, _ = brokenEOF.Token()
	if err := requireEOF(brokenEOF); err == nil {
		t.Fatal("requireEOF accepted malformed trailing input")
	}
	closingDelimiter := json.NewDecoder(bytes.NewBufferString(`}`))
	if err := validateJSONValue(closingDelimiter, 0); err == nil {
		t.Fatal("validateJSONValue accepted a closing delimiter as a value")
	}
	arrayEnd := json.NewDecoder(bytes.NewBufferString(`[]`))
	_, _ = arrayEnd.Token()
	if err := validateJSONValue(arrayEnd, 0); err == nil {
		t.Fatal("validateJSONValue accepted an array-end delimiter as a value")
	}
	if validateJSON([]byte(`{"a":1,`)) == nil {
		t.Fatal("validateJSON accepted a truncated object key")
	}
}

func withWeekly(wire wireSchedule, weekdays ...wireWeekday) wireSchedule {
	wire.Weekly = weekdays
	return wire
}

func withOutside(wire wireSchedule, outside string) wireSchedule {
	wire.OutsideEffective = outside
	return wire
}

func withEffective(wire wireSchedule, effective *wireEffective) wireSchedule {
	wire.Effective = effective
	return wire
}

func TestWireCompositionAndExceptionMatrix(t *testing.T) {
	start := MustDate(2026, time.January, 1)
	end := MustDate(2026, time.December, 31)
	rule := internalRule(t, internalRange(t, 9, 0, 17, 0))
	base := internalSchedule(t, Config{
		Weekly:           map[time.Weekday]DayRule{time.Monday: rule},
		Metadata:         Metadata{Label: "label", Source: "source", Revision: "revision"},
		EffectiveStart:   &start,
		EffectiveEnd:     &end,
		OutsideEffective: OutsideError,
	})
	wireBase := base.toWire()
	if wireBase.Effective == nil || wireBase.Effective.Start == "" || wireBase.Effective.End == "" ||
		wireBase.OutsideEffective != "error" {
		t.Fatalf("effective wire = %#v", wireBase)
	}

	operations := []struct {
		name string
		op   compositionOperation
	}{
		{"union", compositionUnion}, {"intersection", compositionIntersection},
		{"subtract", compositionSubtract}, {"overlay", compositionOverlay},
	}
	for _, operation := range operations {
		compositionWire := wireSchedule{
			Version: wireVersion, Timezone: "UTC", Weekly: []wireWeekday{},
			Exceptions: []wireException{}, OutsideEffective: "closed",
			Composition: &wireComposition{Operation: operation.name, Left: wireBase, Right: wireBase},
		}
		decoded, err := scheduleFromWire(compositionWire, 1)
		if err != nil || decoded.data.composition == nil || decoded.data.composition.operation != operation.op {
			t.Fatalf("composition %s = %#v, %v", operation.name, decoded, err)
		}
		encoded := decoded.toWire()
		if encoded.Composition == nil || encoded.Composition.Operation != operation.name {
			t.Fatalf("encoded composition %s = %#v", operation.name, encoded)
		}
		clearWireMetadata(&encoded)
		if encoded.Composition.Left.Metadata != (wireMetadata{}) || encoded.Composition.Right.Metadata != (wireMetadata{}) {
			t.Fatalf("composition metadata was not recursively cleared: %#v", encoded)
		}
	}

	newCompositionWire := func() wireSchedule {
		return wireSchedule{
			Version: wireVersion, Timezone: "UTC", Weekly: []wireWeekday{}, Exceptions: []wireException{},
			Composition: &wireComposition{Operation: "invalid", Left: wireBase, Right: wireBase},
		}
	}
	invalidCases := []wireSchedule{
		func() wireSchedule { value := newCompositionWire(); value.Weekly = wireBase.Weekly; return value }(),
		func() wireSchedule { value := newCompositionWire(); value.Composition.Left.Version = 99; return value }(),
		func() wireSchedule { value := newCompositionWire(); value.Composition.Right.Version = 99; return value }(),
		func() wireSchedule { value := newCompositionWire(); value.Timezone = "Europe/Helsinki"; return value }(),
		newCompositionWire(),
	}
	for index, value := range invalidCases {
		if _, err := scheduleFromWire(value, 1); err == nil {
			t.Errorf("invalid composition %d succeeded", index)
		}
	}

	exceptionDate := formatDate(start)
	validException := wireException{
		Date: exceptionDate, Operation: "close", Rule: wireRule{State: "inherited", Ranges: []wireRange{}},
		Source: "source", Revision: "revision", Set: "set",
	}
	exceptionWire := wireBase
	exceptionWire.Exceptions = []wireException{validException}
	decoded, err := scheduleFromWire(exceptionWire, 1)
	if err != nil || len(decoded.data.exceptions) != 1 || decoded.data.exceptions[0].set != "set" {
		t.Fatalf("exception wire = %#v, %v", decoded, err)
	}
	badExceptions := []wireException{
		func() wireException { value := validException; value.Date = "bad"; return value }(),
		func() wireException { value := validException; value.Operation = "bad"; return value }(),
		func() wireException { value := validException; value.Rule.Ranges = nil; return value }(),
		func() wireException { value := validException; value.Source = ""; return value }(),
		func() wireException {
			value := validException
			value.Set = strings.Repeat("x", maxProvenanceBytes+1)
			return value
		}(),
	}
	for index, item := range badExceptions {
		value := wireBase
		value.Exceptions = []wireException{item}
		if _, err := scheduleFromWire(value, 1); err == nil {
			t.Errorf("invalid exception %d succeeded", index)
		}
	}
	badWeeklyRule := wireBase
	badWeeklyRule.Weekly = []wireWeekday{{Weekday: "monday", Rule: wireRule{State: "closed"}}}
	if _, err := scheduleFromWire(badWeeklyRule, 1); err == nil {
		t.Fatal("invalid weekly rule succeeded")
	}
}
