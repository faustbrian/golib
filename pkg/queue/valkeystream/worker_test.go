package valkeystream

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey "github.com/valkey-io/valkey-go"
)

type rawMessage []byte

func (m rawMessage) Bytes() []byte { return m }

func TestWorkerQueuesRequestsSettlesAndShutsDown(t *testing.T) {
	server := miniredis.RunT(t)
	var handled []byte
	worker, err := NewWorkerE(
		WithAddress(server.Addr()),
		WithStreamName("jobs"),
		WithGroup("workers"),
		WithConsumer("worker-1"),
		WithBlockTime(10*time.Millisecond),
		WithRequestTimeout(time.Second),
		WithReclaim(time.Second, 10*time.Millisecond, 4),
		WithRunFunc(func(_ context.Context, task core.TaskMessage) error {
			handled = append([]byte(nil), task.Payload()...)
			return nil
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, "valkey-streams", worker.BackendName())
	assert.Equal(t, "jobs", worker.QueueName())

	message := job.NewMessage(rawMessage("payload"))
	require.NoError(t, worker.Queue(&message))
	received, err := worker.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), received.Payload())
	require.NoError(t, worker.Run(context.Background(), received))
	assert.Equal(t, []byte("payload"), handled)

	state, err := worker.transport.GroupState(context.Background(), "jobs", "workers")
	require.NoError(t, err)
	assert.Equal(t, int64(1), state.Pending)
	require.NoError(t, received.(*job.Message).Ack())
	state, err = worker.transport.GroupState(context.Background(), "jobs", "workers")
	require.NoError(t, err)
	assert.Zero(t, state.Pending)

	require.NoError(t, worker.Shutdown())
	assert.ErrorIs(t, worker.Shutdown(), queue.ErrQueueShutdown)
	assert.ErrorIs(t, worker.Queue(&message), queue.ErrQueueShutdown)
	_, err = worker.Request()
	assert.ErrorIs(t, err, queue.ErrQueueHasBeenClosed)
}

func TestWorkerNackLeavesDeliveryPendingForReclaim(t *testing.T) {
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddress(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithConsumer("worker"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second), WithReclaim(time.Hour, time.Hour, 1),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	message := job.NewMessage(rawMessage("retry"))
	require.NoError(t, worker.Queue(&message))
	delivery, err := worker.Request()
	require.NoError(t, err)
	require.NoError(t, delivery.(*job.Message).Nack())

	state, err := worker.transport.GroupState(context.Background(), "jobs", "workers")
	require.NoError(t, err)
	assert.Equal(t, int64(1), state.Pending)
}

func TestWorkerDeadLettersPermanentFailureBeforeAttemptLimit(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddress(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithConsumer("worker"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second), WithReclaim(time.Hour, time.Hour, 1),
		WithFailureStream("jobs-failures"), WithDeadLetter("jobs-dead", 5),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	message := job.NewMessage(rawMessage("permanent"))
	require.NoError(t, worker.Queue(&message))
	delivery, err := worker.Request()
	require.NoError(t, err)

	handlerErr := management.NewFailure(
		management.ClassificationPermanent,
		"invalid_order",
		errors.New("invalid order"),
	)
	require.NoError(t, delivery.(*job.Message).NackFailure(handlerErr))

	state, err := worker.transport.GroupState(context.Background(), "jobs", "workers")
	require.NoError(t, err)
	assert.Zero(t, state.Pending)
	deadLetters, err := server.Stream("jobs-dead")
	require.NoError(t, err)
	assert.Len(t, deadLetters, 1)
	records, err := worker.ListDeadLetters(context.Background(), management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt, Direction: management.SortAscending,
	})
	require.NoError(t, err)
	require.Len(t, records.Items, 1)
	record := records.Items[0]
	assert.Equal(t, management.CurrentEnvelopeVersion, record.EnvelopeVersion)
	assert.Equal(t, management.ClassificationPermanent, record.Classification)
	assert.Equal(t, "invalid_order", record.FailureCode)
	assert.Equal(t, "jobs", record.Stream)
	assert.Equal(t, "workers", record.ConsumerGroup)
	assert.NotEmpty(t, record.OriginalID)
	assert.Equal(t, record.OriginalID, record.SourceRecordID)
	assert.NotNil(t, record.DeadLetteredAt)
}

func TestTerminalFailurePolicy(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		classification management.Classification
		attempts       int64
		want           bool
	}{
		"retryable below limit":      {management.ClassificationRetryable, 2, false},
		"retryable at limit":         {management.ClassificationRetryable, 3, true},
		"permanent below limit":      {management.ClassificationPermanent, 1, true},
		"malformed below limit":      {management.ClassificationMalformed, 1, true},
		"canceled at limit":          {management.ClassificationCanceled, 3, false},
		"infrastructure above limit": {management.ClassificationInfrastructure, 4, false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := management.NewFailure(tt.classification, "classified_failure", errors.New("cause"))
			assert.Equal(t, tt.want, terminalFailure(err, tt.attempts, 3))
		})
	}
}

