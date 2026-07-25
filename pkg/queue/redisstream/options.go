package redisdb

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/management"
)

const maxReplayDestinations = 64

// Option for queue system
type Option func(*options)

type options struct {
	runFunc                func(context.Context, core.TaskMessage) error
	logger                 queue.Logger
	addr                   string
	db                     int
	connectionString       string
	username               string
	password               string
	streamName             string
	cluster                bool
	group                  string
	consumer               string
	maxLength              int64
	blockTime              time.Duration
	tls                    *tls.Config
	requestTimeout         time.Duration
	connectTimeout         time.Duration
	reclaimMinIdle         time.Duration
	reclaimInterval        time.Duration
	reclaimBatchSize       int64
	failureStream          string
	deadLetterStream       string
	maxDeliveryAttempts    int64
	commandTimeout         time.Duration
	management             *management.StatusMetadata
	replayDestinations     map[string]struct{}
	replayInvalid          bool
	recordMaxLength        int64
	recordRetentionInvalid bool
}

func (o options) validateDeadLetter() error {
	invalid := o.reclaimMinIdle <= 0 || o.reclaimInterval <= 0 ||
		o.reclaimBatchSize <= 0 || o.reclaimBatchSize > streamqueue.MaxBatchSize ||
		o.maxDeliveryAttempts < 2 || o.commandTimeout <= 0 ||
		strings.TrimSpace(o.failureStream) == "" || strings.TrimSpace(o.deadLetterStream) == "" ||
		o.failureStream == o.streamName || o.deadLetterStream == o.streamName ||
		o.failureStream == o.deadLetterStream || o.replayInvalid ||
		(o.replayDestinations != nil && len(o.replayDestinations) == 0) ||
		o.recordRetentionInvalid || o.recordMaxLength < 0
	for destination := range o.replayDestinations {
		invalid = invalid || destination == o.failureStream ||
			destination == o.deadLetterStream
	}
	if invalid {
		return fmt.Errorf("%w: unsafe Redis Streams dead-letter policy", queue.ErrInvalidConfiguration)
	}

	return nil
}

// WithRecordRetention deliberately enables approximate maximum-count
// retention for failure and dead-letter streams. It is disabled by default.
func WithRecordRetention(maxRecords int64) Option {
	return func(w *options) {
		w.recordMaxLength = maxRecords
		w.recordRetentionInvalid = maxRecords <= 0
	}
}

// WithReplayDestinations allowlists bounded logical streams for
// administrative replay. Replay remains disabled when this option is absent.
func WithReplayDestinations(destinations ...string) Option {
	return func(w *options) {
		w.replayDestinations = make(map[string]struct{}, len(destinations))
		if len(destinations) == 0 || len(destinations) > maxReplayDestinations {
			w.replayInvalid = true
			return
		}
		for _, destination := range destinations {
			destination = strings.TrimSpace(destination)
			if destination == "" || len(destination) > management.MaxIdentityBytes ||
				strings.ContainsAny(destination, "\x00\r\n") {
				w.replayInvalid = true
				continue
			}
			if _, duplicate := w.replayDestinations[destination]; duplicate {
				w.replayInvalid = true
				continue
			}
			w.replayDestinations[destination] = struct{}{}
		}
	}
}

// WithReclaim configures bounded stale pending-entry recovery.
func WithReclaim(minIdle, interval time.Duration, batchSize int64) Option {
	return func(w *options) {
		w.reclaimMinIdle = minIdle
		w.reclaimInterval = interval
		w.reclaimBatchSize = batchSize
	}
}

// WithFailureStream configures the stream retaining failed delivery attempts.
func WithFailureStream(stream string) Option {
	return func(w *options) { w.failureStream = stream }
}

// WithDeadLetter configures the terminal stream and delivery-attempt limit.
func WithDeadLetter(stream string, maxAttempts int64) Option {
	return func(w *options) {
		w.deadLetterStream = stream
		w.maxDeliveryAttempts = maxAttempts
	}
}

// WithCommandTimeout bounds record append and source settlement commands.
func WithCommandTimeout(timeout time.Duration) Option {
	return func(w *options) { w.commandTimeout = timeout }
}

