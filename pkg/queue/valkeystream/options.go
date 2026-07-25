package valkeystream

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/management"
)

const (
	maxReadBatchSize      = 256
	maxReclaimBatchSize   = 256
	maxBlockingPoolSize   = 128
	maxReplayDestinations = 64
)

type options struct {
	address             string
	username            string
	password            string
	db                  int
	tlsConfig           *tls.Config
	clientName          string
	dialTimeout         time.Duration
	commandTimeout      time.Duration
	requestTimeout      time.Duration
	blockTime           time.Duration
	shutdownTimeout     time.Duration
	blockingPoolMinSize int
	blockingPoolSize    int
	blockingPoolCleanup time.Duration
	stream              string
	group               string
	consumer            string
	maxLength           int64
	recordMaxLength     int64
	readBatchSize       int
	reclaimMinIdle      time.Duration
	reclaimInterval     time.Duration
	reclaimBatchSize    int
	failureStream       string
	deadLetterStream    string
	maxDeliveryAttempts int64
	logger              queue.Logger
	runFunc             func(context.Context, core.TaskMessage) error
	management          *management.StatusMetadata
	replayDestinations  map[string]struct{}
}

// WithManagementStatus enables native worker and queue status reporting.
func WithManagementStatus(metadata management.StatusMetadata) Option {
	return func(opts *options) error {
		if err := metadata.Validate(); err != nil {
			return ErrInvalidManagementStatus
		}
		copyMetadata := metadata
		opts.management = &copyMetadata

		return nil
	}
}

// Option configures a Valkey Streams worker without exposing native client
// option types.
type Option func(*options) error

// WithAddress sets the standalone Valkey host and port.
func WithAddress(address string) Option {
	return func(opts *options) error {
		opts.address = strings.TrimSpace(address)
		return nil
	}
}

// WithAuthentication sets Valkey ACL credentials.
func WithAuthentication(username, password string) Option {
	return func(opts *options) error {
		opts.username = username
		opts.password = password
		return nil
	}
}

// WithDB selects the standalone Valkey database.
func WithDB(database int) Option {
	return func(opts *options) error {
		opts.db = database
		return nil
	}
}

// WithTLSConfig enables TLS using a private clone of config.
func WithTLSConfig(config *tls.Config) Option {
	return func(opts *options) error {
		if config == nil {
			return invalidOption("tls", errors.New("TLS configuration is required"))
		}
		opts.tlsConfig = config.Clone()
		if opts.tlsConfig.MinVersion < tls.VersionTLS12 {
			opts.tlsConfig.MinVersion = tls.VersionTLS12
		}
		return nil
	}
}

// WithClientName sets the name reported to Valkey for owned connections.
func WithClientName(name string) Option {
	return func(opts *options) error {
		opts.clientName = strings.TrimSpace(name)
		return nil
	}
}

// WithDialTimeout bounds initial and reconnect dial attempts.
func WithDialTimeout(timeout time.Duration) Option {
	return func(opts *options) error {
		opts.dialTimeout = timeout
		return nil
	}
}

// WithCommandTimeout bounds non-blocking Valkey commands.
func WithCommandTimeout(timeout time.Duration) Option {
	return func(opts *options) error {
		opts.commandTimeout = timeout
		return nil
	}
}

// WithRequestTimeout bounds how long Request waits for a delivery.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(opts *options) error {
		opts.requestTimeout = timeout
		return nil
	}
}

// WithBlockTime bounds each consumer-group blocking read.
func WithBlockTime(timeout time.Duration) Option {
	return func(opts *options) error {
		opts.blockTime = timeout
		return nil
	}
}

// WithShutdownTimeout bounds graceful worker shutdown.
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(opts *options) error {
		opts.shutdownTimeout = timeout
		return nil
	}
}

