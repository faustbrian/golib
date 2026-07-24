package memory_test

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease/conformance"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
)

func TestConformance(t *testing.T) {
	conformance.TestStore(t, func(*testing.T) conformance.Harness {
		now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		return conformance.Harness{
			Store:   memory.New(),
			Now:     func() time.Time { return now },
			Advance: func(duration time.Duration) { now = now.Add(duration) },
		}
	})
}
