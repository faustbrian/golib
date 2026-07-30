package kafka

import (
	"context"
	"errors"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestBrokerObserverReportsCopiedConnectionMetadata(t *testing.T) {

	var got Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				got = observation

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	finishedAt := time.Unix(1_700_000_000, 0)
	hook := newFranzObserverHook(
		"producer-client",
		"",
		newObserverDispatcher(policy),
	)
	hook.now = func() time.Time {
		return finishedAt
	}

	hook.OnBrokerConnect(
		kgo.BrokerMetadata{
			NodeID: 7,
			Host:   "credentials-must-not-be-copied.internal",
			Port:   9092,
		},
		250*time.Millisecond,
		nil,
		nil,
	)

	if got.Kind != ObservationBrokerConnect ||
		got.ClientID != "producer-client" ||
		got.GroupID != "" ||
		got.BrokerID != 7 ||
		!got.BrokerKnown ||
		!got.StartedAt.Equal(finishedAt.Add(-250*time.Millisecond)) ||
		got.Duration != 250*time.Millisecond ||
		!got.Succeeded ||
		got.Category != ErrorUnknown {
		t.Fatalf("broker connection observation = %#v", got)
	}
}

func TestBrokerObserverReportsRequestLatencyAndRedactedFailure(t *testing.T) {

	var got Observation
	hook := newTestFranzObserverHook(t, "consumer-client", "projection-group", &got)
	finishedAt := time.Unix(1_700_000_100, 0)
	hook.now = func() time.Time {
		return finishedAt
	}

	hook.OnBrokerE2E(
		kgo.BrokerMetadata{NodeID: -1, Host: "do-not-export.internal"},
		1,
		kgo.BrokerE2E{
			BytesWritten: 1_024,
			BytesRead:    2_048,
			WriteWait:    20 * time.Millisecond,
			TimeToWrite:  5 * time.Millisecond,
			ReadWait:     10 * time.Millisecond,
			TimeToRead:   15 * time.Millisecond,
			ReadErr:      kerr.BrokerNotAvailable,
		},
	)

	if got.Kind != ObservationBrokerRequest ||
		got.ClientID != "consumer-client" ||
		got.GroupID != "projection-group" ||
		got.BrokerID != 0 ||
		got.BrokerKnown ||
		got.APIKey != 1 ||
		!got.APIKeyKnown ||
		got.RequestBytes != 1_024 ||
		got.ResponseBytes != 2_048 ||
		got.QueueDuration != 20*time.Millisecond ||
		got.Duration != 50*time.Millisecond ||
		!got.StartedAt.Equal(finishedAt.Add(-50*time.Millisecond)) ||
		got.Succeeded ||
		got.Category != ErrorRetryable {
		t.Fatalf("broker request observation = %#v", got)
	}
}

func TestBrokerObserverReportsThrottleAndDisconnect(t *testing.T) {

	var got []Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				got = append(got, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	observedAt := time.Unix(1_700_000_200, 0)
	hook := newFranzObserverHook(
		"consumer-client",
		"projection-group",
		newObserverDispatcher(policy),
	)
	hook.now = func() time.Time {
		return observedAt
	}
	meta := kgo.BrokerMetadata{NodeID: 9, Host: "do-not-export.internal"}

	hook.OnBrokerThrottle(meta, 175*time.Millisecond, true)
	hook.OnBrokerDisconnect(meta, nil)

	if len(got) != 2 {
		t.Fatalf("broker observations = %d, want 2", len(got))
	}
	if got[0].Kind != ObservationBrokerThrottle ||
		got[0].BrokerID != 9 ||
		!got[0].BrokerKnown ||
		got[0].ThrottleDuration != 175*time.Millisecond ||
		!got[0].ThrottledAfterResponse ||
		!got[0].StartedAt.Equal(observedAt) ||
		!got[0].Succeeded {
		t.Fatalf("broker throttle observation = %#v", got[0])
	}
	if got[1].Kind != ObservationBrokerDisconnect ||
		got[1].BrokerID != 9 ||
		!got[1].BrokerKnown ||
		!got[1].StartedAt.Equal(observedAt) ||
		!got[1].Succeeded {
		t.Fatalf("broker disconnect observation = %#v", got[1])
	}
}

func TestConsumerGroupObserverReportsRedactedManagementError(t *testing.T) {

	var got Observation
	hook := newTestFranzObserverHook(
		t,
		"consumer-client",
		"projection-group",
		&got,
	)
	observedAt := time.Unix(1_700_000_250, 0)
	hook.now = func() time.Time {
		return observedAt
	}

	hook.OnGroupManageError(kerr.GroupAuthorizationFailed)

	if got.Kind != ObservationConsumeGroupError ||
		got.ClientID != "consumer-client" ||
		got.GroupID != "projection-group" ||
		!got.StartedAt.Equal(observedAt) ||
		got.Duration != 0 ||
		got.Succeeded ||
		got.Category != ErrorAuthorization {
		t.Fatalf("group management observation = %#v", got)
	}
}

func TestBrokerObserverClipsInvalidHookMetadata(t *testing.T) {

	var got []Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				got = append(got, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	observedAt := time.Unix(1_700_000_300, 0)
	hook := newFranzObserverHook(
		"bounded-client",
		"",
		newObserverDispatcher(policy),
	)
	hook.now = func() time.Time {
		return observedAt
	}
	unknownBroker := kgo.BrokerMetadata{NodeID: -1}

	hook.OnBrokerConnect(
		unknownBroker,
		-time.Millisecond,
		nil,
		kerr.SaslAuthenticationFailed,
	)
	hook.OnBrokerE2E(unknownBroker, -1, kgo.BrokerE2E{
		BytesWritten: -1,
		BytesRead:    -2,
		WriteWait:    time.Duration(math.MaxInt64),
		TimeToWrite:  time.Nanosecond,
	})
	hook.OnBrokerE2E(unknownBroker, -1, kgo.BrokerE2E{
		ReadWait: -time.Nanosecond,
	})
	hook.OnBrokerThrottle(unknownBroker, -time.Millisecond, false)

	if len(got) != 4 {
		t.Fatalf("broker observations = %d, want 4", len(got))
	}
	if got[0].BrokerKnown ||
		got[0].BrokerID != 0 ||
		got[0].Duration != 0 ||
		!got[0].StartedAt.Equal(observedAt) ||
		got[0].Succeeded ||
		got[0].Category != ErrorAuthorization ||
		!got[0].Truncated {
		t.Fatalf("bounded connection observation = %#v", got[0])
	}
	if got[1].APIKeyKnown ||
		got[1].APIKey != 0 ||
		got[1].RequestBytes != 0 ||
		got[1].ResponseBytes != 0 ||
		got[1].QueueDuration != time.Duration(math.MaxInt64) ||
		got[1].Duration != time.Duration(math.MaxInt64) ||
		!got[1].Truncated {
		t.Fatalf("bounded request observation = %#v", got[1])
	}
	if got[2].Duration != 0 || !got[2].Truncated {
		t.Fatalf("negative request duration observation = %#v", got[2])
	}
	if got[3].ThrottleDuration != 0 || !got[3].Truncated {
		t.Fatalf("bounded throttle observation = %#v", got[3])
	}
}

func TestBrokerObserverHelpersAcceptExactZeroAndMaximumBoundaries(t *testing.T) {

	if brokerID, known := observedBrokerID(0); brokerID != 0 || !known {
		t.Fatalf("observedBrokerID(0) = %d/%t", brokerID, known)
	}
	if apiKey, known := observedAPIKey(0); apiKey != 0 || !known {
		t.Fatalf("observedAPIKey(0) = %d/%t", apiKey, known)
	}
	if duration, truncated := observedDuration(0); duration != 0 || truncated {
		t.Fatalf("observedDuration(0) = %s/%t", duration, truncated)
	}
	if bytes, truncated := observedBytes(0); bytes != 0 || truncated {
		t.Fatalf("observedBytes(0) = %d/%t", bytes, truncated)
	}
	duration, truncated := observedRequestDuration(kgo.BrokerE2E{
		WriteWait:   0,
		TimeToWrite: time.Duration(math.MaxInt64 - 3),
		ReadWait:    1,
		TimeToRead:  2,
	})
	if duration != time.Duration(math.MaxInt64) || truncated {
		t.Fatalf("exact maximum request duration = %s/%t", duration, truncated)
	}
}

func TestProducerWiresBrokerObserversIntoFranzClient(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = connection.Close()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-serverDone
	})

	observed := make(chan Observation, 16)
	reentryResult := make(chan error, 1)
	var firstObservation sync.Once
	var producer *Producer
	producer, err = NewProducer(ProducerConfig{
		Brokers:       []string{listener.Addr().String()},
		ClientID:      "wired-producer",
		AllowedTopics: []string{"events"},
		DialTimeout:   100 * time.Millisecond,
		Security:      DevelopmentPlaintextSecurity(),
		Observers: ObserverPolicy{
			Observers: []ObserverFunc{
				func(_ context.Context, observation Observation) error {
					if observation.Kind == ObservationBrokerConnect {
						firstObservation.Do(func() {
							reentryResult <- producer.Close()
							observed <- observation
						})
					}

					return nil
				},
			},
			FailureHandler: func(context.Context, ObservationFailure) {},
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := producer.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	if healthErr := producer.Health(ctx); healthErr == nil {
		t.Fatal("Health() error = nil, want broker initialization failure")
	}

	select {
	case observation := <-observed:
		if observation.Kind != ObservationBrokerConnect ||
			observation.ClientID != "wired-producer" {
			t.Fatalf("wired broker observation = %#v", observation)
		}
	default:
		t.Fatal("broker connection was not observed")
	}
	if reentryErr := <-reentryResult; !errors.Is(reentryErr, ErrObserverReentry) {
		t.Fatalf("observer Close() error = %v, want %v", reentryErr, ErrObserverReentry)
	}
}

func TestConsumerWiresBrokerObserversWithGroupIdentity(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = connection.Close()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-serverDone
	})

	observed := make(chan Observation, 1)
	reentryResult := make(chan error, 1)
	var firstConnection sync.Once
	var consumer *Consumer
	consumer, err = NewConsumer(ConsumerConfig{
		Brokers:         []string{listener.Addr().String()},
		ClientID:        "wired-consumer",
		GroupID:         "projection-group",
		Topics:          []string{"events"},
		ResetOffset:     OffsetEarliest,
		DialTimeout:     100 * time.Millisecond,
		ShutdownTimeout: 250 * time.Millisecond,
		Security:        DevelopmentPlaintextSecurity(),
		Observers: ObserverPolicy{
			Observers: []ObserverFunc{
				func(_ context.Context, observation Observation) error {
					if observation.Kind == ObservationBrokerConnect {
						firstConnection.Do(func() {
							reentryResult <- consumer.Close()
							observed <- observation
						})
					}

					return nil
				},
			},
			FailureHandler: func(context.Context, ObservationFailure) {},
		},
	})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := consumer.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	_, runErr := consumer.RunOnce(ctx, HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		return nil
	}))
	if runErr == nil {
		t.Fatal("RunOnce() error = nil, want broker initialization failure")
	}

	select {
	case observation := <-observed:
		if observation.ClientID != "wired-consumer" ||
			observation.GroupID != "projection-group" {
			t.Fatalf("wired consumer observation = %#v", observation)
		}
	default:
		t.Fatal("consumer broker connection was not observed")
	}
	if reentryErr := <-reentryResult; !errors.Is(reentryErr, ErrObserverReentry) {
		t.Fatalf("observer Close() error = %v, want %v", reentryErr, ErrObserverReentry)
	}
}

func newTestFranzObserverHook(
	t *testing.T,
	clientID string,
	groupID string,
	got *Observation,
) *franzObserverHook {
	t.Helper()

	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				*got = observation

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}

	return newFranzObserverHook(
		clientID,
		groupID,
		newObserverDispatcher(policy),
	)
}
