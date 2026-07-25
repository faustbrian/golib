package nats

import (
	"context"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"

	"github.com/nats-io/nats.go"
)

// Option for queue system
type Option func(*options)

type options struct {
	runFunc        func(context.Context, core.TaskMessage) error
	logger         queue.Logger
	addr           string
	subj           string
	queue          string
	requestTimeout time.Duration
	connectTimeout time.Duration
}

// WithAddr setup the addr of NATS
func WithAddr(addrs ...string) Option {
	return func(w *options) {
		if len(addrs) > 0 {
			w.addr = strings.Join(addrs, ",")
		}
	}
}

// WithSubj setup the subject of NATS
func WithSubj(subj string) Option {
	return func(w *options) {
		w.subj = subj
	}
}

// WithQueue setup the queue of NATS
func WithQueue(queue string) Option {
	return func(w *options) {
		w.queue = queue
	}
}

// WithRunFunc setup the run func of queue
func WithRunFunc(fn func(context.Context, core.TaskMessage) error) Option {
	return func(w *options) {
		w.runFunc = fn
	}
}

// WithLogger set custom logger
func WithLogger(l queue.Logger) Option {
	return func(w *options) {
		w.logger = l
	}
}

// WithRequestTimeout sets how long Request waits for a NATS message.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(w *options) {
		w.requestTimeout = timeout
	}
}

// WithConnectTimeout bounds the initial NATS connection attempt.
func WithConnectTimeout(timeout time.Duration) Option {
	return func(w *options) {
		w.connectTimeout = timeout
	}
}

func newOptions(opts ...Option) options {
	defaultOpts := options{
		addr:   nats.DefaultURL,
		subj:   "foobar",
		queue:  "foobar",
		logger: queue.NewLogger(),
		runFunc: func(context.Context, core.TaskMessage) error {
			return nil
		},
		requestTimeout: 6 * time.Second,
		connectTimeout: 2 * time.Second,
	}

	// Loop through each option
	for _, opt := range opts {
		// Call the option giving the instantiated
		opt(&defaultOpts)
	}

	return defaultOpts
}