// WithManagementStatus enables native worker and queue status reporting.
func WithManagementStatus(metadata management.StatusMetadata) Option {
	return func(w *options) {
		copyMetadata := metadata
		w.management = &copyMetadata
	}
}

// WithAddr setup the addr of redis
func WithAddr(addr string) Option {
	return func(w *options) {
		w.addr = addr
	}
}

// WithMaxLength setup the max length for publish messages
func WithMaxLength(m int64) Option {
	return func(w *options) {
		w.maxLength = m
	}
}

// WithBlockTime configures the preferred blocking read duration. Reads poll at
// least once per second so shutdown remains bounded.
// we use the block command to make sure if no entry is found we wait
// until an entry is found
func WithBlockTime(m time.Duration) Option {
	return func(w *options) {
		w.blockTime = m
	}
}

// WithPassword redis password
func WithDB(db int) Option {
	return func(w *options) {
		w.db = db
	}
}

// WithCluster redis cluster
func WithCluster() Option {
	return func(w *options) {
		w.cluster = true
	}
}

// WithStreamName Stream name
func WithStreamName(name string) Option {
	return func(w *options) {
		w.streamName = name
	}
}

// WithGroup group name
func WithGroup(name string) Option {
	return func(w *options) {
		w.group = name
	}
}

// WithConsumer consumer name
func WithConsumer(name string) Option {
	return func(w *options) {
		w.consumer = name
	}
}

// WithUsername redis username
// This is only used for redis cluster
func WithUsername(username string) Option {
	return func(w *options) {
		w.username = username
	}
}

// WithPassword redis password
func WithPassword(passwd string) Option {
	return func(w *options) {
		w.password = passwd
	}
}

// WithConnectionString redis connection string
func WithConnectionString(connectionString string) Option {
	return func(w *options) {
		w.connectionString = connectionString
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

// WithTLS returns an Option that configures the use of TLS for the connection.
// It sets the minimum TLS version to TLS 1.2.
func WithTLS() Option {
	return func(w *options) {
		w.tls = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}
}

// WithSkipTLSVerify returns an Option that configures the TLS settings to skip
// verification of the server's certificate. This is useful for connecting to
// servers with self-signed certificates or when certificate verification is
// not required. Use this option with caution as it makes the connection
// susceptible to man-in-the-middle attacks.
func WithSkipTLSVerify() Option {
	return func(w *options) {
		if w.tls == nil {
			w.tls = &tls.Config{
				InsecureSkipVerify: true, //nolint: gosec

			}
			return
		}
		w.tls.InsecureSkipVerify = true
	}
}

// WithRequestTimeout sets how long Request waits for a stream message.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(w *options) {
		w.requestTimeout = timeout
	}
}

// WithConnectTimeout bounds initial Redis connection validation.
func WithConnectTimeout(timeout time.Duration) Option {
	return func(w *options) {
		w.connectTimeout = timeout
	}
}

func newOptions(opts ...Option) options {
	defaultOpts := options{
		streamName: "golang-queue",
		group:      "golang-queue",
		consumer:   "golang-queue",
		logger:     queue.NewLogger(),
		runFunc: func(context.Context, core.TaskMessage) error {
			return nil
		},
		blockTime:           60 * time.Second,
		requestTimeout:      6 * time.Second,
		connectTimeout:      5 * time.Second,
		reclaimMinIdle:      30 * time.Second,
		reclaimInterval:     time.Second,
		reclaimBatchSize:    16,
		maxDeliveryAttempts: 5,
		commandTimeout:      5 * time.Second,
	}

	// Loop through each option
	for _, opt := range opts {
		// Call the option giving the instantiated
		opt(&defaultOpts)
	}
	if defaultOpts.failureStream == "" {
		defaultOpts.failureStream = defaultOpts.streamName + "-failures"
	}
	if defaultOpts.deadLetterStream == "" {
		defaultOpts.deadLetterStream = defaultOpts.streamName + "-dead"
	}

	return defaultOpts
}
