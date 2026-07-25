package temporaltest_test

import (
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	"github.com/faustbrian/golib/pkg/temporal/temporaltest"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func TestAllenCasesAreExhaustiveAndConverseCoherent(t *testing.T) {
	t.Parallel()

	cases, err := temporaltest.AllenCases(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC), time.Hour)
	if err != nil {
		t.Fatalf("AllenCases(): %v", err)
	}
	if len(cases) != 13 {
		t.Fatalf("len(cases) = %d", len(cases))
	}
	seen := make(map[temporal.Relation]bool, len(cases))
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()
			temporaltest.AssertRelation(t, fixture.Left, fixture.Right, fixture.Want)
			reverse, err := fixture.Right.RelationTo(fixture.Left)
			if err != nil || reverse != fixture.Want.Converse() {
				t.Fatalf("reverse relation = %v, %v", reverse, err)
			}
		})
		seen[fixture.Want] = true
	}
	if len(seen) != 13 {
		t.Fatalf("unique relations = %d", len(seen))
	}
	if _, err := temporaltest.AllenCases(time.Time{}, 0); err == nil {
		t.Fatal("AllenCases(zero step) error = nil")
	}
}

func TestSetAssertionsCompareRepresentedMembership(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	a, _ := instant.New(start, start.Add(time.Hour), temporal.ClosedOpen)
	b, _ := instant.New(start.Add(time.Hour), start.Add(2*time.Hour), temporal.ClosedOpen)
	combined, _ := instant.New(start, start.Add(2*time.Hour), temporal.ClosedOpen)
	set, _ := instant.NewSet(temporal.Limits{}, a, b)
	want, _ := instant.NewSet(temporal.Limits{}, combined)
	temporaltest.AssertInstantSetEqual(t, set, want)

	eight, _ := timeofday.Parse("08:00", temporal.Limits{})
	nine, _ := timeofday.Parse("09:00", temporal.Limits{})
	daily, _ := timeofday.Between(eight, nine, temporal.ClosedOpen)
	dailySet, _ := timeofday.NewIntervalSet(temporal.Limits{}, daily)
	temporaltest.AssertDailySetEqual(t, dailySet, dailySet)
}

func TestAssertionsReportUsefulFailures(t *testing.T) {
	t.Parallel()

	recorder := &recordingT{}
	cases, err := temporaltest.AllenCases(time.Time{}, time.Hour)
	if err != nil {
		t.Fatalf("AllenCases(): %v", err)
	}
	temporaltest.AssertRelation(recorder, cases[0].Left, cases[0].Right, temporal.After)

	emptyInstant, _ := instant.NewSet(temporal.Limits{})
	nonemptyInstant, _ := instant.NewSet(temporal.Limits{}, cases[0].Left)
	temporaltest.AssertInstantSetEqual(recorder, emptyInstant, nonemptyInstant)

	emptyDaily, _ := timeofday.NewIntervalSet(temporal.Limits{})
	temporaltest.AssertDailySetEqual(recorder, emptyDaily, mustDailySet(t))
	if recorder.failures != 3 || !recorder.helperCalled {
		t.Fatalf("recorder = %+v", recorder)
	}

	empty, _ := instant.New(time.Time{}, time.Time{}, temporal.Open)
	temporaltest.AssertRelation(recorder, empty, cases[0].Right, temporal.Before)
	if recorder.failures != 4 {
		t.Fatalf("relation error was not reported: %+v", recorder)
	}
}

func mustDailySet(t *testing.T) timeofday.IntervalSet {
	t.Helper()
	start, _ := timeofday.Parse("08:00", temporal.Limits{})
	end, _ := timeofday.Parse("09:00", temporal.Limits{})
	interval, _ := timeofday.Between(start, end, temporal.ClosedOpen)
	set, err := timeofday.NewIntervalSet(temporal.Limits{}, interval)
	if err != nil {
		t.Fatalf("NewIntervalSet(): %v", err)
	}
	return set
}

type recordingT struct {
	helperCalled bool
	failures     int
}

func (t *recordingT) Helper() { t.helperCalled = true }
func (t *recordingT) Errorf(_ string, _ ...any) {
	t.failures++
}

var _ temporaltest.TestingT = (*recordingT)(nil)