func TestWorkerStatsReportsOutstandingWorkAndLifecycleCounters(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	server.SetTime(now.Add(-2 * time.Second))
	worker, err := NewWorkerE(
		WithAddress(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithConsumer("worker"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second), WithReclaim(time.Hour, time.Hour, 1),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })

	message := job.NewMessage(rawMessage("observed"))
	require.NoError(t, worker.Queue(&message))
	delivery, err := worker.Request()
	require.NoError(t, err)

	stats, err := worker.Stats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Pending)
	assert.True(t, stats.LagKnown)
	assert.Equal(t, stats.Pending+stats.Lag, stats.Depth)
	assert.InDelta(t, float64(2*time.Second), float64(stats.OldestPendingAge), float64(100*time.Millisecond))
	assert.Equal(t, uint64(1), stats.Enqueued)
	assert.Equal(t, uint64(1), stats.Delivered)
	assert.Zero(t, stats.Reclaimed)
	assert.Zero(t, stats.Retries)
	assert.Zero(t, stats.Acknowledged)
	assert.Zero(t, stats.DeadLettered)
	assert.Zero(t, stats.SettlementFailures)

	require.NoError(t, delivery.(*job.Message).Ack())
	stats, err = worker.Stats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(1), stats.Acknowledged)
	assert.Zero(t, stats.Pending)
}

func TestWorkerReclaimsAndDeadLettersTerminalDelivery(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Now().UTC()
	server.SetTime(now)
	first, err := NewWorkerE(
		WithAddress(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithConsumer("worker-1"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second), WithReclaim(time.Hour, time.Hour, 1),
		WithDeadLetter("jobs-dead", 2),
	)
	require.NoError(t, err)
	message := job.NewMessage(rawMessage("terminal"))
	require.NoError(t, first.Queue(&message))
	delivery, err := first.Request()
	require.NoError(t, err)
	require.NoError(t, delivery.(*job.Message).Nack())
	require.NoError(t, first.Shutdown())

	server.SetTime(now.Add(2 * time.Second))
	second, err := NewWorkerE(
		WithAddress(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithConsumer("worker-2"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second), WithReclaim(time.Millisecond, time.Millisecond, 1),
		WithDeadLetter("jobs-dead", 2),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Shutdown() })
	reclaimed, err := second.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("terminal"), reclaimed.Payload())
	require.NoError(t, reclaimed.(*job.Message).Nack())
	stats, err := second.Stats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(1), stats.Delivered)
	assert.Equal(t, uint64(1), stats.Reclaimed)
	assert.Equal(t, uint64(1), stats.Retries)
	assert.Equal(t, uint64(1), stats.DeadLettered)
	state, err := second.transport.GroupState(context.Background(), "jobs", "workers")
	require.NoError(t, err)
	assert.Zero(t, state.Pending)
	deadLetters, err := server.Stream("jobs-dead")
	require.NoError(t, err)
	assert.Len(t, deadLetters, 1)
}

