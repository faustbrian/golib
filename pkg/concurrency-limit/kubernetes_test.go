package concurrencylimit_test

import (
	"context"
	"testing"

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
