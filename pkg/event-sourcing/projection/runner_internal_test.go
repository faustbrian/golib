package projection

import (
	"context"
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

var errProjectionTest = errors.New("projection test failure")

func TestNewRunnerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	valid := internalRunnerConfig()
	tests := map[string]func(*RunnerConfig){
		"name": func(config *RunnerConfig) {
			config.Name = ""
		},
		"reader": func(config *RunnerConfig) {
			config.Reader = nil
		},
		"checkpoint store": func(config *RunnerConfig) {
			config.Checkpoints = nil
		},
		"handler": func(config *RunnerConfig) {
			config.Handler = nil
		},
		"zero batch": func(config *RunnerConfig) {
			config.BatchSize = 0
		},
		"large batch": func(config *RunnerConfig) {
			config.BatchSize = eventsourcing.MaxReadMessages + 1
		},
	}
	for name, mutate := range tests {
		mutate := mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := valid
			mutate(&config)
			runner, err := NewRunner(config)
			if runner != nil ||
				!errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("NewRunner() = %#v, %v", runner, err)
			}
		})
	}
}

func TestRunnerRejectsInvalidCallsAndPreservesLoadFailures(t *testing.T) {
	t.Parallel()

	runner := internalRunner(t, internalRunnerConfig())
	var nilRunner *Runner
	var nilContext context.Context
	if result, err := nilRunner.RunBatch(
		context.Background(),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
		result != (BatchResult{}) {
		t.Fatalf("nil RunBatch() = %#v, %v", result, err)
	}
	if result, err := runner.RunBatch(
		nilContext,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
		result != (BatchResult{}) {
		t.Fatalf("RunBatch(nil) = %#v, %v", result, err)
	}

	config := internalRunnerConfig()
	config.Checkpoints = internalCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			return 0, errProjectionTest
		},
		save: func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
			eventsourcing.GlobalPosition,
		) error {
			return nil
		},
	}
	runner = internalRunner(t, config)
	if _, err := runner.RunBatch(
		context.Background(),
	); !errors.Is(err, errProjectionTest) {
		t.Fatalf("RunBatch(load failure) = %v", err)
	}

	config = internalRunnerConfig()
	config.Checkpoints = internalCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			return 0, nil
		},
		save: func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
			eventsourcing.GlobalPosition,
		) error {
			return nil
		},
	}
	runner = internalRunner(t, config)
	if _, err := runner.RunBatch(
		context.Background(),
	); !errors.Is(err, ErrCheckpointCorrupt) {
		t.Fatalf("RunBatch(zero checkpoint) = %v", err)
	}

	config = internalRunnerConfig()
	runner = internalRunner(t, config)
	runner.batchSize = 0
	if _, err := runner.RunBatch(
		context.Background(),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("RunBatch(invalid internal batch) = %v", err)
	}
}

func TestRunnerHandlesExhaustedAndInvalidReaders(t *testing.T) {
	t.Parallel()

	maximum := eventsourcing.GlobalPosition(^uint64(0))
	config := internalRunnerConfig()
	config.Checkpoints = internalCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			return maximum, nil
		},
		save: func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
			eventsourcing.GlobalPosition,
		) error {
			return nil
		},
	}
	config.Reader = internalGlobalReader{
		read: func(
			_ context.Context,
			options eventsourcing.ReadGlobalOptions,
		) (eventsourcing.MessageIterator, error) {
			if options.FromPosition() != maximum ||
				options.ToPosition() != maximum ||
				options.Limit() != 1 {
				t.Fatalf("checkpoint verification options = %#v", options)
			}

			return &internalIterator{
				messages: []eventsourcing.Message{
					internalProjectionMessage(t, maximum),
				},
			}, nil
		},
	}
	runner := internalRunner(t, config)
	result, err := runner.RunBatch(context.Background())
	if err != nil || result.Checkpoint() != maximum {
		t.Fatalf("RunBatch(maximum) = %#v, %v", result, err)
	}

	config.Reader = readerWithIterator(&internalIterator{})
	runner = internalRunner(t, config)
	if _, err := runner.RunBatch(
		context.Background(),
	); !errors.Is(err, ErrCheckpointAheadOfHistory) {
		t.Fatalf("RunBatch(missing maximum) = %v", err)
	}

	tests := map[string]struct {
		reader eventsourcing.GlobalReader
		want   error
	}{
		"reader failure": {
			reader: internalGlobalReader{
				read: func(
					context.Context,
					eventsourcing.ReadGlobalOptions,
				) (eventsourcing.MessageIterator, error) {
					return nil, errProjectionTest
				},
			},
			want: errProjectionTest,
		},
		"nil iterator": {
			reader: internalGlobalReader{
				read: func(
					context.Context,
					eventsourcing.ReadGlobalOptions,
				) (eventsourcing.MessageIterator, error) {
					return nil, nil
				},
			},
			want: eventsourcing.ErrInvalidArgument,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := internalRunnerConfig()
			config.Reader = testCase.reader
			runner := internalRunner(t, config)
			if _, err := runner.RunBatch(
				context.Background(),
			); !errors.Is(err, testCase.want) {
				t.Fatalf("RunBatch() = %v", err)
			}
		})
	}
}

