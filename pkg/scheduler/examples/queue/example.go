// Package queueexample demonstrates wiring durable queue dispatch into a
// scheduler runner. The application supplies its configured queue and lease
// backends.
package queueexample

import (
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/lease"
	schedulerqueue "github.com/faustbrian/golib/pkg/scheduler/queue"
)

// NewRunner builds a production-shaped runner around application-owned durable
// queue and distributed lease backends.
func NewRunner(
	backend schedulerqueue.Enqueuer,
	leases lease.Store,
	owner string,
) (*scheduler.Runner, error) {
	schedule, err := scheduler.NewSchedule(
		"nightly-report",
		"reports.generate",
		scheduler.Daily(),
		scheduler.WithTimezone("Europe/Helsinki"),
		scheduler.WithParameters(map[string]any{"format": "pdf"}),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithOneServer(5*time.Minute),
	)
	if err != nil {
		return nil, err
	}
	registry, err := scheduler.Compile(schedule)
	if err != nil {
		return nil, err
	}
	dispatcher, err := schedulerqueue.New(backend)
	if err != nil {
		return nil, err
	}
	return scheduler.NewRunner(registry, leases, dispatcher, scheduler.WithOwner(owner))
}