// WithBlockingPool configures the bounded pool used by blocking commands.
func WithBlockingPool(minimum, maximum int, cleanup time.Duration) Option {
	return func(opts *options) error {
		opts.blockingPoolMinSize = minimum
		opts.blockingPoolSize = maximum
		opts.blockingPoolCleanup = cleanup
		return nil
	}
}

// WithStreamName sets the Valkey stream key.
func WithStreamName(name string) Option {
	return func(opts *options) error {
		opts.stream = strings.TrimSpace(name)
		return nil
	}
}

// WithGroup sets the Valkey consumer group.
func WithGroup(name string) Option {
	return func(opts *options) error {
		opts.group = strings.TrimSpace(name)
		return nil
	}
}

// WithConsumer sets the stable identity used for reads and reclaim ownership.
func WithConsumer(name string) Option {
	return func(opts *options) error {
		opts.consumer = strings.TrimSpace(name)
		return nil
	}
}

// WithMaxLength sets the approximate maximum stream length.
func WithMaxLength(length int64) Option {
	return func(opts *options) error {
		opts.maxLength = length
		return nil
	}
}

// WithRecordRetention deliberately enables exact maximum-count retention for
// failure and dead-letter streams. It is disabled by default.
func WithRecordRetention(maxRecords int64) Option {
	return func(opts *options) error {
		if maxRecords <= 0 {
			return invalidOption("record retention", errors.New("unsafe value"))
		}
		opts.recordMaxLength = maxRecords
		return nil
	}
}

// WithReadBatchSize bounds entries returned by each XREADGROUP command.
func WithReadBatchSize(size int) Option {
	return func(opts *options) error {
		opts.readBatchSize = size
		return nil
	}
}

// WithReclaim configures stale-delivery recovery.
func WithReclaim(minIdle, interval time.Duration, batchSize int) Option {
	return func(opts *options) error {
		opts.reclaimMinIdle = minIdle
		opts.reclaimInterval = interval
		opts.reclaimBatchSize = batchSize
		return nil
	}
}

// WithFailureStream configures the bounded stream used to retain failed
// delivery attempts for management inspection.
func WithFailureStream(stream string) Option {
	return func(opts *options) error {
		opts.failureStream = strings.TrimSpace(stream)
		return nil
	}
}

// WithDeadLetter configures terminal delivery handling.
func WithDeadLetter(stream string, maxAttempts int64) Option {
	return func(opts *options) error {
		opts.deadLetterStream = strings.TrimSpace(stream)
		opts.maxDeliveryAttempts = maxAttempts
		return nil
	}
}

// WithReplayDestinations allowlists bounded logical streams for administrative
// replay. Replay remains disabled when this option is absent.
func WithReplayDestinations(destinations ...string) Option {
	return func(opts *options) error {
		if len(destinations) == 0 || len(destinations) > maxReplayDestinations {
			return invalidOption("replay destinations", errors.New("unsafe value"))
		}
		allowed := make(map[string]struct{}, len(destinations))
		for _, destination := range destinations {
			destination = strings.TrimSpace(destination)
			if destination == "" || len(destination) > management.MaxIdentityBytes ||
				strings.ContainsAny(destination, "\x00\r\n") {
				return invalidOption("replay destination", errors.New("unsafe value"))
			}
			if _, duplicate := allowed[destination]; duplicate {
				return invalidOption("replay destination", errors.New("duplicate value"))
			}
			allowed[destination] = struct{}{}
		}
		opts.replayDestinations = allowed
		return nil
	}
}

// WithLogger sets the worker logger.
func WithLogger(logger queue.Logger) Option {
	return func(opts *options) error {
		if logger == nil {
			return invalidOption("logger", errors.New("logger is required"))
		}
		opts.logger = logger
		return nil
	}
}

// WithRunFunc sets the task handler.
func WithRunFunc(run func(context.Context, core.TaskMessage) error) Option {
	return func(opts *options) error {
		if run == nil {
			return invalidOption("run function", errors.New("run function is required"))
		}
		opts.runFunc = run
		return nil
	}
}

