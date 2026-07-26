package memory_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

const (
	memoryBenchmarkWriters          = 8
	memoryBenchmarkAppendsPerWriter = 25
)

var memoryBenchmarkStore *memory.Store

func TestConcurrentAppendPerformanceFixtures(t *testing.T) {
	t.Parallel()

	for _, hotStream := range []bool{false, true} {
		fixture := newConcurrentAppendFixture(t, hotStream)
		store, err := fixture.run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if count := countGlobalMessages(t, store); count != fixture.operationCount() {
			t.Fatalf("global message count = %d", count)
		}
		wantStreamMessages := 1
		if hotStream {
			wantStreamMessages = fixture.operationCount()
		}
		if count := countStreamMessages(t, store, fixture.firstStream); count != wantStreamMessages {
			t.Fatalf("stream message count = %d", count)
		}
	}
}

func BenchmarkConcurrentAppendWorkloads(b *testing.B) {
	for _, benchmark := range []struct {
		name      string
		hotStream bool
	}{
		{name: "independent_streams"},
		{name: "one_hot_stream", hotStream: true},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			fixture := newConcurrentAppendFixture(b, benchmark.hotStream)
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				store, err := fixture.run(ctx)
				if err != nil {
					b.Fatal(err)
				}
				memoryBenchmarkStore = store
			}

			elapsed := b.Elapsed()
			b.StopTimer()
			b.ReportMetric(
				float64(b.N*fixture.operationCount())/elapsed.Seconds(),
				"appends/s",
			)
			b.ReportMetric(float64(fixture.operationCount()), "appends/workload")
			if count := countGlobalMessages(b, memoryBenchmarkStore); count != fixture.operationCount() {
				b.Fatalf("global message count = %d", count)
			}
		})
	}
}

type concurrentAppendOperation struct {
	stream  eventsourcing.StreamID
	pending eventsourcing.PendingMessage
}

type concurrentAppendFixture struct {
	workers     [][]concurrentAppendOperation
	expected    eventsourcing.ExpectedVersion
	firstStream eventsourcing.StreamID
}

func newConcurrentAppendFixture(
	testingTB testing.TB,
	hotStream bool,
) concurrentAppendFixture {
	testingTB.Helper()

	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.changed",
		Version:     1,
		ContentType: eventsourcing.JSONContentType,
		Payload:     []byte("{}"),
	})
	if err != nil {
		testingTB.Fatal(err)
	}
	hot, err := eventsourcing.NewStreamID("account", "hot-account")
	if err != nil {
		testingTB.Fatal(err)
	}
	fixture := concurrentAppendFixture{
		workers:  make([][]concurrentAppendOperation, memoryBenchmarkWriters),
		expected: eventsourcing.ExpectNewStream(),
	}
	if hotStream {
		fixture.expected = eventsourcing.ExpectAnyVersion()
	}
	for worker := range fixture.workers {
		fixture.workers[worker] = make(
			[]concurrentAppendOperation,
			memoryBenchmarkAppendsPerWriter,
		)
		for operation := range fixture.workers[worker] {
			index := worker*memoryBenchmarkAppendsPerWriter + operation
			stream := hot
			if !hotStream {
				stream, err = eventsourcing.NewStreamID(
					"account",
					fmt.Sprintf("account-%d", index),
				)
				if err != nil {
					testingTB.Fatal(err)
				}
			}
			pending, pendingErr := eventsourcing.NewPendingMessage(
				eventsourcing.PendingMessageInput{
					ID:         fmt.Sprintf("concurrent-message-%d", index),
					Stream:     stream,
					Event:      event,
					RecordedAt: time.Date(2026, time.July, 25, 14, 0, 0, 0, time.UTC),
				},
			)
			if pendingErr != nil {
				testingTB.Fatal(pendingErr)
			}
			fixture.workers[worker][operation] = concurrentAppendOperation{
				stream:  stream,
				pending: pending,
			}
			if worker == 0 && operation == 0 {
				fixture.firstStream = stream
			}
		}
	}

	return fixture
}

func (fixture concurrentAppendFixture) operationCount() int {
	return len(fixture.workers) * memoryBenchmarkAppendsPerWriter
}

func (fixture concurrentAppendFixture) run(
	ctx context.Context,
) (*memory.Store, error) {
	store := memory.NewStore()
	errors := make(chan error, len(fixture.workers))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for _, operations := range fixture.workers {
		operations := operations
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for _, operation := range operations {
				if _, err := store.Append(
					ctx,
					operation.stream,
					fixture.expected,
					[]eventsourcing.PendingMessage{operation.pending},
				); err != nil {
					errors <- err

					return
				}
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errors)
	if err := <-errors; err != nil {
		return nil, err
	}

	return store, nil
}

func countGlobalMessages(testingTB testing.TB, store *memory.Store) int {
	testingTB.Helper()

	options, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: 1,
			Limit:        eventsourcing.MaxReadMessages,
		},
	)
	if err != nil {
		testingTB.Fatal(err)
	}
	iterator, err := store.ReadGlobal(context.Background(), options)
	if err != nil {
		testingTB.Fatal(err)
	}
	defer func() {
		if closeErr := iterator.Close(); closeErr != nil {
			testingTB.Error(closeErr)
		}
	}()
	count := 0
	for iterator.Next(context.Background()) {
		count++
	}
	if err := iterator.Err(); err != nil {
		testingTB.Fatal(err)
	}

	return count
}

func countStreamMessages(
	testingTB testing.TB,
	store *memory.Store,
	stream eventsourcing.StreamID,
) int {
	testingTB.Helper()

	options, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{
			FromVersion: 1,
			Limit:       eventsourcing.MaxReadMessages,
		},
	)
	if err != nil {
		testingTB.Fatal(err)
	}
	iterator, err := store.ReadStream(context.Background(), stream, options)
	if err != nil {
		testingTB.Fatal(err)
	}
	defer func() {
		if closeErr := iterator.Close(); closeErr != nil {
			testingTB.Error(closeErr)
		}
	}()
	count := 0
	for iterator.Next(context.Background()) {
		count++
	}
	if err := iterator.Err(); err != nil {
		testingTB.Fatal(err)
	}

	return count
}
