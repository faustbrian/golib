package bulkhead_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/bulkhead"
)

func TestKubernetesSIGTERMRemovesReadinessBeforeBoundedDrain(t *testing.T) {
	pod := newPodModel(t, "revision-1", 1)
	started := make(chan struct{})
	finish := make(chan struct{})
	completed := executeAsync(pod.policy, context.Background(), func(context.Context) (struct{}, error) {
		close(started)
		<-finish
		return struct{}{}, nil
	})
	<-started

	drainContext, cancel := context.WithCancel(context.Background())
	cancel()
	err := pod.terminate(drainContext)
	if !errors.Is(err, bulkhead.ErrDrainIncomplete) || !errors.Is(err, context.Canceled) {
		t.Fatalf("terminate() error = %v, want bounded incomplete drain", err)
	}
	if pod.ready {
		t.Fatal("terminating pod remained ready")
	}
	if _, err := pod.policy.Acquire(context.Background(), 1); !errors.Is(err, bulkhead.ErrClosed) {
		t.Fatalf("post-readiness-removal Acquire() error = %v, want ErrClosed", err)
	}
	if snapshot := pod.policy.Snapshot(); snapshot.ActiveWeight != 1 || !snapshot.Draining || snapshot.Drained {
		t.Fatalf("incomplete drain Snapshot() = %+v", snapshot)
	}

	close(finish)
	if err := receiveExecution(t, completed); err != nil {
		t.Fatalf("admitted Execute() error = %v", err)
	}
	if err := drainWithin(pod.policy); err != nil {
		t.Fatalf("completed Drain() error = %v", err)
	}
}

func TestKubernetesAbruptKillCannotProduceCompletionEvidence(t *testing.T) {
	pod := newPodModel(t, "revision-1", 1)
	permit, err := pod.policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	pod.ready = false
	pod.abruptlyKilled = true

	snapshot := pod.policy.Snapshot()
	if snapshot.ActiveWeight != 1 || snapshot.Draining || snapshot.Drained {
		t.Fatalf("abrupt-kill pre-exit Snapshot() = %+v", snapshot)
	}
	if pod.completed() {
		t.Fatal("abruptly killed pod reported admitted work completed")
	}

	// Test cleanup models the otherwise unobservable operation return. A real
	// process kill discards this process-local state and cannot emit completion.
	if err := permit.Release(); err != nil {
		t.Fatalf("cleanup Release() error = %v", err)
	}
}

func TestKubernetesScaleUpScaleDownAndMixedRevisionsRemainProcessLocal(t *testing.T) {
	oldA := newPodModel(t, "revision-1", 2)
	oldB := newPodModel(t, "revision-1", 2)
	newPod := newPodModel(t, "revision-2", 1)
	pods := []*podModel{oldA, oldB, newPod}

	oldPermits := acquirePodCapacity(t, oldA)
	if _, err := oldA.policy.Acquire(context.Background(), 1); !errors.Is(err, bulkhead.ErrRejected) {
		t.Fatalf("saturated old pod Acquire() error = %v, want ErrRejected", err)
	}
	coldPermit, err := newPod.policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("cold scale-up pod Acquire() error = %v", err)
	}
	if snapshot := oldB.policy.Snapshot(); snapshot.ActiveWeight != 0 || snapshot.AvailableWeight != 2 {
		t.Fatalf("independent old pod Snapshot() = %+v", snapshot)
	}

	var aggregateCapacity, aggregateActive int64
	revisions := map[string]int{}
	for _, pod := range pods {
		snapshot := pod.policy.Snapshot()
		aggregateCapacity += snapshot.Capacity
		aggregateActive += snapshot.ActiveWeight
		revisions[snapshot.PolicyRevision]++
	}
	if aggregateCapacity != 5 || aggregateActive != 3 ||
		revisions["revision-1"] != 2 || revisions["revision-2"] != 1 {
		t.Fatalf("mixed rollout = capacity %d, active %d, revisions %+v", aggregateCapacity, aggregateActive, revisions)
	}

	oldA.ready = false
	if err := oldA.policy.Close(); err != nil {
		t.Fatalf("scale-down Close() error = %v", err)
	}
	drainContext, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := oldA.policy.Drain(drainContext); !errors.Is(err, bulkhead.ErrDrainIncomplete) {
		t.Fatalf("active scale-down Drain() error = %v, want ErrDrainIncomplete", err)
	}
	for _, permit := range oldPermits {
		_ = permit.Release()
	}
	if err := drainWithin(oldA.policy); err != nil {
		t.Fatalf("scale-down Drain() after completion error = %v", err)
	}
	if oldA.ready {
		t.Fatal("scaled-down pod remained ready")
	}
	_ = coldPermit.Release()
}

type podModel struct {
	policy         *bulkhead.Bulkhead
	ready          bool
	abruptlyKilled bool
}

func newPodModel(t *testing.T, revision string, capacity int64) *podModel {
	t.Helper()
	return &podModel{
		policy: mustPolicy(t, bulkhead.Config{
			Resource:       "database",
			PolicyRevision: revision,
			Capacity:       capacity,
		}),
		ready: true,
	}
}

func (pod *podModel) terminate(ctx context.Context) error {
	pod.ready = false
	if err := pod.policy.Close(); err != nil {
		return err
	}
	return pod.policy.Drain(ctx)
}

func (pod *podModel) completed() bool {
	return !pod.abruptlyKilled && pod.policy.Snapshot().Drained
}

func acquirePodCapacity(t *testing.T, pod *podModel) []*bulkhead.Permit {
	t.Helper()
	capacity := pod.policy.Snapshot().Capacity
	permits := make([]*bulkhead.Permit, 0, capacity)
	for range capacity {
		permit, err := pod.policy.Acquire(context.Background(), 1)
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		permits = append(permits, permit)
	}
	return permits
}
