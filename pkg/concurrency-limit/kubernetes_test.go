package concurrencylimit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
)

func TestPodScaleUpStartsIndependentColdStateAndRollingAlgorithmsCanMix(t *testing.T) {
	t.Parallel()

	oldPod := newFixedLimiter(t, 4, concurrencylimit.QueueConfig{})
	newAlgorithm, err := concurrencylimit.NewAIMDAlgorithm(concurrencylimit.AIMDConfig{Increase: 1, DecreaseFactor: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	newPod, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit: 1, MaxLimit: 8, InitialLimit: 2, Algorithm: newAlgorithm,
	})
	if err != nil {
		t.Fatal(err)
	}
	if oldPod.Snapshot().Limit+newPod.Snapshot().Limit != 6 {
		t.Fatal("scale-up did not multiply independent pod-local limits")
	}
	if oldPod.Snapshot().Algorithm.Name != "fixed" || newPod.Snapshot().Algorithm.Name != "aimd" {
		t.Fatal("rolling update did not preserve mixed algorithm identity")
	}

	permit, err := oldPod.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	oldPod.BeginDrain()
	if err = permit.Complete(concurrencylimit.OutcomeIgnored); err != nil {
		t.Fatal(err)
	}
	if oldPod.Snapshot().Samples != 0 || newPod.Snapshot().Samples != 0 {
		t.Fatal("shutdown cancellation changed learning")
	}
}

func TestScaleDownDrainReleasesQueueAndAbruptReplacementStartsCold(t *testing.T) {
	t.Parallel()

	oldPod := newFixedLimiter(t, 1, concurrencylimit.QueueConfig{MaxQueued: 2, MaxWait: time.Hour})
	active, err := oldPod.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	queued := make(chan error, 2)
	for range 2 {
		go func() {
			_, acquireErr := oldPod.Acquire(context.Background())
			queued <- acquireErr
		}()
	}
	waitForQueued(t, oldPod, 2)
	oldPod.BeginDrain()
	for range 2 {
		select {
		case acquireErr := <-queued:
			if !errors.Is(acquireErr, concurrencylimit.ErrDraining) {
				t.Fatalf("drained waiter error = %v", acquireErr)
			}
		case <-time.After(time.Second):
			t.Fatal("drained waiter did not terminate")
		}
	}
	if err = active.Complete(concurrencylimit.OutcomeIgnored); err != nil {
		t.Fatal(err)
	}
	if snapshot := oldPod.Snapshot(); snapshot.InFlight != 0 || snapshot.Queued != 0 || !snapshot.Draining || snapshot.Samples != 0 {
		t.Fatalf("drained pod snapshot = %+v", snapshot)
	}

	abruptPod := newFixedLimiter(t, 1, concurrencylimit.QueueConfig{})
	abandoned, err := abruptPod.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := abruptPod.Snapshot(); snapshot.InFlight != 1 {
		t.Fatalf("abrupt pod did not retain active process-local state: %+v", snapshot)
	}
	_ = abandoned

	replacement := newFixedLimiter(t, 1, concurrencylimit.QueueConfig{})
	if snapshot := replacement.Snapshot(); snapshot.Limit != 1 || snapshot.InFlight != 0 || snapshot.Queued != 0 ||
		snapshot.Samples != 0 || snapshot.Draining {
		t.Fatalf("abrupt replacement inherited process-local state: %+v", snapshot)
	}
}

func TestHPAFeedbackModelRequiresDemandAndRejectionSignals(t *testing.T) {
	t.Parallel()

	const demand = 100
	const admittedBefore = 80
	const admittedAfterLocalRejection = 40
	const cpuPerAdmitted = 1
	beforeCPU := admittedBefore * cpuPerAdmitted
	afterCPU := admittedAfterLocalRejection * cpuPerAdmitted
	rejections := demand - admittedAfterLocalRejection
	if afterCPU >= beforeCPU || rejections == 0 {
		t.Fatal("model must show lower CPU while unmet demand grows")
	}
}

func TestCPUOnlyHPAFeedbackCanScaleDownWhileRejectionsIncrease(t *testing.T) {
	t.Parallel()

	const demand = 100
	const perPodLimit = 10
	const perPodCPUCapacity = 20
	cpuOnlyReplicas := 4
	demandAwareReplicas := 4
	previousCPUOnlyRejections := 0
	for range 3 {
		cpuOnlyAdmitted := min(demand, cpuOnlyReplicas*perPodLimit)
		cpuOnlyUtilization := float64(cpuOnlyAdmitted) / float64(cpuOnlyReplicas*perPodCPUCapacity)
		cpuOnlyRejections := demand - cpuOnlyAdmitted
		if cpuOnlyUtilization < 0.7 && cpuOnlyReplicas > 1 {
			cpuOnlyReplicas--
		}
		if cpuOnlyRejections < previousCPUOnlyRejections {
			t.Fatalf("CPU-only rejection count unexpectedly improved: previous %d current %d", previousCPUOnlyRejections, cpuOnlyRejections)
		}
		previousCPUOnlyRejections = cpuOnlyRejections

		demandAwareAdmitted := min(demand, demandAwareReplicas*perPodLimit)
		if demand-demandAwareAdmitted > 0 {
			demandAwareReplicas++
		}
	}
	if cpuOnlyReplicas != 1 || demandAwareReplicas != 7 || previousCPUOnlyRejections != 80 {
		t.Fatalf("HPA model = CPU-only %d replicas/%d rejections, demand-aware %d replicas", cpuOnlyReplicas, previousCPUOnlyRejections, demandAwareReplicas)
	}
}
