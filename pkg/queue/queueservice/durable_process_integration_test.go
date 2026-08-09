//go:build integration

package queueservice

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	redisstream "github.com/faustbrian/golib/pkg/queue/redisstream"
	"github.com/faustbrian/golib/pkg/queue/valkeystream"
	"github.com/faustbrian/golib/pkg/service"
	"github.com/redis/go-redis/v9"
)

const durableBackendProcessHelper = "QUEUE_SERVICE_DURABLE_BACKEND_PROCESS_HELPER"

const durableProcessLease = time.Second

func TestDurableBackendProcessTerminationDuplicateWindowAndRecovery(t *testing.T) {
	backends := []struct {
		name    string
		address string
	}{
		{name: "redis-streams", address: requiredIntegrationAddress(t, "TEST_REDIS_ADDRESS")},
		{name: "valkey-streams", address: requiredIntegrationAddress(t, "TEST_VALKEY_ADDRESS")},
	}
	terminationPoints := []struct {
		name        string
		mode        string
		phase       string
		wantPending int64
		wantEffects int64
	}{
		{name: "before-handler-effect", mode: "before-effect", phase: "before-effect", wantPending: 1, wantEffects: 1},
		{name: "after-handler-effect", mode: "after-effect", phase: "after-effect", wantPending: 1, wantEffects: 2},
		{name: "after-settlement", mode: "after-settlement", phase: "after-settlement", wantPending: 0, wantEffects: 1},
	}

	for _, backend := range backends {
		for _, point := range terminationPoints {
			t.Run(backend.name+"/"+point.name, func(t *testing.T) {
				identity := integrationIdentity(t)
				stream := identity + "-jobs"
				group := identity + "-workers"
				effects := identity + "-effects"
				client := redis.NewClient(&redis.Options{Addr: backend.address})
				t.Cleanup(func() { _ = client.Close() })
				message := job.NewMessage(
					queuedPayload("durable-work"),
					job.AllowOption{Timeout: job.Time(30 * time.Second)},
				)
				if err := client.XAdd(t.Context(), &redis.XAddArgs{
					Stream: stream,
					Values: map[string]any{"body": string(message.Bytes())},
				}).Err(); err != nil {
					t.Fatalf("enqueue durable process work: %v", err)
				}

				killDurableBackendProcessAtPhase(
					t, backend.name, backend.address, stream, group, effects,
					point.mode, point.phase,
				)
				awaitDurablePending(t, client, stream, group, point.wantPending)
				if point.wantPending > 0 {
					awaitDurableLeaseExpiry(t, client, stream, group, durableProcessLease)
				}
				runDurableBackendRecoveryProcesses(
					t, backend.name, backend.address, stream, group, effects, 2,
				)
				awaitDurablePending(t, client, stream, group, 0)
				value, err := client.Get(t.Context(), effects).Int64()
				if err != nil {
					t.Fatalf("read durable handler effects: %v", err)
				}
				if value != point.wantEffects {
					owners, ownersErr := client.LRange(t.Context(), effects+"-owners", 0, -1).Result()
					t.Fatalf(
						"handler effects = %d by %v (%v), want %d",
						value,
						owners,
						ownersErr,
						point.wantEffects,
					)
				}
			})
		}
	}
}

