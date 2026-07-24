package management_test

import (
	"context"
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

func ExampleDesiredStateReconciler_Reconcile() {
	target := management.Target{Kind: management.TargetQueue, Name: "critical"}
	reconciler, err := management.NewDesiredStateReconciler(
		management.DesiredStateReconcilerConfig{
			Reader: desiredExampleReader{record: management.DesiredRecord{
				Target: target, State: management.DesiredPaused, Revision: 3,
				ChangedAt: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
				CommandID: "pause-critical-3",
			}},
			Applier: desiredExampleApplier{},
			Targets: []management.Target{target},
		},
	)
	if err != nil {
		panic(err)
	}
	if err := reconciler.Reconcile(context.Background()); err != nil {
		panic(err)
	}
	// Output: apply paused to queue critical at revision 3
}

type desiredExampleReader struct {
	record management.DesiredRecord
}

func (r desiredExampleReader) GetDesiredState(
	context.Context,
	management.Target,
) (management.DesiredRecord, error) {
	return r.record, nil
}

type desiredExampleApplier struct{}

func (desiredExampleApplier) ApplyDesiredState(
	_ context.Context,
	record management.DesiredRecord,
) error {
	fmt.Printf(
		"apply %s to %s %s at revision %d\n",
		record.State,
		record.Target.Kind,
		record.Target.Name,
		record.Revision,
	)
	return nil
}
