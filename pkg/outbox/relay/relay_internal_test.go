package relay

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/postgres"
)

func TestExponentialBackoffIsBounded(t *testing.T) {
	t.Parallel()

	for _, attempt := range []int{-1, 0, 1, 5, 100} {
		delay := exponentialBackoff(attempt)
		if delay < 0 || delay > maximumBackoff {
			t.Fatalf("attempt %d delay = %s", attempt, delay)
		}
	}
	if maximumBackoff != time.Minute {
		t.Fatalf("maximum backoff = %s", maximumBackoff)
	}
}

func TestWaitContextHandlesTimerAndCancellation(t *testing.T) {
	t.Parallel()

	if err := waitContext(context.Background(), time.Nanosecond); err != nil {
		t.Fatalf("timer wait: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait error = %v", err)
	}
}

func TestNewAppliesEveryRuntimeDefault(t *testing.T) {
	relay, err := New(&internalStore{}, internalPublisher{}, Config{Owner: "relay"})
	if err != nil {
		t.Fatalf("create relay: %v", err)
	}
	if relay.config.BatchSize != defaultBatchSize || relay.config.Workers != runtime.NumCPU() ||
		relay.config.LeaseDuration != defaultLeaseDuration ||
		relay.config.LeaseRenewalInterval != defaultLeaseDuration/2 ||
		relay.config.MaxAttempts != defaultMaxAttempts ||
		relay.config.PollInterval != defaultPollInterval ||
		relay.config.TransitionTimeout != 5*time.Second {
		t.Fatalf("default config = %#v", relay.config)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := relay.config.Wait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("default wait error = %v", err)
	}
	if err := relay.config.Heartbeat(ctx, time.Hour, func(context.Context) error {
		t.Fatal("default heartbeat extended after cancellation")
		return nil
	}); err != nil {
		t.Fatalf("default heartbeat error = %v", err)
	}
}

func TestInvalidConfigExactBoundaries(t *testing.T) {
	valid := Config{
		BatchSize: maximumBatchSize, Workers: maximumWorkers,
		LeaseDuration: maximumLeaseDuration, LeaseRenewalInterval: maximumLeaseDuration - 1,
		MaxAttempts: maximumAttempts, PollInterval: 1, TransitionTimeout: 1,
	}
	if invalidConfig(valid) {
		t.Fatalf("exact maximum config rejected: %#v", valid)
	}
	for name, config := range map[string]Config{
		"batch":      {BatchSize: 0, Workers: 1, LeaseDuration: 2, LeaseRenewalInterval: 1, MaxAttempts: 1},
		"workers":    {BatchSize: 1, Workers: 0, LeaseDuration: 2, LeaseRenewalInterval: 1, MaxAttempts: 1},
		"renewal":    {BatchSize: 1, Workers: 1, LeaseDuration: 1, LeaseRenewalInterval: 0, MaxAttempts: 1},
		"attempts":   {BatchSize: 1, Workers: 1, LeaseDuration: 2, LeaseRenewalInterval: 1, MaxAttempts: 0},
		"poll":       {BatchSize: 1, Workers: 1, LeaseDuration: 2, LeaseRenewalInterval: 1, MaxAttempts: 1, PollInterval: 0},
		"transition": {BatchSize: 1, Workers: 1, LeaseDuration: 2, LeaseRenewalInterval: 1, MaxAttempts: 1, TransitionTimeout: 0},
	} {
		if invalidConfig(config) {
			t.Fatalf("exact lower %s config rejected: %#v", name, config)
		}
	}
	tests := []func(*Config){
		func(config *Config) { config.BatchSize = -1 },
		func(config *Config) { config.BatchSize = maximumBatchSize + 1 },
		func(config *Config) { config.Workers = -1 },
		func(config *Config) { config.Workers = maximumWorkers + 1 },
		func(config *Config) { config.LeaseDuration = -1 },
		func(config *Config) { config.LeaseDuration = maximumLeaseDuration + 1 },
		func(config *Config) { config.LeaseRenewalInterval = -1 },
		func(config *Config) { config.LeaseRenewalInterval = config.LeaseDuration },
		func(config *Config) { config.MaxAttempts = -1 },
		func(config *Config) { config.MaxAttempts = maximumAttempts + 1 },
		func(config *Config) { config.PollInterval = -1 },
		func(config *Config) { config.TransitionTimeout = -1 },
	}
	for index, mutate := range tests {
		config := valid
		mutate(&config)
		if !invalidConfig(config) {
			t.Fatalf("invalid config %d accepted: %#v", index, config)
		}
	}
}