func TestWorkerShutdownCancelsBlockingReadAndClosesConnections(t *testing.T) {
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddress(server.Addr()), WithBlockTime(time.Second),
		WithRequestTimeout(2*time.Second), WithShutdownTimeout(time.Second),
	)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- worker.Shutdown() }()
	select {
	case err = <-done:
		require.NoError(t, err)
	case <-time.After(250 * time.Millisecond):
		t.Fatal("shutdown did not cancel the blocking read")
	}
	require.Eventually(t, func() bool {
		return server.CurrentConnectionCount() == 0
	}, time.Second, time.Millisecond)
}

func TestWorkersCreateOneConsumerGroupConcurrently(t *testing.T) {
	server := miniredis.RunT(t)
	const count = 8
	workers := make(chan *Worker, count)
	errorsFound := make(chan error, count)
	var group sync.WaitGroup
	group.Add(count)
	for index := range count {
		go func() {
			defer group.Done()
			worker, err := NewWorkerE(
				WithAddress(server.Addr()), WithStreamName("jobs"),
				WithGroup("workers"), WithConsumer(string(rune('a'+index))),
			)
			if err != nil {
				errorsFound <- err
				return
			}
			workers <- worker
		}()
	}
	group.Wait()
	close(workers)
	close(errorsFound)
	require.Empty(t, errorsFound)
	for worker := range workers {
		require.NoError(t, worker.Shutdown())
	}
}

func TestWorkerConstructorsReturnTypedConnectionErrors(t *testing.T) {
	_, err := NewWorkerE()
	assert.ErrorIs(t, err, ErrInvalidConfiguration)

	started := time.Now()
	worker, err := NewWorkerE(
		WithAddress("127.0.0.1:1"), WithDialTimeout(20*time.Millisecond),
		WithCommandTimeout(20*time.Millisecond),
	)
	assert.Nil(t, worker)
	assert.Error(t, err)
	assert.Less(t, time.Since(started), 250*time.Millisecond)
	assert.NotContains(t, err.Error(), "password")
	assert.NotContains(t, err.Error(), "127.0.0.1:1")

	assert.Panics(t, func() {
		NewWorker(
			WithAddress("127.0.0.1:1"), WithDialTimeout(time.Millisecond),
			WithCommandTimeout(time.Millisecond),
		)
	})
}

func TestLegacyWorkerConstructorReturnsConnectedWorker(t *testing.T) {
	server := miniredis.RunT(t)
	worker := NewWorker(WithAddress(server.Addr()))
	require.NoError(t, worker.Shutdown())
}

func TestWorkerConstructorClosesClientWhenGroupCreationFails(t *testing.T) {
	server := miniredis.RunT(t)
	require.NoError(t, server.Set("jobs", "not-a-stream"))
	worker, err := NewWorkerE(WithAddress(server.Addr()), WithStreamName("jobs"))
	assert.Nil(t, worker)
	assert.Error(t, err)
	require.Eventually(t, func() bool {
		return server.CurrentConnectionCount() == 0
	}, time.Second, time.Millisecond)
}

func TestWorkerConstructorClosesClientWhenPingFails(t *testing.T) {
	server := miniredis.RunT(t)
	original := newValkeyClient
	newValkeyClient = func(option valkey.ClientOption) (valkey.Client, error) {
		client, err := valkey.NewClient(option)
		if err != nil {
			return nil, err
		}
		server.Close()
		return client, nil
	}
	t.Cleanup(func() { newValkeyClient = original })

	worker, err := NewWorkerE(WithAddress(server.Addr()))
	assert.Nil(t, worker)
	assert.ErrorContains(t, err, "connect to server")
}

func TestWorkerQueueAndRequestFailurePaths(t *testing.T) {
	opts := defaultWorkerOptions(t)
	opts.reclaimInterval = time.Millisecond
	transport := &scriptedTransport{
		read: make(chan scriptedRead, 2), claim: make(chan scriptedClaim),
		addErr: errors.New("add failed"),
	}
	worker := newWorkerForTransport(opts, transport)
	assert.ErrorContains(t, worker.Queue(nil), "task is required")
	message := job.NewMessage(rawMessage("payload"))
	assert.ErrorIs(t, worker.Queue(&message), transport.addErr)

	transport.read <- scriptedRead{err: errors.New("read failed")}
	transport.read <- scriptedRead{deliveries: []streamqueue.Delivery{{
		ID: "1-0", Body: message.Bytes(), Attempts: 1,
	}}}
	received, err := worker.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), received.Payload())
	require.NoError(t, worker.Shutdown())
}

