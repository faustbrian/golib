package temporaltest_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/notation"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func TestPinnedPHPCompatibilityFixture(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile("../compat/fixtures/php_v1.json")
	if err != nil {
		t.Fatalf("ReadFile(): %v", err)
	}
	var fixture phpFixture
	if err := json.Unmarshal(payload, &fixture); err != nil {
		t.Fatalf("json.Unmarshal(): %v", err)
	}
	if fixture.Schema != "php-temporal-compat/v1" || fixture.Source.Commit != "469603239dbe700739c29b4c532a90382b6cbedf" {
		t.Fatalf("fixture provenance = %+v", fixture.Source)
	}
	assertPHPAPIInventory(t, fixture.PublicAPI)
	assertPHPBehaviorCoverage(t, fixture.PublicAPI, fixture.BehaviorCoverage)

	boundsMap := map[string]temporal.Bounds{
		"IncludeStartExcludeEnd": temporal.ClosedOpen,
		"IncludeAll":             temporal.Closed,
		"ExcludeStartIncludeEnd": temporal.OpenClosed,
		"ExcludeAll":             temporal.Open,
	}
	for _, item := range fixture.Bounds {
		bounds, ok := boundsMap[item.PHPCase]
		if !ok || bounds.IncludesStart() != item.IncludesStart || bounds.IncludesEnd() != item.IncludesEnd {
			t.Fatalf("bounds fixture = %+v", item)
		}
	}

	for _, item := range fixture.Times {
		parsed, err := timeofday.Parse(item.ISO, temporal.Limits{})
		if err != nil || parsed.Offset()/time.Microsecond != time.Duration(item.Microseconds) {
			t.Fatalf("time fixture %s = %v, %v", item.Name, parsed, err)
		}
	}
	if !timeofday.EndOfDay().IsEndBoundary() || timeofday.EndOfDay().Offset() != 24*time.Hour {
		t.Fatal("Go end boundary lost its deliberate distinction from the PHP last microsecond")
	}

	durations := map[string]time.Duration{
		"ordinary":  9 * time.Hour,
		"circular":  4 * time.Hour,
		"collapsed": 0,
		"full_day":  24 * time.Hour,
	}
	for _, item := range fixture.Intervals {
		decoded, err := notation.ParseDuration(item.Duration, temporal.Limits{})
		if duration, ok := durations[item.Name]; !ok || err != nil || decoded.Value() != duration {
			t.Fatalf("interval fixture = %+v", item)
		}
	}
	assertPHPDurationOperations(t, fixture.DurationOperations)
	assertPHPTimeOperations(t, fixture.TimeOperations)
	assertPHPIntervalOperations(t, fixture.IntervalOperations)
	if !fixture.Predicates.IncludesInside || fixture.Predicates.IncludesExcludedEnd ||
		!fixture.Predicates.Overlaps || fixture.Predicates.DoesNotOverlapAbutting ||
		!fixture.Predicates.Abuts || fixture.Predicates.DoesNotAbutGap {
		t.Fatalf("predicate fixture = %+v", fixture.Predicates)
	}
}

func assertPHPBehaviorCoverage(t *testing.T, inventory []string, coverage []phpBehaviorCoverage) {
	t.Helper()
	if len(coverage) != len(inventory) {
		t.Fatalf("PHP behavior coverage has %d entries for %d public symbols", len(coverage), len(inventory))
	}
	for index, item := range coverage {
		if item.Symbol != inventory[index] {
			t.Fatalf("PHP behavior coverage symbol %d = %q; want %q", index, item.Symbol, inventory[index])
		}
		if item.Status != "supported" && item.Status != "diverges" {
			t.Fatalf("PHP behavior coverage status for %s = %q", item.Symbol, item.Status)
		}
		if item.Contract == "" || item.GoEvidence == "" || item.Migration == "" {
			t.Fatalf("PHP behavior coverage for %s is incomplete: %+v", item.Symbol, item)
		}
		evidencePrefix := "go-test:"
		if item.Status == "diverges" {
			evidencePrefix = "go-divergence:"
		}
		if !strings.HasPrefix(item.GoEvidence, evidencePrefix) {
			t.Fatalf("PHP behavior coverage evidence for %s = %q; want %s", item.Symbol, item.GoEvidence, evidencePrefix)
		}
	}
}

