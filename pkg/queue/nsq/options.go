package nsq

import (
	"context"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"

	nsq "github.com/nsqio/go-nsq"
)

// An Option configures a mutex.
type Option interface {
	Apply(*Options)
}

// OptionFunc is a function that configures a queue.
type OptionFunc func(*Options)

// Apply calls f(option)
func (f OptionFunc) Apply(option *Options) {
	f(option)
}

type Options struct {
	maxInFlight          int
	addr                 string
	topic                string
	channel              string
	runFunc              func(context.Context, core.TaskMessage) error
	logger               queue.Logger
	logLevel             nsq.LogLevel
	requestTimeout       time.Duration
	touchInterval        time.Duration
	connectTimeout       time.Duration
	deadLetterTopic      string
	maxDeliveryAttempts  uint16
	deadLetterConfigured bool
}

// WithDeadLetter configures the package-owned terminal topic and bounded NSQ
// delivery-attempt policy.
func WithDeadLetter(topic string, maxAttempts uint16) Option {
	return OptionFunc(func(o *Options) {
		o.deadLetterTopic = topic
		o.maxDeliveryAttempts = maxAttempts
		o.deadLetterConfigured = true
	})
}

// WithAddr setup the addr of NSQ
func WithAddr(addr string) Option {
	return OptionFunc(func(o *Options) {
		o.addr = addr
	})
}

// WithTopic setup the topic of NSQ
func WithTopic(topic string) Option {
	return OptionFunc(func(o *Options) {
		o.topic = topic
	})
}

// WithChannel setup the channel of NSQ
func WithChannel(channel string) Option {
	return OptionFunc(func(o *Options) {
		o.channel = channel
	})
}

// WithRunFunc setup the run func of queue
func WithRunFunc(fn func(context.Context, core.TaskMessage) error) Option {
	return OptionFunc(func(o *Options) {
		o.runFunc = fn
	})
}

// WithMaxInFlight Maximum number of messages to allow in flight (concurrency knob)
func WithMaxInFlight(num int) Option {
	return OptionFunc(func(o *Options) {
		o.maxInFlight = num
	})
}

// WithLogger set custom logger
func WithLogger(l queue.Logger) Option {
	return OptionFunc(func(o *Options) {
		o.logger = l
	})
}

// WithLogLevel set custom [nsq] log level
func WithLogLevel(lvl nsq.LogLevel) Option {
	return OptionFunc(func(o *Options) {
		o.logLevel = lvl
	})
}

// WithRequestTimeout sets how long Request waits for an NSQ message.
func WithRequestTimeout(timeout time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.requestTimeout = timeout
	})
}

// WithTouchInterval sets how often an in-flight NSQ message is touched.
func WithTouchInterval(interval time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.touchInterval = interval
	})
}

// WithConnectTimeout bounds NSQ producer and consumer connection attempts.
func WithConnectTimeout(timeout time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.connectTimeout = timeout
	})
}

func newOptions(opts ...Option) Options {
	defaultOpts := Options{
		addr:        "127.0.0.1:4150",
		topic:       "gorush",
		channel:     "ch",
		maxInFlight: 1,

		logger:   queue.NewLogger(),
		logLevel: nsq.LogLevelInfo,
		runFunc: func(context.Context, core.TaskMessage) error {
			return nil
		},
		requestTimeout: 6 * time.Second,
		touchInterval:  2 * time.Second,
		connectTimeout: time.Second,
	}

	// Loop through each option
	for _, opt := range opts {
		// Call the option giving the instantiated
		opt.Apply(&defaultOpts)
	}
	if !defaultOpts.deadLetterConfigured {
		defaultOpts.deadLetterTopic = defaultOpts.topic + "-dead"
		defaultOpts.maxDeliveryAttempts = 5
	}

	return defaultOpts
}