func TestWorkerReclaimFailureCursorAndCancellationPaths(t *testing.T) {
	opts := defaultWorkerOptions(t)
	opts.reclaimInterval = time.Millisecond
	message := job.NewMessage(rawMessage("reclaimed"))
	transport := &scriptedTransport{
		read: make(chan scriptedRead), claim: make(chan scriptedClaim, 3),
	}
	transport.claim <- scriptedClaim{err: errors.New("claim failed")}
	transport.claim <- scriptedClaim{result: streamqueue.ClaimResult{
		Next:       "5-0",
		Deliveries: []streamqueue.Delivery{{ID: "1-0", Body: message.Bytes(), Attempts: 2, Reclaimed: true}},
	}}
	transport.claim <- scriptedClaim{result: streamqueue.ClaimResult{Next: "0-0"}}
	worker := newWorkerForTransport(opts, transport)
	received, err := worker.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("reclaimed"), received.Payload())

	require.NoError(t, worker.Shutdown())

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	stopped := &Worker{ctx: cancelled, tasks: make(chan streamqueue.Delivery)}
	assert.False(t, stopped.deliver(streamqueue.Delivery{}))
}

func TestWorkerTimeoutAndSettlementFailures(t *testing.T) {
	t.Run("request timeout", func(t *testing.T) {
		opts := defaultWorkerOptions(t)
		opts.requestTimeout = time.Millisecond
		transport := &scriptedTransport{read: make(chan scriptedRead), claim: make(chan scriptedClaim)}
		worker := newWorkerForTransport(opts, transport)
		_, err := worker.Request()
		assert.ErrorIs(t, err, queue.ErrNoTaskInQueue)
		require.NoError(t, worker.Shutdown())
	})

	t.Run("dead letter failure", func(t *testing.T) {
		opts := defaultWorkerOptions(t)
		transport := newFakeTransport(streamqueue.Delivery{
			ID: "1-0", Body: []byte("not-json"), Attempts: 1,
		})
		transport.deadLetterErr = errors.New("dead letter failed")
		worker := newWorkerForTransport(opts, transport)
		_, err := worker.Request()
		assert.ErrorIs(t, err, transport.deadLetterErr)
		stats, statsErr := worker.Stats(context.Background())
		require.NoError(t, statsErr)
		assert.Equal(t, uint64(1), stats.SettlementFailures)
		assert.Zero(t, stats.DeadLettered)
		require.NoError(t, worker.Shutdown())
	})

	t.Run("ack failure", func(t *testing.T) {
		opts := defaultWorkerOptions(t)
		message := job.NewMessage(rawMessage("payload"))
		transport := newFakeTransport(streamqueue.Delivery{
			ID: "1-0", Body: message.Bytes(), Attempts: 1,
		})
		transport.ackErr = errors.New("ack failed")
		worker := newWorkerForTransport(opts, transport)
		delivery, err := worker.Request()
		require.NoError(t, err)
		assert.ErrorIs(t, delivery.(*job.Message).Ack(), transport.ackErr)
		stats, statsErr := worker.Stats(context.Background())
		require.NoError(t, statsErr)
		assert.Equal(t, uint64(1), stats.SettlementFailures)
		assert.Zero(t, stats.Acknowledged)
		require.NoError(t, worker.Shutdown())
	})

	t.Run("terminal dead letter failure", func(t *testing.T) {
		opts := defaultWorkerOptions(t)
		opts.maxDeliveryAttempts = 2
		message := job.NewMessage(rawMessage("payload"))
		transport := newFakeTransport(streamqueue.Delivery{
			ID: "1-0", Body: message.Bytes(), Attempts: 2, Reclaimed: true,
		})
		transport.deadLetterErr = errors.New("dead letter failed")
		worker := newWorkerForTransport(opts, transport)
		delivery, err := worker.Request()
		require.NoError(t, err)
		assert.ErrorIs(t, delivery.(*job.Message).Nack(), transport.deadLetterErr)
		stats, statsErr := worker.Stats(context.Background())
		require.NoError(t, statsErr)
		assert.Equal(t, uint64(1), stats.SettlementFailures)
		assert.Zero(t, stats.DeadLettered)
		require.NoError(t, worker.Shutdown())
	})

	t.Run("shutdown timeout", func(t *testing.T) {
		opts := defaultWorkerOptions(t)
		opts.shutdownTimeout = time.Millisecond
		opts.reclaimInterval = time.Millisecond
		transport := newStubbornTransport()
		worker := newWorkerForTransport(opts, transport)
		for range 2 {
			select {
			case <-transport.entered:
			case <-time.After(time.Second):
				t.Fatal("worker loop did not enter transport")
			}
		}
		err := worker.Shutdown()
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.True(t, transport.closed.Load())
		select {
		case <-worker.done:
		case <-time.After(time.Second):
			t.Fatal("worker loops did not exit after forced client close")
		}
	})
}