func newOptions(option ...Option) (options, error) {
	opts := options{
		clientName:          "go-queue",
		dialTimeout:         5 * time.Second,
		commandTimeout:      5 * time.Second,
		requestTimeout:      6 * time.Second,
		blockTime:           time.Second,
		shutdownTimeout:     10 * time.Second,
		blockingPoolMinSize: 1,
		blockingPoolSize:    8,
		blockingPoolCleanup: time.Minute,
		stream:              "golang-queue",
		group:               "golang-queue",
		consumer:            fmt.Sprintf("go-queue-%d", os.Getpid()),
		maxLength:           10_000,
		readBatchSize:       16,
		reclaimMinIdle:      30 * time.Second,
		reclaimInterval:     5 * time.Second,
		reclaimBatchSize:    16,
		failureStream:       "golang-queue-failures",
		deadLetterStream:    "golang-queue-dead",
		maxDeliveryAttempts: 5,
		logger:              queue.NewLogger(),
		runFunc: func(context.Context, core.TaskMessage) error {
			return nil
		},
	}
	for _, apply := range option {
		if apply == nil {
			return options{}, invalidOption("option", errors.New("option is required"))
		}
		if err := apply(&opts); err != nil {
			return options{}, err
		}
	}
	if err := opts.validate(); err != nil {
		return options{}, err
	}
	return opts, nil
}

func (opts options) validate() error {
	checks := []struct {
		invalid bool
		field   string
	}{
		{opts.address == "" || strings.ContainsAny(opts.address, "@/"), "address"},
		{opts.db < 0, "database"},
		{opts.clientName == "", "client name"},
		{opts.dialTimeout <= 0, "dial timeout"},
		{opts.commandTimeout <= 0, "command timeout"},
		{opts.requestTimeout <= 0, "request timeout"},
		{opts.blockTime <= 0 || opts.blockTime > opts.requestTimeout, "block time"},
		{opts.shutdownTimeout <= 0, "shutdown timeout"},
		{opts.blockingPoolMinSize < 0, "blocking pool minimum"},
		{opts.blockingPoolSize <= 0 || opts.blockingPoolSize > maxBlockingPoolSize, "blocking pool size"},
		{opts.blockingPoolMinSize > opts.blockingPoolSize, "blocking pool minimum"},
		{opts.blockingPoolCleanup <= 0, "blocking pool cleanup"},
		{opts.stream == "", "stream"},
		{opts.group == "", "group"},
		{opts.consumer == "", "consumer"},
		{opts.maxLength <= 0, "maximum stream length"},
		{opts.recordMaxLength < 0, "record retention"},
		{opts.readBatchSize <= 0 || opts.readBatchSize > maxReadBatchSize, "read batch size"},
		{opts.reclaimMinIdle <= 0, "reclaim minimum idle time"},
		{opts.reclaimInterval <= 0, "reclaim interval"},
		{opts.reclaimBatchSize <= 0 || opts.reclaimBatchSize > maxReclaimBatchSize, "reclaim batch size"},
		{opts.failureStream == "" || opts.failureStream == opts.stream || opts.failureStream == opts.deadLetterStream, "failure stream"},
		{opts.deadLetterStream == "" || opts.deadLetterStream == opts.stream, "dead-letter stream"},
		{opts.maxDeliveryAttempts < 2, "maximum delivery attempts"},
	}
	for destination := range opts.replayDestinations {
		if destination == opts.failureStream || destination == opts.deadLetterStream {
			return invalidOption("replay destination", errors.New("conflicts with management stream"))
		}
	}
	for _, check := range checks {
		if check.invalid {
			return invalidOption(check.field, errors.New("unsafe value"))
		}
	}
	return nil
}

func invalidOption(field string, cause error) error {
	return &ConfigurationError{Field: field, Cause: cause}
}