func TestBoundedBackoffExactLimits(t *testing.T) {
	for name, delay := range map[string]time.Duration{
		"zero": 0,
		"max":  maximumBackoff,
	} {
		got, err := boundedBackoff(func(int) time.Duration { return delay }, 1)
		if err != nil || got != delay {
			t.Fatalf("%s delay/error = %s/%v", name, got, err)
		}
	}
	if delay, err := boundedBackoff(func(int) time.Duration { return maximumBackoff + 1 }, 1); !errors.Is(err, ErrInvalidBackoff) || delay != maximumBackoff {
		t.Fatalf("overflow delay/error = %s/%v", delay, err)
	}
}

func TestBackoffCeilingUsesExactExponentialAndCap(t *testing.T) {
	for attempt, want := range map[int]time.Duration{
		-1: 100 * time.Millisecond,
		0:  100 * time.Millisecond,
		1:  100 * time.Millisecond,
		2:  200 * time.Millisecond,
		11: 60 * time.Second,
		12: 60 * time.Second,
	} {
		if got := backoffCeiling(attempt); got != want {
			t.Fatalf("attempt %d ceiling = %s, want %s", attempt, got, want)
		}
	}
}

func TestHeartbeatStopClassification(t *testing.T) {
	other := errors.New("other")
	tests := []struct {
		parent    error
		heartbeat error
		after     bool
		want      bool
	}{
		{heartbeat: context.Canceled, after: true, want: true},
		{heartbeat: context.Canceled, after: false, want: false},
		{parent: context.DeadlineExceeded, heartbeat: context.DeadlineExceeded, want: true},
		{parent: context.Canceled, heartbeat: other, after: true, want: false},
		{heartbeat: other, after: false, want: false},
	}
	for index, test := range tests {
		if got := heartbeatStopExpected(test.parent, test.heartbeat, test.after); got != test.want {
			t.Fatalf("case %d = %t, want %t", index, got, test.want)
		}
	}
}

func TestHeartbeatPublishCancellationClassification(t *testing.T) {
	want := errors.New("heartbeat failed")
	for index, test := range []struct {
		err   error
		after bool
		want  bool
	}{
		{err: nil, after: false, want: false},
		{err: nil, after: true, want: false},
		{err: want, after: false, want: true},
		{err: want, after: true, want: false},
	} {
		if got := heartbeatShouldCancelPublish(test.err, test.after); got != test.want {
			t.Fatalf("case %d = %t, want %t", index, got, test.want)
		}
	}
}

func TestCanceledPublishClassification(t *testing.T) {
	other := errors.New("other")
	for index, test := range []struct {
		parent  error
		publish error
		want    bool
	}{
		{parent: context.Canceled, publish: context.Canceled, want: true},
		{parent: context.Canceled, publish: context.DeadlineExceeded, want: true},
		{parent: nil, publish: context.Canceled, want: false},
		{parent: context.Canceled, publish: other, want: false},
	} {
		if got := canceledPublish(test.parent, test.publish); got != test.want {
			t.Fatalf("case %d = %t, want %t", index, got, test.want)
		}
	}
}

func TestMaintainLeaseExtendsUntilCancellationOrFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("extension failed")
	if err := maintainLease(context.Background(), time.Nanosecond, func(context.Context) error {
		return want
	}); !errors.Is(err, want) {
		t.Fatalf("extension error = %v, want %v", err, want)
	}

	extensions := 0
	if err := maintainLease(context.Background(), time.Nanosecond, func(context.Context) error {
		extensions++
		if extensions == 1 {
			return nil
		}

		return want
	}); !errors.Is(err, want) {
		t.Fatalf("second extension error = %v, want %v", err, want)
	}
	if extensions != 2 {
		t.Fatalf("extensions = %d, want 2", extensions)
	}
}

type internalStore struct{}

func (*internalStore) Ping(context.Context) error { return nil }

func (*internalStore) Claim(context.Context, postgres.ClaimRequest) ([]postgres.Claim, error) {
	return nil, nil
}

func (*internalStore) MarkDelivered(context.Context, postgres.LeaseRef) error { return nil }

func (*internalStore) ExtendLease(context.Context, postgres.LeaseRef, time.Duration) (time.Time, error) {
	return time.Time{}, nil
}

func (*internalStore) Retry(context.Context, postgres.LeaseRef, time.Duration, error) error {
	return nil
}

func (*internalStore) DeadLetter(context.Context, postgres.LeaseRef, error) error { return nil }

func (*internalStore) ReleaseLease(context.Context, postgres.LeaseRef) error { return nil }

type internalPublisher struct{}

func (internalPublisher) Publish(context.Context, outbox.Envelope) error { return nil }

func (internalPublisher) Health(context.Context) error { return nil }
