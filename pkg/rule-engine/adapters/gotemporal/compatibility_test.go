package ruleenginetemporal_test

import (
	"context"
	"testing"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginetemporal "github.com/faustbrian/golib/pkg/rule-engine/adapters/gotemporal"
)

func TestPersistedEncodingCompatibilityFixtures(t *testing.T) {
	t.Parallel()

	fixtures := []struct {
		name       string
		period     string
		instant    string
		membership bool
	}{
		{
			name:       "initial_unversioned_utc",
			period:     "period:2026-07-19T10:00:00Z|2026-07-19T11:00:00Z|[)",
			instant:    "instant:2026-07-19T10:30:00Z",
			membership: true,
		},
		{
			name:       "initial_unversioned_numeric_offset",
			period:     "period:2026-07-19T12:00:00+02:00|2026-07-19T13:00:00+02:00|[]",
			instant:    "instant:2026-07-19T09:59:59.999999999Z",
			membership: false,
		},
		{
			name:       "current_unversioned_nanoseconds",
			period:     "period:2026-07-19T10:00:00.000000001Z|2026-07-19T10:00:00.000000002Z|[]",
			instant:    "instant:2026-07-19T10:00:00.000000002Z",
			membership: true,
		},
	}
	operator := temporalOperatorByName(t, ruleenginetemporal.OpPeriodContains)
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			matched, err := operator.Evaluate(
				context.Background(),
				ruleengine.String(fixture.period),
				ruleengine.String(fixture.instant),
			)
			if err != nil || matched != fixture.membership {
				t.Fatalf("Evaluate() = %t, %v; want %t", matched, err, fixture.membership)
			}
		})
	}
}