func TestRunnerRejectsCorruptGlobalHistoryAndReaderOverrun(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		messages  []eventsourcing.Message
		batchSize uint32
	}{
		"missing position": {
			messages:  []eventsourcing.Message{internalProjectionMessage(t, 0)},
			batchSize: 1,
		},
		"position gap": {
			messages:  []eventsourcing.Message{internalProjectionMessage(t, 2)},
			batchSize: 1,
		},
		"overrun": {
			messages: []eventsourcing.Message{
				internalProjectionMessage(t, 1),
				internalProjectionMessage(t, 2),
			},
			batchSize: 1,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := internalRunnerConfig()
			config.BatchSize = testCase.batchSize
			config.Reader = readerWithIterator(&internalIterator{
				messages: testCase.messages,
			})
			runner := internalRunner(t, config)
			_, err := runner.RunBatch(context.Background())
			if !errors.Is(err, eventsourcing.ErrCorruptHistory) {
				t.Fatalf("RunBatch() = %v", err)
			}
		})
	}
}

func TestRunnerCheckpointVerificationFailsClosed(t *testing.T) {
	t.Parallel()

	iteratorFailure := errors.New("checkpoint iterator failed")
	closeFailure := errors.New("checkpoint iterator close failed")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := map[string]struct {
		checkpoint eventsourcing.GlobalPosition
		ctx        context.Context
		reader     eventsourcing.GlobalReader
		want       []error
	}{
		"invalid checkpoint": {
			ctx:  context.Background(),
			want: []error{eventsourcing.ErrInvalidArgument},
		},
		"reader failure": {
			checkpoint: 1,
			ctx:        context.Background(),
			reader: internalGlobalReader{read: func(
				context.Context,
				eventsourcing.ReadGlobalOptions,
			) (eventsourcing.MessageIterator, error) {
				return nil, errProjectionTest
			}},
			want: []error{errProjectionTest},
		},
		"nil iterator": {
			checkpoint: 1,
			ctx:        context.Background(),
			reader: internalGlobalReader{read: func(
				context.Context,
				eventsourcing.ReadGlobalOptions,
			) (eventsourcing.MessageIterator, error) {
				return nil, nil
			}},
			want: []error{eventsourcing.ErrInvalidArgument},
		},
		"missing checkpoint": {
			checkpoint: 1,
			ctx:        context.Background(),
			reader:     readerWithIterator(&internalIterator{}),
			want: []error{
				ErrCheckpointAheadOfHistory,
				ErrCheckpointCorrupt,
			},
		},
		"canceled verification": {
			checkpoint: 1,
			ctx:        cancelled,
			reader:     readerWithIterator(&internalIterator{}),
			want:       []error{context.Canceled},
		},
		"empty iterator failure": {
			checkpoint: 1,
			ctx:        context.Background(),
			reader: readerWithIterator(&internalIterator{
				err:      iteratorFailure,
				closeErr: closeFailure,
			}),
			want: []error{iteratorFailure, closeFailure},
		},
		"missing global position": {
			checkpoint: 1,
			ctx:        context.Background(),
			reader: readerWithIterator(&internalIterator{
				messages: []eventsourcing.Message{
					internalProjectionMessage(t, 0),
				},
			}),
			want: []error{eventsourcing.ErrCorruptHistory},
		},
		"wrong global position": {
			checkpoint: 1,
			ctx:        context.Background(),
			reader: readerWithIterator(&internalIterator{
				messages: []eventsourcing.Message{
					internalProjectionMessage(t, 2),
				},
			}),
			want: []error{eventsourcing.ErrCorruptHistory},
		},
		"reader overrun": {
			checkpoint: 1,
			ctx:        context.Background(),
			reader: readerWithIterator(&internalIterator{
				messages: []eventsourcing.Message{
					internalProjectionMessage(t, 1),
					internalProjectionMessage(t, 2),
				},
			}),
			want: []error{eventsourcing.ErrCorruptHistory},
		},
		"terminal iterator failures": {
			checkpoint: 1,
			ctx:        context.Background(),
			reader: readerWithIterator(&internalIterator{
				messages: []eventsourcing.Message{
					internalProjectionMessage(t, 1),
				},
				err:      iteratorFailure,
				closeErr: closeFailure,
			}),
			want: []error{iteratorFailure, closeFailure},
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := internalRunnerConfig()
			if testCase.reader != nil {
				config.Reader = testCase.reader
			}
			runner := internalRunner(t, config)
			err := runner.verifyCheckpoint(
				testCase.ctx,
				testCase.checkpoint,
			)
			for _, want := range testCase.want {
				if !errors.Is(err, want) {
					t.Fatalf("verifyCheckpoint() = %v, want %v", err, want)
				}
			}
		})
	}
}

