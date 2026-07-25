package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/require"
)

type recordingObserver struct {
	mu     sync.Mutex
	events []Event
}

func (o *recordingObserver) Observe(event Event) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, event)
}

func (o *recordingObserver) kinds() []EventKind {
	o.mu.Lock()
	defer o.mu.Unlock()
	kinds := make([]EventKind, 0, len(o.events))
	for _, event := range o.events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

func (o *recordingObserver) snapshot() []Event {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Event(nil), o.events...)
}

func TestObserverReceivesSuccessfulTaskLifecycle(t *testing.T) {
	observer := &recordingObserver{}
	done := make(chan struct{})
	q, err := NewQueue(
		WithWorker(NewRing()),
		WithObserver(observer),
		WithAfterFn(func() { close(done) }),
	)
	require.NoError(t, err)

	q.Start()
	require.NoError(t, q.QueueTask(func(context.Context) error { return nil }))
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	q.Release()

	require.Subset(t, observer.kinds(), []EventKind{
		EventEnqueued,
		EventHandlerStarted,
		EventHandlerSucceeded,
		EventShutdownStarted,
		EventShutdownCompleted,
	})
	for _, event := range observer.snapshot() {
		require.Equal(t, "memory", event.Backend)
	}
}

func TestObserverReceivesRetryAndFailure(t *testing.T) {
	observer := &recordingObserver{}
	done := make(chan struct{})
	q, err := NewQueue(
		WithWorker(NewRing()),
		WithObserver(observer),
		WithAfterFn(func() { close(done) }),
	)
	require.NoError(t, err)

	q.Start()
	require.NoError(t, q.QueueTask(
		func(context.Context) error { return errors.New("fail") },
		job.AllowOption{RetryCount: job.Int64(1), RetryDelay: job.Time(time.Millisecond)},
	))
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	q.Release()

	require.Subset(t, observer.kinds(), []EventKind{
		EventRetryScheduled,
		EventHandlerFailed,
	})
	for _, event := range observer.snapshot() {
		if event.Kind != EventRetryScheduled && event.Kind != EventHandlerFailed {
			continue
		}
		require.Equal(t, management.ClassificationRetryable, event.Classification)
		require.Equal(t, "handler_failed", event.FailureCode)
	}
}
