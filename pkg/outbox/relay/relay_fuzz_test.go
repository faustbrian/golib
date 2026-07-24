package relay_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/faustbrian/golib/pkg/outbox/relay"
)

func FuzzRelayOptions(f *testing.F) {
	f.Add("fuzz", int16(1), int16(1), int16(3), int64(time.Second),
		int64(time.Second/2), int64(time.Millisecond), int64(time.Second), byte(0))
	f.Add("", int16(-1), int16(0), int16(-1), int64(-1),
		int64(-1), int64(-1), int64(-1), byte(255))

	f.Fuzz(func(
		t *testing.T,
		owner string,
		batch, workers, attempts int16,
		lease, renewal, poll, transition int64,
		serialization byte,
	) {
		worker, err := relay.New(&recordingStore{}, &recordingPublisher{}, relay.Config{
			Owner: owner, BatchSize: int(batch), Workers: int(workers),
			MaxAttempts: int(attempts), LeaseDuration: time.Duration(lease),
			LeaseRenewalInterval: time.Duration(renewal), PollInterval: time.Duration(poll),
			TransitionTimeout: time.Duration(transition),
			Serialization:     postgres.SerializationMode(serialization),
		})
		if err != nil {
			return
		}
		if _, err := worker.RunOnce(context.Background()); err != nil {
			t.Fatalf("run once: %v", err)
		}
	})
}

func FuzzPublisherFailures(f *testing.F) {
	f.Add("timeout after acceptance", byte(0), int16(1), int64(time.Second))
	f.Add("invalid routing key", byte(1), int16(1), int64(0))
	f.Add(string([]byte{0xff, 0x00, 0xfe}), byte(255), int16(3), int64(-1))

	f.Fuzz(func(t *testing.T, failure string, class byte, attemptsInput int16, backoffNanos int64) {
		attempts := int(uint16(attemptsInput)%3) + 1
		publishErr := fmt.Errorf("publisher failure digest=%x", []byte(failure))
		store := &recordingStore{claims: []postgres.Claim{claim("publisher-fuzz", attempts)}}
		worker, createErr := relay.New(store, &recordingPublisher{err: publishErr}, relay.Config{
			Owner: "publisher-fuzz", MaxAttempts: 3,
			ClassifyError: func(error) relay.ErrorClass {
				switch class % 3 {
				case 0:
					return relay.ErrorTransient
				case 1:
					return relay.ErrorPermanent
				default:
					return relay.ErrorClass(255)
				}
			},
			Backoff: func(int) time.Duration { return time.Duration(backoffNanos) },
		})
		if createErr != nil {
			t.Fatalf("create relay: %v", createErr)
		}

		result, runErr := worker.RunOnce(context.Background())
		if runErr != nil && strings.Contains(runErr.Error(), "digest=") {
			t.Fatalf("returned publisher details: %v", runErr)
		}
		if attempts == 3 || class%3 == 1 {
			if runErr != nil || result.DeadLettered != 1 || len(store.dead) != 1 {
				t.Fatalf("dead-letter result/store/error = %#v/%#v/%v", result, store, runErr)
			}

			return
		}
		if result.Retried != 1 || len(store.retried) != 1 ||
			!errors.Is(store.retried[0].cause, publishErr) {
			t.Fatalf("retry result/store/error = %#v/%#v/%v", result, store, runErr)
		}
		if class%3 == 2 && !errors.Is(runErr, relay.ErrInvalidErrorClass) {
			t.Fatalf("invalid classifier error = %v", runErr)
		}
	})
}

func BenchmarkRelayRunOnce1000(b *testing.B) {
	b.ReportAllocs()
	claims := make([]postgres.Claim, 1000)
	for index := range claims {
		claims[index] = claim("benchmark", 1)
	}

	for b.Loop() {
		store := &recordingStore{claims: claims}
		worker, err := relay.New(store, &recordingPublisher{}, relay.Config{
			Owner: "benchmark", BatchSize: len(claims), Workers: 8,
		})
		if err != nil {
			b.Fatalf("create relay: %v", err)
		}
		if _, err := worker.RunOnce(context.Background()); err != nil {
			b.Fatalf("run once: %v", err)
		}
	}
}