func TestWorkerStatsPropagatesSnapshotAndIdentifierErrors(t *testing.T) {
	opts := defaultWorkerOptions(t)
	transport := newFakeTransport(streamqueue.Delivery{})
	transport.stateErr = errors.New("stats failed")
	worker := newWorkerForTransport(opts, transport)
	_, err := worker.Stats(context.Background())
	assert.ErrorIs(t, err, transport.stateErr)
	transport.stateErr = nil
	transport.state = streamqueue.GroupState{Pending: 1, Lag: 0, OldestPendingID: "invalid"}
	_, err = worker.Stats(context.Background())
	assert.ErrorIs(t, err, streamqueue.ErrMalformedDelivery)
	require.NoError(t, worker.Shutdown())
}

func TestRequestReturnsClosedWhenContextIsCancelled(t *testing.T) {
	opts := defaultWorkerOptions(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker := &Worker{opts: opts, ctx: ctx, tasks: make(chan streamqueue.Delivery)}
	_, err := worker.Request()
	assert.ErrorIs(t, err, queue.ErrQueueHasBeenClosed)

	ctx = context.Background()
	tasks := make(chan streamqueue.Delivery)
	close(tasks)
	worker = &Worker{opts: opts, ctx: ctx, tasks: tasks}
	_, err = worker.Request()
	assert.ErrorIs(t, err, queue.ErrQueueHasBeenClosed)
}

func TestWorkerLoopsStopWhileHandlingResults(t *testing.T) {
	t.Run("read retry cancellation", func(t *testing.T) {
		opts := defaultWorkerOptions(t)
		opts.reclaimInterval = time.Hour
		returned := make(chan struct{}, 1)
		transport := &scriptedTransport{
			read: make(chan scriptedRead, 1), claim: make(chan scriptedClaim),
			readReturned: returned,
		}
		transport.read <- scriptedRead{err: errors.New("read failed")}
		worker := newWorkerForTransport(opts, transport)
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Fatal("read did not return")
		}
		worker.cancel()
		select {
		case <-worker.done:
		case <-time.After(time.Second):
			t.Fatal("read retry did not observe cancellation")
		}
		worker.stopped.Store(true)
		require.NoError(t, transport.Close())
	})

	t.Run("read delivery cancellation", func(t *testing.T) {
		opts := defaultWorkerOptions(t)
		message := job.NewMessage(rawMessage("payload"))
		gate := make(chan struct{})
		called := make(chan struct{}, 1)
		transport := &scriptedTransport{
			read: make(chan scriptedRead, 1), claim: make(chan scriptedClaim),
			readGate: gate, readCalled: called,
		}
		transport.read <- scriptedRead{deliveries: []streamqueue.Delivery{{
			ID: "1-0", Body: message.Bytes(), Attempts: 1,
		}}}
		worker := newWorkerForTransport(opts, transport)
		<-called
		for len(worker.tasks) < cap(worker.tasks) {
			worker.tasks <- streamqueue.Delivery{}
		}
		worker.cancel()
		close(gate)
		select {
		case <-worker.done:
		case <-time.After(time.Second):
			t.Fatal("read delivery did not observe cancellation")
		}
		worker.stopped.Store(true)
		require.NoError(t, transport.Close())
	})

	t.Run("reclaim delivery cancellation", func(t *testing.T) {
		opts := defaultWorkerOptions(t)
		opts.reclaimInterval = time.Millisecond
		message := job.NewMessage(rawMessage("payload"))
		gate := make(chan struct{})
		called := make(chan struct{}, 1)
		transport := &scriptedTransport{
			read: make(chan scriptedRead), claim: make(chan scriptedClaim, 1),
			claimGate: gate, claimCalled: called,
		}
		transport.claim <- scriptedClaim{result: streamqueue.ClaimResult{
			Deliveries: []streamqueue.Delivery{{ID: "1-0", Body: message.Bytes(), Attempts: 2}},
		}}
		worker := newWorkerForTransport(opts, transport)
		<-called
		for len(worker.tasks) < cap(worker.tasks) {
			worker.tasks <- streamqueue.Delivery{}
		}
		worker.cancel()
		close(gate)
		select {
		case <-worker.done:
		case <-time.After(time.Second):
			t.Fatal("reclaim delivery did not observe cancellation")
		}
		worker.stopped.Store(true)
		require.NoError(t, transport.Close())
	})
}

