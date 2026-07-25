// Package relay publishes claimed outbox envelopes with bounded concurrency.
package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/postgres"
)

const (
	defaultBatchSize         = 100
	defaultLeaseDuration     = 30 * time.Second
	defaultMaxAttempts       = 10
	defaultPollInterval      = time.Second
	defaultTransitionTimeout = 5 * time.Second
	maximumBackoff           = time.Minute
	maximumBatchSize         = 1000
	maximumWorkers           = 256
	maximumAttempts          = 10_000
	maximumLeaseDuration     = 24 * time.Hour
)

var (
	ErrStoreRequired      = errors.New("outbox/relay: store is required")
	ErrPublisherRequired  = errors.New("outbox/relay: publisher is required")
	ErrPublisherPanic     = errors.New("outbox/relay: publisher panicked")
	ErrClassifierPanic    = errors.New("outbox/relay: error classifier panicked")
	ErrInvalidErrorClass  = errors.New("outbox/relay: error classifier returned an invalid class")
	ErrBackoffPanic       = errors.New("outbox/relay: backoff function panicked")
	ErrInvalidBackoff     = errors.New("outbox/relay: backoff result is outside bounds")
	ErrHeartbeatPanic     = errors.New("outbox/relay: heartbeat panicked")
	ErrClaimBatchOverflow = errors.New("outbox/relay: store returned more claims than requested")
	ErrOwnerRequired      = errors.New("outbox/relay: owner is required")
	ErrInvalidConfig      = errors.New("outbox/relay: configuration values must be positive")
)

// ErrorClass controls whether a failed publication is retried or terminated.
type ErrorClass uint8

const (
	ErrorTransient ErrorClass = iota
	ErrorPermanent
)

// Publisher accepts an envelope. Returning nil means the publisher accepted
// it, not that exactly-once delivery has been achieved.
type Publisher interface {
	Publish(context.Context, outbox.Envelope) error
}

// Store is the lease-safe persistence contract used by Relay.
type Store interface {
	Ping(context.Context) error
	Claim(context.Context, postgres.ClaimRequest) ([]postgres.Claim, error)
	ExtendLease(context.Context, postgres.LeaseRef, time.Duration) (time.Time, error)
	MarkDelivered(context.Context, postgres.LeaseRef) error
	Retry(context.Context, postgres.LeaseRef, time.Duration, error) error
	DeadLetter(context.Context, postgres.LeaseRef, error) error
	ReleaseLease(context.Context, postgres.LeaseRef) error
}

// Config bounds one relay instance and injects deterministic policy seams.
type Config struct {
	Owner                string
	BatchSize            int
	Workers              int
	LeaseDuration        time.Duration
	LeaseRenewalInterval time.Duration
	MaxAttempts          int
	PollInterval         time.Duration
	TransitionTimeout    time.Duration
	Clock                func() time.Time
	Backoff              func(attempt int) time.Duration
	ClassifyError        func(error) ErrorClass
	Wait                 func(context.Context, time.Duration) error
	Serialization        postgres.SerializationMode
	Observer             outbox.Observer
	Logger               *slog.Logger
	Heartbeat            func(context.Context, time.Duration, func(context.Context) error) error
}

// Result summarizes one bounded polling cycle.
type Result struct {
	Claimed      int
	Published    int
	Delivered    int
	Retried      int
	DeadLettered int
	Released     int
}

// Relay coordinates claims, publisher calls, and state transitions.
type Relay struct {
	store     Store
	publisher Publisher
	config    Config
}