func assertPHPAPIInventory(t *testing.T, inventory []string) {
	t.Helper()
	if len(inventory) < 200 {
		t.Fatalf("public PHP API inventory has only %d entries", len(inventory))
	}
	required := map[string]bool{
		`Cline\Temporal\Period\Bounds::buildIso80000()`: false,
		`Cline\Temporal\Period\Period::intersect()`:     false,
		`Cline\Temporal\Period\Sequence::subtract()`:    false,
		`Cline\Temporal\Time\Duration::sum()`:           false,
		`Cline\Temporal\Time\Interval::complement()`:    false,
		`Cline\Temporal\Time\IntervalSet::union()`:      false,
		`Cline\Temporal\Time\Time::shift()`:             false,
	}
	previous := ""
	for _, symbol := range inventory {
		if symbol <= previous {
			t.Fatalf("public PHP API inventory is not unique and sorted at %q", symbol)
		}
		if strings.Contains(symbol, `\Chart\`) {
			t.Fatalf("chart symbol entered non-chart inventory: %s", symbol)
		}
		if _, ok := required[symbol]; ok {
			required[symbol] = true
		}
		previous = symbol
	}
	for symbol, found := range required {
		if !found {
			t.Errorf("public PHP API inventory is missing %s", symbol)
		}
	}
}

func assertPHPDurationOperations(t *testing.T, fixture phpDurationOperations) {
	t.Helper()
	base, err := notation.ParseDuration(fixture.Base, temporal.Limits{})
	if err != nil || base.Value() != 90*time.Minute+500*time.Millisecond {
		t.Fatalf("duration base = %v, %v", base, err)
	}
	negative, _ := base.Negate()
	absolute, _ := negative.Abs()
	sum, _ := base.Add(timeofday.NewDuration(30 * time.Minute))
	multiplied, _ := base.Multiply(2)
	divided, _, _ := base.Divide(2)
	rounded, _ := base.Round(time.Hour, timeofday.RoundNearest)
	operations := map[string]timeofday.Duration{
		fixture.Negated:     negative,
		fixture.Absolute:    absolute,
		fixture.Sum:         sum,
		fixture.Multiplied:  multiplied,
		fixture.Divided:     divided,
		fixture.RoundedHour: rounded,
	}
	for encoded, want := range operations {
		got, err := notation.ParseDuration(encoded, temporal.Limits{})
		if err != nil || got != want {
			t.Fatalf("duration operation %q = %v, %v; want %v", encoded, got, err, want)
		}
	}
	if !fixture.CompareLonger || base.Compare(timeofday.NewDuration(time.Hour)) <= 0 {
		t.Fatal("duration comparison fixture mismatch")
	}
}

func assertPHPTimeOperations(t *testing.T, fixture phpTimeOperations) {
	t.Helper()
	start := mustPHPTime(t, "23:30:00")
	shifted, _ := start.Shift(2*time.Hour, timeofday.Wrap)
	rounded, _ := start.Round(time.Hour, timeofday.RoundNearest)
	clamped, _ := mustPHPTime(t, "07:00:00").Clamp(mustPHPTime(t, "08:00:00"), mustPHPTime(t, "17:00:00"))
	if !mustPHPTime(t, fixture.ShiftWrap).Equal(shifted) ||
		!mustPHPTime(t, fixture.Clamp).Equal(clamped) {
		t.Fatal("time operation fixture mismatch")
	}
	if !mustPHPTime(t, fixture.RoundHour).Equal(timeofday.Midnight()) || !rounded.IsEndBoundary() {
		t.Fatal("rounding fixture lost the documented PHP midnight/Go end-boundary divergence")
	}
	difference, _ := notation.ParseDuration(fixture.Difference, temporal.Limits{})
	distance, _ := notation.ParseDuration(fixture.Distance, temporal.Limits{})
	left := mustPHPTime(t, "22:00:00")
	right := mustPHPTime(t, "02:00:00")
	if difference.Value() != left.Difference(right) || distance.Value() != left.CircularDistance(right) {
		t.Fatal("time distance fixture mismatch")
	}
}

func assertPHPIntervalOperations(t *testing.T, fixture phpIntervalOperations) {
	t.Helper()
	left := mustPHPInterval(t, "[10:00:00,12:00:00)")
	overlap := mustPHPInterval(t, "[11:00:00,13:00:00)")
	far := mustPHPInterval(t, "[14:00:00,16:00:00)")
	intersection, _ := left.Intersection(overlap, temporal.Limits{})
	if !intersection.Equal(mustPHPDailySet(t, fixture.Intersection)) {
		t.Fatal("interval intersection fixture mismatch")
	}
	gap, err := left.Gap(far)
	if err != nil || !gap.SetEqual(mustPHPInterval(t, fixture.Gap)) {
		t.Fatal("interval gap fixture mismatch")
	}
	union, _ := left.Union(overlap, temporal.Limits{})
	if !union.Equal(mustPHPDailySet(t, fixture.Union...)) {
		t.Fatal("interval union fixture mismatch")
	}
	difference, _ := left.Difference(overlap, temporal.Limits{})
	if !difference.Equal(mustPHPDailySet(t, fixture.Difference...)) {
		t.Fatal("interval difference fixture mismatch")
	}
	leftSet, _ := timeofday.NewIntervalSet(temporal.Limits{}, left)
	complement, _ := leftSet.Complement()
	if !complement.Equal(mustPHPDailySet(t, fixture.Complement)) {
		t.Fatal("interval complement fixture mismatch")
	}
	parts, _ := left.Split(30*time.Minute, temporal.Limits{})
	partSet, _ := timeofday.NewIntervalSet(temporal.Limits{}, parts...)
	if !partSet.Equal(mustPHPDailySet(t, fixture.Split...)) {
		t.Fatal("interval split fixture mismatch")
	}
	steps, _ := left.Steps(30*time.Minute, temporal.Limits{})
	if len(steps) != len(fixture.Steps) {
		t.Fatal("interval steps fixture length mismatch")
	}
	for index := range steps {
		if !steps[index].Equal(mustPHPTime(t, fixture.Steps[index])) {
			t.Fatalf("interval step %d fixture mismatch", index)
		}
	}
}

func mustPHPTime(t *testing.T, encoded string) timeofday.Time {
	t.Helper()
	value, err := timeofday.Parse(encoded, temporal.Limits{})
	if err != nil {
		t.Fatalf("parse PHP time %q: %v", encoded, err)
	}
	return value
}

func mustPHPInterval(t *testing.T, encoded string) timeofday.Interval {
	t.Helper()
	value, err := notation.ParseDailyInterval(encoded, notation.ISO80000, temporal.Limits{})
	if err != nil {
		t.Fatalf("parse PHP interval %q: %v", encoded, err)
	}
	return value
}

func mustPHPDailySet(t *testing.T, encoded ...string) timeofday.IntervalSet {
	t.Helper()
	intervals := make([]timeofday.Interval, len(encoded))
	for index, value := range encoded {
		intervals[index] = mustPHPInterval(t, value)
	}
	set, err := timeofday.NewIntervalSet(temporal.Limits{}, intervals...)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

type phpFixture struct {
	Schema string `json:"schema"`
	Source struct {
		Commit string `json:"commit"`
	} `json:"source"`
	PublicAPI        []string              `json:"public_api"`
	BehaviorCoverage []phpBehaviorCoverage `json:"behavior_coverage"`
	Bounds           []struct {
		PHPCase       string `json:"php_case"`
		IncludesStart bool   `json:"includes_start"`
		IncludesEnd   bool   `json:"includes_end"`
	} `json:"bounds"`
	Times []struct {
		Name         string `json:"name"`
		ISO          string `json:"iso"`
		Microseconds int64  `json:"microseconds"`
	} `json:"times"`
	Intervals []struct {
		Name     string `json:"name"`
		Duration string `json:"duration"`
	} `json:"intervals"`
	DurationOperations phpDurationOperations `json:"duration_operations"`
	TimeOperations     phpTimeOperations     `json:"time_operations"`
	IntervalOperations phpIntervalOperations `json:"interval_operations"`
	Predicates         struct {
		IncludesInside         bool `json:"includes_inside"`
		IncludesExcludedEnd    bool `json:"includes_excluded_end"`
		Overlaps               bool `json:"overlaps"`
		DoesNotOverlapAbutting bool `json:"does_not_overlap_abutting"`
		Abuts                  bool `json:"abuts"`
		DoesNotAbutGap         bool `json:"does_not_abut_gap"`
	} `json:"predicates"`
}

type phpBehaviorCoverage struct {
	Symbol     string `json:"symbol"`
	Status     string `json:"status"`
	Contract   string `json:"contract"`
	GoEvidence string `json:"go_evidence"`
	Migration  string `json:"migration"`
}

type phpDurationOperations struct {
	Base          string `json:"base"`
	Negated       string `json:"negated"`
	Absolute      string `json:"absolute"`
	Sum           string `json:"sum"`
	Multiplied    string `json:"multiplied"`
	Divided       string `json:"divided"`
	RoundedHour   string `json:"rounded_hour"`
	CompareLonger bool   `json:"compare_longer"`
}

type phpTimeOperations struct {
	ShiftWrap  string `json:"shift_wrap"`
	RoundHour  string `json:"round_hour"`
	Difference string `json:"difference"`
	Distance   string `json:"distance"`
	Clamp      string `json:"clamp"`
}

type phpIntervalOperations struct {
	Intersection string   `json:"intersection"`
	Gap          string   `json:"gap"`
	Union        []string `json:"union"`
	Difference   []string `json:"difference"`
	Complement   string   `json:"complement"`
	Split        []string `json:"split"`
	Steps        []string `json:"steps"`
}