func TestWaitContextCompletesOrCancels(t *testing.T) {
	assert.True(t, waitContext(context.Background(), time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.False(t, waitContext(ctx, time.Hour))
}

func TestWorkerRejectsMalformedAndOversizedDeliveries(t *testing.T) {
	tests := map[string]streamqueue.Delivery{
		"malformed": {ID: "1-0", Body: []byte("not-json"), Attempts: 1},
		"oversized": {ID: "1-0", Body: []byte(string(make([]byte, job.DefaultMaxMessageBytes+1))), Attempts: 1},
	}
	for name, delivery := range tests {
		t.Run(name, func(t *testing.T) {
			transport := newFakeTransport(delivery)
			worker := newWorkerForTransport(defaultWorkerOptions(t), transport)
			_, err := worker.Request()
			assert.Error(t, err)
			assert.Equal(t, 1, transport.deadLetters)
			require.NoError(t, worker.Shutdown())
		})
	}
}

type fakeTransport struct {
	delivery      streamqueue.Delivery
	reads         int
	deadLetters   int
	deadLetterErr error
	ackErr        error
	state         streamqueue.GroupState
	stateErr      error
	closed        bool
}

func newFakeTransport(delivery streamqueue.Delivery) *fakeTransport {
	return &fakeTransport{delivery: delivery}
}

func (*fakeTransport) EnsureGroup(context.Context, string, string) error { return nil }
func (*fakeTransport) Add(context.Context, streamqueue.AddRequest) (string, error) {
	return "1-0", nil
}
func (t *fakeTransport) Read(context.Context, streamqueue.ReadRequest) ([]streamqueue.Delivery, error) {
	if t.reads > 0 {
		return nil, nil
	}
	t.reads++
	return []streamqueue.Delivery{t.delivery}, nil
}
func (*fakeTransport) Claim(context.Context, streamqueue.ClaimRequest) (streamqueue.ClaimResult, error) {
	return streamqueue.ClaimResult{Next: "0-0"}, nil
}
func (t *fakeTransport) Ack(context.Context, streamqueue.AckRequest) error { return t.ackErr }
func (t *fakeTransport) DeadLetter(context.Context, streamqueue.DeadLetterRequest) error {
	t.deadLetters++
	return t.deadLetterErr
}

type scriptedRead struct {
	deliveries []streamqueue.Delivery
	err        error
}

type scriptedClaim struct {
	result streamqueue.ClaimResult
	err    error
}

type scriptedTransport struct {
	read         chan scriptedRead
	claim        chan scriptedClaim
	addErr       error
	readGate     <-chan struct{}
	claimGate    <-chan struct{}
	readCalled   chan<- struct{}
	claimCalled  chan<- struct{}
	readReturned chan<- struct{}
}

func (*scriptedTransport) EnsureGroup(context.Context, string, string) error { return nil }
func (t *scriptedTransport) Add(context.Context, streamqueue.AddRequest) (string, error) {
	return "1-0", t.addErr
}
func (t *scriptedTransport) Read(ctx context.Context, _ streamqueue.ReadRequest) ([]streamqueue.Delivery, error) {
	select {
	case response := <-t.read:
		if t.readCalled != nil {
			t.readCalled <- struct{}{}
		}
		if t.readGate != nil {
			<-t.readGate
		}
		if t.readReturned != nil {
			t.readReturned <- struct{}{}
		}
		return response.deliveries, response.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (t *scriptedTransport) Claim(ctx context.Context, _ streamqueue.ClaimRequest) (streamqueue.ClaimResult, error) {
	select {
	case response := <-t.claim:
		if t.claimCalled != nil {
			t.claimCalled <- struct{}{}
		}
		if t.claimGate != nil {
			<-t.claimGate
		}
		return response.result, response.err
	case <-ctx.Done():
		return streamqueue.ClaimResult{}, ctx.Err()
	}
}
func (*scriptedTransport) Ack(context.Context, streamqueue.AckRequest) error { return nil }
func (*scriptedTransport) DeadLetter(context.Context, streamqueue.DeadLetterRequest) error {
	return nil
}
func (*scriptedTransport) GroupState(context.Context, string, string) (streamqueue.GroupState, error) {
	return streamqueue.GroupState{}, nil
}
func (*scriptedTransport) Close() error { return nil }

type stubbornTransport struct {
	release chan struct{}
	entered chan struct{}
	closed  atomic.Bool
	once    sync.Once
}

func newStubbornTransport() *stubbornTransport {
	return &stubbornTransport{release: make(chan struct{}), entered: make(chan struct{}, 2)}
}
func (*stubbornTransport) EnsureGroup(context.Context, string, string) error { return nil }
func (*stubbornTransport) Add(context.Context, streamqueue.AddRequest) (string, error) {
	return "1-0", nil
}
func (t *stubbornTransport) Read(context.Context, streamqueue.ReadRequest) ([]streamqueue.Delivery, error) {
	t.entered <- struct{}{}
	<-t.release
	return nil, context.Canceled
}
func (t *stubbornTransport) Claim(context.Context, streamqueue.ClaimRequest) (streamqueue.ClaimResult, error) {
	t.entered <- struct{}{}
	<-t.release
	return streamqueue.ClaimResult{}, context.Canceled
}
func (*stubbornTransport) Ack(context.Context, streamqueue.AckRequest) error { return nil }
func (*stubbornTransport) DeadLetter(context.Context, streamqueue.DeadLetterRequest) error {
	return nil
}
func (*stubbornTransport) GroupState(context.Context, string, string) (streamqueue.GroupState, error) {
	return streamqueue.GroupState{}, nil
}
func (t *stubbornTransport) Close() error {
	t.closed.Store(true)
	t.once.Do(func() { close(t.release) })
	return nil
}
func (t *fakeTransport) GroupState(context.Context, string, string) (streamqueue.GroupState, error) {
	return t.state, t.stateErr
}
func (t *fakeTransport) Close() error { t.closed = true; return nil }

func defaultWorkerOptions(t *testing.T) options {
	t.Helper()
	opts, err := newOptions(
		WithAddress("unused:6379"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(100*time.Millisecond), WithReclaim(time.Hour, time.Hour, 1),
		WithLogger(queue.NewEmptyLogger()),
	)
	require.NoError(t, err)
	return opts
}

func TestWorkerRunReturnsHandlerError(t *testing.T) {
	runErr := errors.New("run")
	opts := defaultWorkerOptions(t)
	opts.runFunc = func(context.Context, core.TaskMessage) error { return runErr }
	worker := newWorkerForTransport(opts, newFakeTransport(streamqueue.Delivery{}))
	assert.ErrorIs(t, worker.Run(context.Background(), nil), runErr)
	require.NoError(t, worker.Shutdown())
}