func TestDurableBackendProcessHelper(t *testing.T) {
	mode := os.Getenv(durableBackendProcessHelper)
	if mode == "" {
		return
	}
	backendName := os.Getenv("QUEUE_SERVICE_DURABLE_BACKEND")
	address := os.Getenv("QUEUE_SERVICE_DURABLE_ADDRESS")
	stream := os.Getenv("QUEUE_SERVICE_DURABLE_STREAM")
	effects := os.Getenv("QUEUE_SERVICE_DURABLE_EFFECTS")
	client := redis.NewClient(&redis.Options{Addr: address})
	defer func() { _ = client.Close() }()
	if mode == "publish" {
		message := job.NewMessage(
			queuedPayload("durable-work"),
			job.AllowOption{Timeout: job.Time(30 * time.Second)},
		)
		if err := client.XAdd(t.Context(), &redis.XAddArgs{
			Stream: stream,
			Values: map[string]any{"body": string(message.Bytes())},
		}).Err(); err != nil {
			t.Fatalf("publish durable pod work: %v", err)
		}
		writeDurableProcessPhase(t, "published")

		return
	}
	settled := make(chan struct{})
	var settledOnce sync.Once
	consumer := fmt.Sprintf("process-%d", os.Getpid())
	handler := func(ctx context.Context, _ core.TaskMessage) error {
		if mode == "before-effect" {
			writeDurableProcessPhase(t, "before-effect")
			<-ctx.Done()

			return ctx.Err()
		}
		pipeline := client.TxPipeline()
		pipeline.Incr(ctx, effects)
		pipeline.RPush(ctx, effects+"-owners", consumer)
		if _, err := pipeline.Exec(ctx); err != nil {
			return err
		}
		if mode == "after-effect" {
			writeDurableProcessPhase(t, "after-effect")
			<-ctx.Done()

			return ctx.Err()
		}

		return nil
	}
	worker := newDurableProcessBackendWorker(
		t, backendName, address, stream,
		consumer, handler, mode == "recover", 3,
	)
	coordinator, err := queue.NewQueue(
		queue.WithWorker(worker),
		queue.WithWorkerCount(1),
		queue.WithLogger(queue.NewEmptyLogger()),
		queue.WithAfterFn(func() {
			if mode == "after-settlement" {
				writeDurableProcessPhase(t, "after-settlement")
				select {}
			}
			settledOnce.Do(func() { close(settled) })
		}),
	)
	if err != nil {
		t.Fatalf("queue.NewQueue() error = %v", err)
	}
	adapter, err := NewWorker(WorkerOptions{Name: "durable-process-worker", Queue: coordinator})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	runtime, err := service.New(service.Config{
		Components: []service.Component{adapter.Component()},
	})
	if err != nil {
		t.Fatalf("service.New() error = %v", err)
	}
	if err = runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if mode != "recover" {
		select {}
	}
	select {
	case <-settled:
		writeDurableProcessPhase(t, "settled")
	case <-time.After(2 * time.Second):
		writeDurableProcessPhase(t, "no-work")
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err = runtime.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func newDurableProcessBackendWorker(
	t *testing.T,
	backendName string,
	address string,
	stream string,
	consumer string,
	handler func(context.Context, core.TaskMessage) error,
	recovering bool,
	maxAttempts int64,
) core.Worker {
	return newDurableProcessBackendWorkerWithLease(
		t, backendName, address, stream, consumer, handler,
		recovering, durableProcessLease, maxAttempts,
	)
}

func newDurableProcessBackendWorkerWithLease(
	t *testing.T,
	backendName string,
	address string,
	stream string,
	consumer string,
	handler func(context.Context, core.TaskMessage) error,
	recovering bool,
	recoveryLease time.Duration,
	maxAttempts int64,
) core.Worker {
	t.Helper()
	group := strings.TrimSuffix(stream, "-jobs") + "-workers"
	switch backendName {
	case "redis-streams":
		options := []redisstream.Option{
			redisstream.WithAddr(address),
			redisstream.WithStreamName(stream),
			redisstream.WithGroup(group),
			redisstream.WithConsumer(consumer),
			redisstream.WithBlockTime(10 * time.Millisecond),
			redisstream.WithRequestTimeout(250 * time.Millisecond),
			redisstream.WithCommandTimeout(500 * time.Millisecond),
			redisstream.WithReclaim(time.Hour, time.Hour, 8),
			redisstream.WithFailureStream(stream + "-failures"),
			redisstream.WithDeadLetter(stream+"-dead", maxAttempts),
			redisstream.WithRunFunc(handler),
			redisstream.WithLogger(queue.NewEmptyLogger()),
		}
		if recovering {
			options = append(options, redisstream.WithReclaim(recoveryLease, 10*time.Millisecond, 8))
		}
		worker, err := redisstream.NewWorkerE(options...)
		if err != nil {
			t.Fatalf("redisstream.NewWorkerE() error = %v", err)
		}

		return worker
	case "valkey-streams":
		options := []valkeystream.Option{
			valkeystream.WithAddress(address),
			valkeystream.WithStreamName(stream),
			valkeystream.WithGroup(group),
			valkeystream.WithConsumer(consumer),
			valkeystream.WithBlockTime(10 * time.Millisecond),
			valkeystream.WithRequestTimeout(250 * time.Millisecond),
			valkeystream.WithCommandTimeout(500 * time.Millisecond),
			valkeystream.WithDialTimeout(500 * time.Millisecond),
			valkeystream.WithShutdownTimeout(2 * time.Second),
			valkeystream.WithReclaim(time.Hour, time.Hour, 8),
			valkeystream.WithFailureStream(stream + "-failures"),
			valkeystream.WithDeadLetter(stream+"-dead", maxAttempts),
			valkeystream.WithRunFunc(handler),
			valkeystream.WithLogger(queue.NewEmptyLogger()),
		}
		if recovering {
			options = append(options, valkeystream.WithReclaim(recoveryLease, 10*time.Millisecond, 8))
		}
		worker, err := valkeystream.NewWorkerE(options...)
		if err != nil {
			t.Fatalf("valkeystream.NewWorkerE() error = %v", err)
		}

		return worker
	default:
		t.Fatalf("unsupported durable backend %q", backendName)

		return nil
	}
}

func killDurableBackendProcessAtPhase(
	t *testing.T,
	backendName string,
	address string,
	stream string,
	group string,
	effects string,
	mode string,
	phase string,
) {
	t.Helper()
	command := durableBackendProcessCommand(
		backendName, address, stream, group, effects, mode,
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("durable helper stdout pipe: %v", err)
	}
	command.Stderr = command.Stdout
	if err = command.Start(); err != nil {
		t.Fatalf("start durable helper process: %v", err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	scanner := bufio.NewScanner(stdout)
	phaseSeen := make(chan bool, 1)
	go func() {
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == phase {
				phaseSeen <- true

				return
			}
		}
		phaseSeen <- false
	}()
	select {
	case seen := <-phaseSeen:
		if !seen {
			t.Fatal("durable helper exited before the requested termination phase")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("durable helper did not reach the requested termination phase")
	}
	if err = command.Process.Kill(); err != nil {
		t.Fatalf("kill durable helper process: %v", err)
	}
	if err = command.Wait(); err == nil {
		t.Fatal("killed durable helper process exited successfully")
	}
}

func writeDurableProcessPhase(t *testing.T, phase string) {
	t.Helper()
	if _, err := fmt.Fprintln(os.Stdout, phase); err != nil {
		t.Fatalf("write durable process phase: %v", err)
	}
}

func runDurableBackendRecoveryProcesses(
	t *testing.T,
	backendName string,
	address string,
	stream string,
	group string,
	effects string,
	count int,
) {
	t.Helper()
	var wait sync.WaitGroup
	errorsFound := make(chan error, count)
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			command := durableBackendProcessCommand(
				backendName, address, stream, group, effects, "recover",
			)
			output, err := command.CombinedOutput()
			if err != nil {
				errorsFound <- fmt.Errorf("durable recovery helper: %w: %s", err, output)
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
}

func durableBackendProcessCommand(
	backendName string,
	address string,
	stream string,
	group string,
	effects string,
	mode string,
) *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^TestDurableBackendProcessHelper$")
	command.Env = append(os.Environ(),
		durableBackendProcessHelper+"="+mode,
		"QUEUE_SERVICE_DURABLE_BACKEND="+backendName,
		"QUEUE_SERVICE_DURABLE_ADDRESS="+address,
		"QUEUE_SERVICE_DURABLE_STREAM="+stream,
		"QUEUE_SERVICE_DURABLE_GROUP="+group,
		"QUEUE_SERVICE_DURABLE_EFFECTS="+effects,
	)

	return command
}

func awaitDurablePending(
	t *testing.T,
	client *redis.Client,
	stream string,
	group string,
	wanted int64,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		pending, err := client.XPending(t.Context(), stream, group).Result()
		if err == nil && pending.Count == wanted {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending deliveries = (%+v, %v), want %d", pending, err, wanted)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func awaitDurableLeaseExpiry(
	t *testing.T,
	client *redis.Client,
	stream string,
	group string,
	minimum time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		pending, err := client.XPendingExt(t.Context(), &redis.XPendingExtArgs{
			Stream: stream,
			Group:  group,
			Start:  "-",
			End:    "+",
			Count:  1,
		}).Result()
		if err == nil && len(pending) == 1 && pending[0].Idle >= minimum {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending lease = (%+v, %v), want idle >= %s", pending, err, minimum)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