// New validates and constructs a relay.
func New(store Store, publisher Publisher, config Config) (*Relay, error) {
	if store == nil {
		return nil, ErrStoreRequired
	}
	if publisher == nil {
		return nil, ErrPublisherRequired
	}
	if config.Owner == "" {
		return nil, ErrOwnerRequired
	}
	if config.BatchSize == 0 {
		config.BatchSize = defaultBatchSize
	}
	if config.Workers == 0 {
		config.Workers = runtime.NumCPU()
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = defaultLeaseDuration
	}
	if config.LeaseRenewalInterval == 0 {
		config.LeaseRenewalInterval = config.LeaseDuration / 2
		if config.LeaseRenewalInterval == 0 {
			config.LeaseRenewalInterval = config.LeaseDuration
		}
	}
	if config.MaxAttempts == 0 {
		config.MaxAttempts = defaultMaxAttempts
	}
	if config.PollInterval == 0 {
		config.PollInterval = defaultPollInterval
	}
	if config.TransitionTimeout == 0 {
		config.TransitionTimeout = defaultTransitionTimeout
	}
	if config.BatchSize < 0 || config.BatchSize > maximumBatchSize ||
		config.Workers < 0 || config.Workers > maximumWorkers ||
		config.LeaseDuration < 0 || config.LeaseDuration > maximumLeaseDuration ||
		config.LeaseRenewalInterval < 0 ||
		config.LeaseRenewalInterval >= config.LeaseDuration || config.MaxAttempts < 0 ||
		config.MaxAttempts > maximumAttempts ||
		config.PollInterval < 0 || config.TransitionTimeout < 0 {
		return nil, ErrInvalidConfig
	}
	if config.Serialization > postgres.SerializeByTopic {
		return nil, ErrInvalidConfig
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	config.Clock = containClockPanic(config.Clock)
	if config.Backoff == nil {
		config.Backoff = exponentialBackoff
	}
	if config.ClassifyError == nil {
		config.ClassifyError = func(error) ErrorClass { return ErrorTransient }
	}
	if config.Wait == nil {
		config.Wait = waitContext
	}
	if config.Heartbeat == nil {
		config.Heartbeat = maintainLease
	}

	return &Relay{store: store, publisher: publisher, config: config}, nil
}

func containClockPanic(clock func() time.Time) func() time.Time {
	return func() (value time.Time) {
		defer func() { _ = recover() }()

		return clock()
	}
}

// Run polls until cancellation. Full batches are followed immediately to
// drain backlog; partial batches wait through the injected polling function.
func (relay *Relay) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		result, err := relay.RunOnce(ctx)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		if result.Claimed == relay.config.BatchSize {
			continue
		}
		if err := relay.config.Wait(ctx, relay.config.PollInterval); err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return fmt.Errorf("outbox/relay: wait for poll: %w", err)
		}
	}
}

// RunOnce claims at most one batch and drains it with bounded worker
// concurrency. Publisher failures that are durably scheduled are reflected in
// Result; persistence failures are returned.
func (relay *Relay) RunOnce(ctx context.Context) (Result, error) {
	startedAt := relay.config.Clock()
	claims, err := relay.store.Claim(ctx, postgres.ClaimRequest{
		Owner:         relay.config.Owner,
		Limit:         relay.config.BatchSize,
		LeaseDuration: relay.config.LeaseDuration,
		Serialization: relay.config.Serialization,
	})
	if err != nil {
		relay.observe(ctx, outbox.Event{Operation: outbox.OperationClaim, Outcome: outbox.OutcomeFailure,
			Duration: relay.durationSince(startedAt)})
		return Result{}, fmt.Errorf("outbox/relay: claim: %w", err)
	}
	relay.observe(ctx, outbox.Event{Operation: outbox.OperationClaim, Outcome: outbox.OutcomeSuccess,
		Count: len(claims), Duration: relay.durationSince(startedAt)})

	result := Result{Claimed: len(claims)}
	if len(claims) > relay.config.BatchSize {
		return result, fmt.Errorf("%w: got %d, requested %d",
			ErrClaimBatchOverflow, len(claims), relay.config.BatchSize)
	}
	jobs := make(chan postgres.Claim, len(claims))
	for _, claim := range claims {
		jobs <- claim
	}
	close(jobs)

	workerCount := min(relay.config.Workers, len(claims))
	var wait sync.WaitGroup
	var lock sync.Mutex
	var transitionErrors []error
	for range workerCount {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for claim := range jobs {
				published, transition, transitionErr := relay.process(ctx, claim)
				lock.Lock()
				if published {
					result.Published++
				}
				switch transition {
				case transitionDelivered:
					result.Delivered++
				case transitionRetried:
					result.Retried++
				case transitionDeadLettered:
					result.DeadLettered++
				case transitionReleased:
					result.Released++
				}
				if transitionErr != nil {
					transitionErrors = append(transitionErrors, transitionErr)
				}
				lock.Unlock()
			}
		}()
	}
	wait.Wait()

	return result, errors.Join(transitionErrors...)
}

type transition uint8

const (
	transitionNone transition = iota
	transitionDelivered
	transitionRetried
	transitionDeadLettered
	transitionReleased
)

