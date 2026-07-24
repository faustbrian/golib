// Package schedule provides explicit correlation lifecycle helpers for
// scheduled work. Independent invocations start independent workflows unless
// application-owned metadata is deliberately propagated.
package schedule

import (
	"errors"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	queuecorrelation "github.com/faustbrian/golib/pkg/correlation/queue"
)

// ErrInvalidOptions reports missing scheduled-work dependencies.
var ErrInvalidOptions = errors.New("schedule correlation: invalid options")

// Options configure optional scheduled metadata propagation.
type Options struct {
	Queue queuecorrelation.Options
}

// Adapter starts independent runs and explicitly propagates scheduled jobs.
type Adapter struct {
	factory *correlation.Factory
	queue   *queuecorrelation.Adapter
}

// New constructs a scheduled-work adapter.
func New(factory *correlation.Factory, options Options) (*Adapter, error) {
	if factory == nil {
		return nil, ErrInvalidOptions
	}
	queueAdapter, err := queuecorrelation.New(factory, options.Queue)
	if err != nil {
		return nil, err
	}
	return &Adapter{factory: factory, queue: queueAdapter}, nil
}

// Start begins an independent scheduler invocation with no implicit derived
// or stable workflow identity.
func (adapter *Adapter) Start() (correlation.Values, error) {
	if adapter == nil || adapter.factory == nil {
		return correlation.Values{}, ErrInvalidOptions
	}
	return adapter.factory.Start()
}

// Enqueue creates a child scheduled-work message.
func (adapter *Adapter) Enqueue(metadata map[string]string, parent correlation.Values) (correlation.Values, error) {
	if adapter == nil || adapter.queue == nil {
		return correlation.Values{}, ErrInvalidOptions
	}
	return adapter.queue.Send(metadata, parent)
}

// Run receives explicitly trusted scheduler metadata as a fresh attempt.
func (adapter *Adapter) Run(metadata map[string]string, trusted bool) (correlation.Values, error) {
	if adapter == nil || adapter.queue == nil {
		return correlation.Values{}, ErrInvalidOptions
	}
	return adapter.queue.Receive(metadata, trusted)
}