func TestRunnerJoinsIteratorFailures(t *testing.T) {
	t.Parallel()

	iteratorFailure := errors.New("iterator failed")
	closeFailure := errors.New("iterator close failed")
	config := internalRunnerConfig()
	config.Reader = readerWithIterator(&internalIterator{
		err:      iteratorFailure,
		closeErr: closeFailure,
	})
	runner := internalRunner(t, config)
	_, err := runner.RunBatch(context.Background())
	if !errors.Is(err, iteratorFailure) || !errors.Is(err, closeFailure) {
		t.Fatalf("RunBatch() = %v", err)
	}
}

func internalRunnerConfig() RunnerConfig {
	return RunnerConfig{
		Name:   "account-summary",
		Reader: readerWithIterator(&internalIterator{}),
		Checkpoints: internalCheckpointStore{
			load: func(
				context.Context,
				string,
			) (eventsourcing.GlobalPosition, error) {
				return 0, ErrCheckpointNotFound
			},
			save: func(
				context.Context,
				string,
				eventsourcing.GlobalPosition,
				eventsourcing.GlobalPosition,
			) error {
				return nil
			},
		},
		Handler: func(context.Context, eventsourcing.Delivery) error {
			return nil
		},
		BatchSize: 1,
	}
}

func internalRunner(t *testing.T, config RunnerConfig) *Runner {
	t.Helper()

	runner, err := NewRunner(config)
	if err != nil {
		t.Fatal(err)
	}

	return runner
}

type internalCheckpointStore struct {
	load   func(context.Context, string) (eventsourcing.GlobalPosition, error)
	status func(context.Context, string) (Status, error)
	save   func(
		context.Context,
		string,
		eventsourcing.GlobalPosition,
		eventsourcing.GlobalPosition,
	) error
}

func (store internalCheckpointStore) Load(
	ctx context.Context,
	name string,
) (eventsourcing.GlobalPosition, error) {
	return store.load(ctx, name)
}

func (store internalCheckpointStore) Status(
	ctx context.Context,
	name string,
) (Status, error) {
	if store.status != nil {
		return store.status(ctx, name)
	}
	checkpoint, err := store.load(ctx, name)
	if errors.Is(err, ErrCheckpointNotFound) {
		return NewStatus(StatusInput{State: StateRunning})
	}
	if err != nil {
		return Status{}, err
	}
	if checkpoint == 0 {
		return Status{}, nil
	}

	return NewStatus(StatusInput{
		State:         StateRunning,
		Checkpoint:    checkpoint,
		HasCheckpoint: true,
	})
}

func (store internalCheckpointStore) Save(
	ctx context.Context,
	name string,
	expected eventsourcing.GlobalPosition,
	next eventsourcing.GlobalPosition,
) error {
	return store.save(ctx, name, expected, next)
}

type internalGlobalReader struct {
	read func(
		context.Context,
		eventsourcing.ReadGlobalOptions,
	) (eventsourcing.MessageIterator, error)
}

func (reader internalGlobalReader) ReadGlobal(
	ctx context.Context,
	options eventsourcing.ReadGlobalOptions,
) (eventsourcing.MessageIterator, error) {
	return reader.read(ctx, options)
}

func readerWithIterator(iterator eventsourcing.MessageIterator) internalGlobalReader {
	return internalGlobalReader{
		read: func(
			context.Context,
			eventsourcing.ReadGlobalOptions,
		) (eventsourcing.MessageIterator, error) {
			return iterator, nil
		},
	}
}

type internalIterator struct {
	messages []eventsourcing.Message
	index    int
	err      error
	closeErr error
}

func (iterator *internalIterator) Next(context.Context) bool {
	if iterator.index >= len(iterator.messages) {
		return false
	}
	iterator.index++

	return true
}

func (iterator *internalIterator) Message() eventsourcing.Message {
	return iterator.messages[iterator.index-1]
}

func (iterator *internalIterator) Err() error {
	return iterator.err
}

func (iterator *internalIterator) Close() error {
	return iterator.closeErr
}

func internalProjectionMessage(
	t *testing.T,
	position eventsourcing.GlobalPosition,
) eventsourcing.Message {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-1")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.changed",
		Version:     1,
		ContentType: "application/json",
		Payload:     []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "message-1",
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Date(2026, time.July, 25, 14, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  1,
		GlobalPosition: position,
	})
	if err != nil {
		t.Fatal(err)
	}

	return message
}