func (relay *Relay) process(ctx context.Context, claim postgres.Claim) (bool, transition, error) {
	lease := postgres.LeaseRef{ID: claim.Envelope.ID, Token: claim.LeaseToken}
	publishContext, cancelPublish := context.WithCancel(ctx)
	var publishFinished atomic.Bool
	type heartbeatResult struct {
		err                  error
		afterPublishFinished bool
	}
	heartbeatDone := make(chan heartbeatResult, 1)
	go func() {
		heartbeatErr := invokeHeartbeat(
			relay.config.Heartbeat,
			publishContext,
			relay.config.LeaseRenewalInterval,
			func(heartbeatContext context.Context) error {
				startedAt := relay.config.Clock()
				_, err := relay.store.ExtendLease(heartbeatContext, lease, relay.config.LeaseDuration)
				outcome := outbox.OutcomeSuccess
				if err != nil {
					outcome = outbox.OutcomeFailure
				}
				relay.observeEnvelope(heartbeatContext, claim.Envelope, outbox.OperationExtendLease, outcome, startedAt)

				return err
			},
		)
		afterPublishFinished := publishFinished.Load()
		if heartbeatErr != nil && !afterPublishFinished {
			cancelPublish()
		}
		heartbeatDone <- heartbeatResult{
			err:                  heartbeatErr,
			afterPublishFinished: afterPublishFinished,
		}
	}()
	publishStartedAt := relay.config.Clock()
	publishErr := relay.publish(publishContext, claim.Envelope)
	publishFinished.Store(true)
	cancelPublish()
	heartbeat := <-heartbeatDone
	expectedHeartbeatStop := heartbeat.afterPublishFinished && errors.Is(heartbeat.err, context.Canceled)
	if ctx.Err() != nil && errors.Is(heartbeat.err, ctx.Err()) {
		expectedHeartbeatStop = true
	}
	if heartbeat.err != nil && !expectedHeartbeatStop {
		relay.observeEnvelope(ctx, claim.Envelope, outbox.OperationPublish, outbox.OutcomeFailure, publishStartedAt)

		return false, transitionNone, fmt.Errorf("outbox/relay: renew lease for %q: %w", claim.Envelope.ID, heartbeat.err)
	}
	if publishErr == nil {
		relay.observeEnvelope(ctx, claim.Envelope, outbox.OperationPublish, outbox.OutcomeSuccess, publishStartedAt)
		transitionStartedAt := relay.config.Clock()
		if err := relay.store.MarkDelivered(ctx, lease); err != nil {
			relay.observeEnvelope(ctx, claim.Envelope, outbox.OperationDeliver, outbox.OutcomeFailure, transitionStartedAt)
			return true, transitionNone, fmt.Errorf("outbox/relay: mark %q delivered: %w", claim.Envelope.ID, err)
		}
		relay.observeEnvelope(ctx, claim.Envelope, outbox.OperationDeliver, outbox.OutcomeSuccess, transitionStartedAt)

		return true, transitionDelivered, nil
	}
	relay.observeEnvelope(ctx, claim.Envelope, outbox.OperationPublish, outbox.OutcomeFailure, publishStartedAt)
	if ctx.Err() != nil && (errors.Is(publishErr, context.Canceled) || errors.Is(publishErr, context.DeadlineExceeded)) {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), relay.config.TransitionTimeout)
		defer cancel()
		transitionStartedAt := relay.config.Clock()
		if err := relay.store.ReleaseLease(cleanupContext, lease); err != nil {
			relay.observeEnvelope(cleanupContext, claim.Envelope, outbox.OperationRelease, outbox.OutcomeFailure, transitionStartedAt)
			return false, transitionNone, fmt.Errorf("outbox/relay: release %q: %w", claim.Envelope.ID, err)
		}
		relay.observeEnvelope(cleanupContext, claim.Envelope, outbox.OperationRelease, outbox.OutcomeSuccess, transitionStartedAt)

		return false, transitionReleased, nil
	}

	if claim.Envelope.Attempts >= relay.config.MaxAttempts {
		transitionStartedAt := relay.config.Clock()
		if err := relay.store.DeadLetter(ctx, lease, publishErr); err != nil {
			relay.observeEnvelope(ctx, claim.Envelope, outbox.OperationDeadLetter, outbox.OutcomeFailure, transitionStartedAt)
			return false, transitionNone, fmt.Errorf("outbox/relay: dead letter %q: %w", claim.Envelope.ID, err)
		}
		relay.observeEnvelope(ctx, claim.Envelope, outbox.OperationDeadLetter, outbox.OutcomeSuccess, transitionStartedAt)

		return false, transitionDeadLettered, nil
	}
	errorClass, policyErr := classifyError(relay.config.ClassifyError, publishErr)
	if errorClass == ErrorPermanent {
		transitionStartedAt := relay.config.Clock()
		if err := relay.store.DeadLetter(ctx, lease, publishErr); err != nil {
			relay.observeEnvelope(ctx, claim.Envelope, outbox.OperationDeadLetter, outbox.OutcomeFailure, transitionStartedAt)
			return false, transitionNone, fmt.Errorf("outbox/relay: dead letter %q: %w", claim.Envelope.ID, err)
		}
		relay.observeEnvelope(ctx, claim.Envelope, outbox.OperationDeadLetter, outbox.OutcomeSuccess, transitionStartedAt)

		return false, transitionDeadLettered, nil
	}

	retryDelay, backoffErr := boundedBackoff(relay.config.Backoff, claim.Envelope.Attempts)
	policyErr = errors.Join(policyErr, backoffErr)
	transitionStartedAt := relay.config.Clock()
	if err := relay.store.Retry(ctx, lease, retryDelay, publishErr); err != nil {
		relay.observeEnvelope(ctx, claim.Envelope, outbox.OperationRetry, outbox.OutcomeFailure, transitionStartedAt)
		return false, transitionNone, fmt.Errorf("outbox/relay: retry %q: %w", claim.Envelope.ID, err)
	}
	relay.observeEnvelope(ctx, claim.Envelope, outbox.OperationRetry, outbox.OutcomeSuccess, transitionStartedAt)
	if policyErr != nil {
		return false, transitionRetried, fmt.Errorf("outbox/relay: apply failure policy for %q: %w", claim.Envelope.ID, policyErr)
	}

	return false, transitionRetried, nil
}

