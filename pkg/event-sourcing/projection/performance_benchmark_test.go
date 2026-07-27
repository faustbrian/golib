package projection

import (
	"context"
	"fmt"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

var projectionBenchmarkResult BatchResult

func TestProjectionPerformanceFixture(t *testing.T) {
	t.Parallel()

	runner, checkpoints := projectionPerformanceRunner(t, 0, 3)
	result, err := runner.RunBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned() != 3 ||
		result.Handled() != 3 ||
		result.Checkpointed() != 3 ||
		result.Checkpoint() != 3 ||
		checkpoints.saves != 3 ||
		checkpoints.handled != 3 {
		t.Fatalf(
			"RunBatch() = %#v, saves=%d handled=%d",
			result,
			checkpoints.saves,
			checkpoints.handled,
		)
	}

	liveRunner, liveCheckpoints := projectionPerformanceRunner(t, 10_000, 3)
	liveResult, err := liveRunner.RunBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if liveResult.Scanned() != 3 ||
		liveResult.Checkpoint() != 10_003 ||
		liveCheckpoints.saves != 3 ||
		liveCheckpoints.handled != 3 {
		t.Fatalf(
			"live RunBatch() = %#v, saves=%d handled=%d",
			liveResult,
			liveCheckpoints.saves,
			liveCheckpoints.handled,
		)
	}
}

func BenchmarkProjectionReplayAndCheckpoint(b *testing.B) {
	for _, size := range []uint32{10, 100, 1_000} {
		b.Run(fmt.Sprintf("batch_%d", size), func(b *testing.B) {
			runner, checkpoints := projectionPerformanceRunner(b, 0, size)
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := runner.RunBatch(ctx)
				if err != nil {
					b.Fatal(err)
				}
				projectionBenchmarkResult = result
			}

			b.StopTimer()
			if projectionBenchmarkResult.Scanned() != size ||
				projectionBenchmarkResult.Handled() != size ||
				projectionBenchmarkResult.Checkpointed() != size ||
				projectionBenchmarkResult.Checkpoint() != eventsourcing.GlobalPosition(size) ||
				checkpoints.saves != uint64(size) ||
				checkpoints.handled != uint64(size) {
				b.Fatalf(
					"RunBatch() = %#v, saves=%d handled=%d",
					projectionBenchmarkResult,
					checkpoints.saves,
					checkpoints.handled,
				)
			}
		})
	}
}

func BenchmarkProjectionLiveCatchUp(b *testing.B) {
	for _, size := range []uint32{10, 100, 1_000} {
		b.Run(fmt.Sprintf("tail_%d", size), func(b *testing.B) {
			runner, checkpoints := projectionPerformanceRunner(b, 1_000_000, size)
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := runner.RunBatch(ctx)
				if err != nil {
					b.Fatal(err)
				}
				projectionBenchmarkResult = result
			}

			b.StopTimer()
			if projectionBenchmarkResult.Scanned() != size ||
				projectionBenchmarkResult.Handled() != size ||
				projectionBenchmarkResult.Checkpointed() != size ||
				projectionBenchmarkResult.Checkpoint() !=
					eventsourcing.GlobalPosition(1_000_000+size) ||
				checkpoints.saves != uint64(size) ||
				checkpoints.handled != uint64(size) {
				b.Fatalf(
					"RunBatch() = %#v, saves=%d handled=%d",
					projectionBenchmarkResult,
					checkpoints.saves,
					checkpoints.handled,
				)
			}
		})
	}
}

func projectionPerformanceRunner(
	testingTB testing.TB,
	checkpoint eventsourcing.GlobalPosition,
	size uint32,
) (*Runner, *projectionPerformanceCheckpoints) {
	testingTB.Helper()

	messages := projectionPerformanceMessages(testingTB, checkpoint, size)
	checkpoints := &projectionPerformanceCheckpoints{initial: checkpoint}
	runner, err := NewRunner(RunnerConfig{
		Name:        "performance-projection",
		Reader:      projectionPerformanceReader{messages: messages},
		Checkpoints: checkpoints,
		Handler: func(context.Context, eventsourcing.Delivery) error {
			checkpoints.handled++

			return nil
		},
		Guard:     PermitReplay,
		BatchSize: size,
	})
	if err != nil {
		testingTB.Fatal(err)
	}

	return runner, checkpoints
}

func projectionPerformanceMessages(
	testingTB testing.TB,
	checkpoint eventsourcing.GlobalPosition,
	size uint32,
) []eventsourcing.Message {
	testingTB.Helper()

	stream, err := eventsourcing.NewStreamID("account", "benchmark-account")
	if err != nil {
		testingTB.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.changed",
		Version:     1,
		ContentType: eventsourcing.JSONContentType,
		Payload:     []byte("{}"),
	})
	if err != nil {
		testingTB.Fatal(err)
	}
	messages := make([]eventsourcing.Message, size)
	for index := range messages {
		position := uint64(checkpoint) + uint64(index) + 1
		pending, pendingErr := eventsourcing.NewPendingMessage(
			eventsourcing.PendingMessageInput{
				ID:         fmt.Sprintf("projection-message-%d", position),
				Stream:     stream,
				Event:      event,
				RecordedAt: time.Date(2026, time.July, 25, 14, 0, 0, 0, time.UTC),
			},
		)
		if pendingErr != nil {
			testingTB.Fatal(pendingErr)
		}
		messages[index], err = eventsourcing.NewMessage(eventsourcing.MessageInput{
			Pending:        pending,
			StreamVersion:  position,
			GlobalPosition: eventsourcing.GlobalPosition(position),
		})
		if err != nil {
			testingTB.Fatal(err)
		}
	}

	return messages
}

type projectionPerformanceReader struct {
	messages []eventsourcing.Message
}

func (reader projectionPerformanceReader) ReadGlobal(
	_ context.Context,
	options eventsourcing.ReadGlobalOptions,
) (eventsourcing.MessageIterator, error) {
	return &projectionPerformanceIterator{
		messages: reader.messages[:options.Limit()],
	}, nil
}

type projectionPerformanceIterator struct {
	messages []eventsourcing.Message
	index    int
}

func (iterator *projectionPerformanceIterator) Next(context.Context) bool {
	if iterator.index >= len(iterator.messages) {
		return false
	}
	iterator.index++

	return true
}

func (iterator *projectionPerformanceIterator) Message() eventsourcing.Message {
	return iterator.messages[iterator.index-1]
}

func (*projectionPerformanceIterator) Err() error {
	return nil
}

func (*projectionPerformanceIterator) Close() error {
	return nil
}

type projectionPerformanceCheckpoints struct {
	checkpoint eventsourcing.GlobalPosition
	initial    eventsourcing.GlobalPosition
	saves      uint64
	handled    uint64
}

func (store *projectionPerformanceCheckpoints) Status(
	context.Context,
	string,
) (Status, error) {
	store.checkpoint = store.initial
	store.saves = 0
	store.handled = 0

	return NewStatus(StatusInput{
		Checkpoint:    store.initial,
		HasCheckpoint: store.initial != 0,
		State:         StateRunning,
	})
}

func (store *projectionPerformanceCheckpoints) Save(
	_ context.Context,
	_ string,
	expected eventsourcing.GlobalPosition,
	next eventsourcing.GlobalPosition,
) error {
	if expected != store.checkpoint || next != expected+1 {
		return ErrCheckpointConflict
	}
	store.checkpoint = next
	store.saves++

	return nil
}
