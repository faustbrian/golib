package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/stretchr/testify/assert"
)

func TestWithRetryInterval(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     time.Duration
	}{
		{
			name:     "Set 2 seconds retry interval",
			duration: 2 * time.Second,
			want:     2 * time.Second,
		},
		{
			name:     "Set 500ms retry interval",
			duration: 500 * time.Millisecond,
			want:     500 * time.Millisecond,
		},
		{
			name:     "Set zero retry interval",
			duration: 0,
			want:     0,
		},
		{
			name:     "Set negative retry interval",
			duration: -1 * time.Second,
			want:     -1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := NewOptions(WithRetryInterval(tt.duration))
			if opts.retryInterval != tt.want {
				t.Errorf("WithRetryInterval() = %v, want %v", opts.retryInterval, tt.want)
			}
		})
	}
}

func TestNewOptionsUsesBoundedAndNilSafeDefaults(t *testing.T) {
	opts := NewOptions(
		nil,
		OptionFunc(nil),
		WithLogger(nil),
		WithMetric(nil),
		WithObserver(nil),
		WithFn(nil),
	)

	assert.Equal(t, 10_000, opts.queueSize)
	assert.NotNil(t, opts.logger)
	assert.NotNil(t, opts.metric)
	assert.NotNil(t, opts.observer)
	assert.NoError(t, opts.fn(context.Background(), nil))
}

func TestNewQueueRejectsUnsafeSchedulerConfiguration(t *testing.T) {
	worker := &optionTestWorker{}

	q, err := NewQueue(WithWorker(worker), WithRetryInterval(0))
	assert.Nil(t, q)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)

	q, err = NewQueue(WithWorker(worker), WithQueueSize(-1))
	assert.Nil(t, q)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)
	assert.Panics(t, func() { NewRing(WithQueueSize(-1)) })
}

type optionTestWorker struct{}

func (*optionTestWorker) Queue(core.TaskMessage) error { return nil }
func (*optionTestWorker) Request() (core.TaskMessage, error) {
	return nil, errors.New("unused")
}
func (*optionTestWorker) Run(context.Context, core.TaskMessage) error { return nil }
func (*optionTestWorker) Shutdown() error                             { return nil }