func classifyError(classifier func(error) ErrorClass, cause error) (class ErrorClass, err error) {
	defer func() {
		if recover() != nil {
			class = ErrorTransient
			err = ErrClassifierPanic
		}
	}()

	class = classifier(cause)
	if class != ErrorTransient && class != ErrorPermanent {
		return ErrorTransient, ErrInvalidErrorClass
	}

	return class, nil
}

func boundedBackoff(backoff func(int) time.Duration, attempt int) (delay time.Duration, err error) {
	defer func() {
		if recover() != nil {
			delay = 0
			err = ErrBackoffPanic
		}
	}()

	delay = backoff(attempt)
	if delay < 0 {
		return 0, ErrInvalidBackoff
	}
	if delay > maximumBackoff {
		return maximumBackoff, ErrInvalidBackoff
	}

	return delay, nil
}

func maintainLease(ctx context.Context, interval time.Duration, extend func(context.Context) error) error {
	for {
		if err := waitContext(ctx, interval); err != nil {
			return nil
		}
		if err := extend(ctx); err != nil {
			return err
		}
	}
}

func invokeHeartbeat(
	heartbeat func(context.Context, time.Duration, func(context.Context) error) error,
	ctx context.Context,
	interval time.Duration,
	extend func(context.Context) error,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrHeartbeatPanic
		}
	}()

	return heartbeat(ctx, interval, extend)
}

func (relay *Relay) publish(ctx context.Context, envelope outbox.Envelope) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrPublisherPanic
		}
	}()

	return relay.publisher.Publish(ctx, envelope)
}

// Readiness verifies database connectivity and, when supported, publisher
// connectivity. All failed checks are returned together.
func (relay *Relay) Readiness(ctx context.Context) error {
	var readinessErrors []error
	if err := relay.store.Ping(ctx); err != nil {
		readinessErrors = append(readinessErrors, fmt.Errorf("outbox/relay: database readiness: %w", err))
	}
	if publisher, ok := relay.publisher.(interface{ Health(context.Context) error }); ok {
		if err := publisher.Health(ctx); err != nil {
			readinessErrors = append(readinessErrors, fmt.Errorf("outbox/relay: publisher readiness: %w", err))
		}
	}

	return errors.Join(readinessErrors...)
}

func (relay *Relay) observeEnvelope(
	ctx context.Context,
	envelope outbox.Envelope,
	operation outbox.Operation,
	outcome outbox.Outcome,
	startedAt time.Time,
) {
	relay.observe(ctx, outbox.Event{
		Operation: operation,
		Outcome:   outcome,
		Count:     1,
		MessageID: envelope.ID,
		Topic:     envelope.Topic,
		Attempts:  envelope.Attempts,
		Duration:  relay.durationSince(startedAt),
	})
}

func (relay *Relay) observe(ctx context.Context, event outbox.Event) {
	if relay.config.Observer != nil {
		containDiagnosticPanic(func() {
			relay.config.Observer.Observe(ctx, event)
		})
	}
	if relay.config.Logger != nil {
		containDiagnosticPanic(func() {
			relay.config.Logger.LogAttrs(ctx, slog.LevelInfo, "outbox operation",
				slog.String("operation", string(event.Operation)),
				slog.String("outcome", string(event.Outcome)),
				slog.Int("count", event.Count),
				slog.String("message_id", event.MessageID),
				slog.String("topic", event.Topic),
				slog.Int("attempts", event.Attempts),
				slog.Duration("duration", event.Duration),
			)
		})
	}
}

func containDiagnosticPanic(callback func()) {
	defer func() { _ = recover() }()
	callback()
}

func (relay *Relay) durationSince(startedAt time.Time) time.Duration {
	duration := relay.config.Clock().Sub(startedAt)
	if duration < 0 {
		return 0
	}

	return duration
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func exponentialBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	exponent := min(attempt-1, 10)
	ceiling := 100 * time.Millisecond * time.Duration(1<<exponent)
	if ceiling > maximumBackoff {
		ceiling = maximumBackoff
	}

	return time.Duration(rand.Int64N(int64(ceiling) + 1))
}
