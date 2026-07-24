package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"

	"github.com/stretchr/testify/assert"
)

type customMetric struct {
	busy      atomic.Int64
	success   atomic.Uint64
	failure   atomic.Uint64
	submitted atomic.Uint64
}

func (m *customMetric) IncBusyWorker()         { m.busy.Add(1) }
func (m *customMetric) DecBusyWorker()         { m.busy.Add(-1) }
func (m *customMetric) BusyWorkers() int64     { return m.busy.Load() }
func (m *customMetric) IncSuccessTask()        { m.success.Add(1) }
func (m *customMetric) IncFailureTask()        { m.failure.Add(1) }
func (m *customMetric) IncSubmittedTask()      { m.submitted.Add(1) }
func (m *customMetric) SuccessTasks() uint64   { return m.success.Load() }
func (m *customMetric) FailureTasks() uint64   { return m.failure.Load() }
func (m *customMetric) SubmittedTasks() uint64 { return m.submitted.Load() }
func (m *customMetric) CompletedTasks() uint64 { return m.success.Load() + m.failure.Load() }

func TestQueueUsesConfiguredMetric(t *testing.T) {
	metric := &customMetric{}
	metric.submitted.Store(41)
	q, err := NewQueue(WithWorker(NewRing()), WithMetric(metric))
	assert.NoError(t, err)

	assert.NoError(t, q.Queue(mockMessage{message: "metric"}))
	assert.Equal(t, uint64(42), q.SubmittedTasks())
	q.Start()
	assert.Eventually(t, func() bool { return q.CompletedTasks() == 1 }, time.Second, time.Millisecond)
	q.Release()
}

func TestMetricData(t *testing.T) {
	w := NewRing(
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			switch string(m.Payload()) {
			case "foo1":
				panic("missing something")
			case "foo2":
				return errors.New("missing something")
			case "foo3":
				return nil
			}
			return nil
		}),
	)
	q, err := NewQueue(
		WithWorker(w),
		WithWorkerCount(4),
	)
	assert.NoError(t, err)
	assert.NoError(t, q.Queue(mockMessage{
		message: "foo1",
	}))
	assert.NoError(t, q.Queue(mockMessage{
		message: "foo2",
	}))
	assert.NoError(t, q.Queue(mockMessage{
		message: "foo3",
	}))
	assert.NoError(t, q.Queue(mockMessage{
		message: "foo4",
	}))
	q.Start()
	assert.Eventually(t, func() bool {
		return q.CompletedTasks() == 4
	}, time.Second, time.Millisecond)
	assert.Equal(t, uint64(4), q.SubmittedTasks())
	assert.Equal(t, uint64(2), q.SuccessTasks())
	assert.Equal(t, uint64(2), q.FailureTasks())
	assert.Equal(t, uint64(4), q.CompletedTasks())
	q.Release()
}
