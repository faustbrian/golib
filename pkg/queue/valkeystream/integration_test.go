//go:build integration

package valkeystream

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	valkey "github.com/valkey-io/valkey-go"
)

const valkey9Image = "valkey/valkey:9.1.0@sha256:8e8d64b405ce18f41b8e5ee20aa4687a8ed0022d1298f2ce31cdcf3a76e09411"

type integrationMessage []byte

func (m integrationMessage) Bytes() []byte { return m }

func TestValkey9NativeLifecycleAndStats(t *testing.T) {
	container, endpoint := startValkey9(t, nil, nil)
	exitCode, output, err := container.Exec(t.Context(), []string{"valkey-cli", "INFO", "server"})
	require.NoError(t, err)
	require.Zero(t, exitCode)
	info, err := io.ReadAll(output)
	require.NoError(t, err)
	assert.Contains(t, string(info), "valkey_version:9.1.0")

	worker := newIntegrationWorker(t, endpoint, "lifecycle", "worker-1")
	message := job.NewMessage(integrationMessage("payload"))
	require.NoError(t, worker.Queue(&message))
	delivery, err := worker.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), delivery.Payload())

	stats, err := worker.Stats(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Depth)
	assert.Equal(t, int64(1), stats.Pending)
	assert.Zero(t, stats.Lag)
	assert.True(t, stats.LagKnown)
	assert.GreaterOrEqual(t, stats.OldestPendingAge, time.Duration(0))
	require.NoError(t, delivery.(*job.Message).Ack())
	stats, err = worker.Stats(t.Context())
	require.NoError(t, err)
	assert.Zero(t, stats.Depth)
	assert.Equal(t, uint64(1), stats.Enqueued)
	assert.Equal(t, uint64(1), stats.Delivered)
	assert.Equal(t, uint64(1), stats.Acknowledged)
	require.NoError(t, worker.Shutdown())
}

func TestValkey9NativeManagementStatusIntegration(t *testing.T) {
	_, endpoint := startValkey9(t, nil, nil)
	options := integrationWorkerOptions(endpoint, "management-status", "worker-1")
	options = append(options, WithManagementStatus(management.StatusMetadata{
		ID: "worker-1", Version: "v1.0.0", Concurrency: 1,
		Protocol: management.ProtocolVersion{Major: 1},
	}))
	worker, err := NewWorkerE(options...)
	require.NoError(t, err)
	defer func() { require.NoError(t, worker.Shutdown()) }()
	message := job.NewMessage(integrationMessage("status-payload"))
	require.NoError(t, worker.Queue(&message))

	reader, err := management.NewStatusReader(management.StatusReaderConfig{
		Workers: []management.WorkerStatusProvider{worker},
		Queues:  []management.QueueStatusProvider{worker},
	})
	require.NoError(t, err)
	workers, err := reader.ListWorkers(t.Context(), management.StatusPageRequest{Limit: 1})
	require.NoError(t, err)
	require.Len(t, workers.Items, 1)
	assert.Equal(t, "worker-1", workers.Items[0].ID)
	assert.Equal(t, "valkey-streams", workers.Items[0].Backend)
	queues, err := reader.ListQueues(t.Context(), management.StatusPageRequest{Limit: 1})
	require.NoError(t, err)
	require.Len(t, queues.Items, 1)
	assert.Equal(t, int64(1), queues.Items[0].Metrics.Depth.Value)
	assert.True(t, queues.Items[0].Metrics.Depth.Supported)
	assert.True(t, queues.Items[0].Metrics.Succeeded.Supported)
}

func TestValkey9NativeManagementRecordsIntegration(t *testing.T) {
	_, endpoint := startValkey9(t, nil, nil)
	options := integrationWorkerOptions(endpoint, "management-records", "worker-1")
	options = append(options, WithManagementStatus(management.StatusMetadata{
		ID: "worker-1", Version: "v1.0.0", Concurrency: 1,
		Protocol: management.ProtocolVersion{Major: 1},
	}), WithReplayDestinations("management-archive"), WithRecordRetention(2))
	worker, err := NewWorkerE(options...)
	require.NoError(t, err)
	message := job.NewMessage(integrationMessage("sensitive"))
	require.NoError(t, worker.Queue(&message))
	delivery, err := worker.Request()
	require.NoError(t, err)
	require.NoError(t, delivery.(*job.Message).Nack())

	failures, err := worker.ListFailures(t.Context(), management.PageRequest{
		Limit: 10, Sort: management.SortOccurredAt, Direction: management.SortDescending,
	})
	require.NoError(t, err)
	require.Len(t, failures.Items, 1)
	assert.Equal(t, management.RecordFailure, failures.Items[0].Kind)
	assert.Empty(t, failures.Items[0].Payload.Data)

	inspected, err := worker.Inspect(t.Context(), management.InspectRequest{
		Kind: management.RecordFailure, ID: failures.Items[0].ID,
		Visibility: management.PayloadRevealed,
	})
	require.NoError(t, err)
	assert.Equal(t, message.Bytes(), inspected.Payload.Data)
	replay := nativeCommand(
		"replay-1", management.CommandReplay, management.TargetFailure,
		failures.Items[0].ID,
	)
	replay.Confirmed = true
	replay.Replay = &management.ReplayOptions{
		Destination:       "management-archive",
		IdempotencyPolicy: management.ReplayRejectDuplicate,
	}
	result, err := worker.Execute(t.Context(), replay)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	replay.ID = "replay-duplicate"
	replay.IdempotencyKey = replay.ID
	result, err = worker.Execute(t.Context(), replay)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "replay_duplicate", result.FailureCode)
	replay.ID = "replay-replace"
	replay.IdempotencyKey = replay.ID
	replay.Replay.IdempotencyPolicy = management.ReplayReplaceDuplicate
	result, err = worker.Execute(t.Context(), replay)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	native := worker.transport.(*nativeTransport)
	replayed, err := native.client.Do(
		t.Context(), native.client.B().Xrange().Key("management-archive").
			Start("-").End("+").Build(),
	).AsXRange()
	require.NoError(t, err)
	require.Len(t, replayed, 1)
	assert.Equal(t, string(message.Bytes()), replayed[0].FieldValues[streamBodyField])
	failures, err = worker.ListFailures(t.Context(), management.PageRequest{
		Limit: 10, Sort: management.SortOccurredAt, Direction: management.SortDescending,
	})
	require.NoError(t, err)
	require.Len(t, failures.Items, 1, "replay must preserve the source record")

	command := nativeCommand(
		"retry-1", management.CommandRetry, management.TargetFailure,
		failures.Items[0].ID,
	)
	result, err = worker.Execute(t.Context(), command)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	failures, err = worker.ListFailures(t.Context(), management.PageRequest{
		Limit: 10, Sort: management.SortOccurredAt, Direction: management.SortDescending,
	})
	require.NoError(t, err)
	assert.Empty(t, failures.Items)
	retried, err := worker.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("sensitive"), retried.Payload())
	require.NoError(t, retried.(*job.Message).Ack())
	transport := worker.transport.(nativeMutationTransport)
	for index := range 3 {
		require.NoError(t, transport.RecordFailure(
			t.Context(), worker.opts.failureStream, worker.opts.stream, worker.opts.group,
			streamqueue.Delivery{
				ID: fmt.Sprintf("%d-0", index+1), Body: message.Bytes(), Attempts: 1,
			}, streamqueue.FailureMetadata{
				Classification: management.ClassificationPermanent, Code: "invalid",
			},
		))
	}
	failures, err = worker.ListFailures(t.Context(), management.PageRequest{
		Limit: 10, Sort: management.SortOccurredAt, Direction: management.SortDescending,
	})
	require.NoError(t, err)
	require.Len(t, failures.Items, 2)
	require.NoError(t, worker.Shutdown())
}

func TestValkey9AuthenticationAndTLS(t *testing.T) {
	t.Run("authentication", func(t *testing.T) {
		const secret = "integration-secret"
		_, endpoint := startValkey9(t, []string{
			"valkey-server", "--appendonly", "no", "--requirepass", secret,
		}, nil)
		wrong, err := NewWorkerE(
			WithAddress(endpoint), WithAuthentication("default", "wrong-secret"),
			WithDialTimeout(250*time.Millisecond), WithCommandTimeout(250*time.Millisecond),
		)
		assert.Nil(t, wrong)
		assert.Error(t, err)
		assert.NotContains(t, err.Error(), endpoint)
		assert.NotContains(t, err.Error(), secret)

		worker, err := NewWorkerE(
			WithAddress(endpoint), WithAuthentication("default", secret),
			WithCommandTimeout(time.Second),
		)
		require.NoError(t, err)
		require.NoError(t, worker.Shutdown())
	})

	t.Run("TLS", func(t *testing.T) {
		files, roots := valkeyTLSFiles(t)
		_, endpoint := startValkey9(t, []string{
			"valkey-server", "--port", "0", "--tls-port", "6379",
			"--tls-cert-file", "/tls/server.crt",
			"--tls-key-file", "/tls/server.key",
			"--tls-ca-cert-file", "/tls/ca.crt",
			"--tls-auth-clients", "no", "--appendonly", "no",
		}, files)
		wrong, err := NewWorkerE(
			WithAddress(endpoint), WithTLSConfig(&tls.Config{
				MinVersion: tls.VersionTLS12, RootCAs: x509.NewCertPool(), ServerName: "localhost",
			}),
			WithDialTimeout(250*time.Millisecond), WithCommandTimeout(250*time.Millisecond),
		)
		assert.Nil(t, wrong)
		assert.Error(t, err)
		assert.NotContains(t, err.Error(), endpoint)

		worker, err := NewWorkerE(
			WithAddress(endpoint), WithTLSConfig(&tls.Config{
				MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "localhost",
			}),
			WithCommandTimeout(time.Second),
		)
		require.NoError(t, err)
		require.NoError(t, worker.Shutdown())
	})
}

func TestValkey9CrashRestartAndPendingRecovery(t *testing.T) {
	container, endpoint := startValkey9(t, nil, nil)
	first := newIntegrationWorker(t, endpoint, "restart", "worker-1")
	message := job.NewMessage(integrationMessage("survives-restart"))
	require.NoError(t, first.Queue(&message))
	delivery, err := first.Request()
	require.NoError(t, err)
	require.NoError(t, delivery.(*job.Message).Nack())
	require.NoError(t, first.Shutdown())

	stopTimeout := time.Second
	require.NoError(t, container.Stop(t.Context(), &stopTimeout))
	require.NoError(t, container.Start(t.Context()))
	endpoint = waitForContainerEndpoint(t, container)

	second := newRecoveringIntegrationWorker(t, endpoint, "restart", "worker-2")
	recovered, err := second.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("survives-restart"), recovered.Payload())
	require.NoError(t, recovered.(*job.Message).Ack())
	stats, err := second.Stats(t.Context())
	require.NoError(t, err)
	assert.Zero(t, stats.Depth)
	assert.Equal(t, uint64(1), stats.Reclaimed)
	assert.Equal(t, uint64(1), stats.Retries)
	require.NoError(t, second.Shutdown())
}

func TestValkey9NetworkPartitionPreservesFailedSettlement(t *testing.T) {
	container, endpoint := startValkey9(t, nil, nil)
	worker := newIntegrationWorker(t, endpoint, "partition", "worker-1")
	message := job.NewMessage(integrationMessage("pending-during-partition"))
	require.NoError(t, worker.Queue(&message))
	delivery, err := worker.Request()
	require.NoError(t, err)

	provider, err := testcontainers.NewDockerProvider()
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Close() })
	_, err = provider.Client().ContainerPause(
		t.Context(), container.GetContainerID(), client.ContainerPauseOptions{},
	)
	require.NoError(t, err)
	paused := true
	t.Cleanup(func() {
		if paused {
			_, _ = provider.Client().ContainerUnpause(
				context.Background(), container.GetContainerID(), client.ContainerUnpauseOptions{},
			)
		}
	})

	started := time.Now()
	err = delivery.(*job.Message).Ack()
	assert.Error(t, err)
	assert.Less(t, time.Since(started), time.Second)
	assert.NotContains(t, err.Error(), endpoint)

	_, err = provider.Client().ContainerUnpause(
		t.Context(), container.GetContainerID(), client.ContainerUnpauseOptions{},
	)
	require.NoError(t, err)
	paused = false
	var reconnected Stats
	require.Eventually(t, func() bool {
		var statsErr error
		reconnected, statsErr = worker.Stats(t.Context())
		return statsErr == nil
	}, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, worker.Shutdown())
	if reconnected.Pending == 0 {
		assert.Zero(t, reconnected.Depth)
		return
	}
	assert.Equal(t, int64(1), reconnected.Pending)

	time.Sleep(30 * time.Millisecond)
	replacement := newRecoveringIntegrationWorker(t, endpoint, "partition", "worker-2")
	recovered, err := replacement.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("pending-during-partition"), recovered.Payload())
	require.NoError(t, recovered.(*job.Message).Ack())
	require.NoError(t, replacement.Shutdown())
}

func TestValkey9PoisonAndDeadLetterFailureRecovery(t *testing.T) {
	_, endpoint := startValkey9(t, nil, nil)

	t.Run("malformed envelope", func(t *testing.T) {
		worker := newIntegrationWorker(t, endpoint, "poison", "worker")
		client := integrationClient(t, endpoint)
		result := client.Do(t.Context(), client.B().Xadd().Key("poison").Id("*").
			FieldValue().FieldValue(streamBodyField, "not-json").Build())
		require.NoError(t, result.Error())
		_, err := worker.Request()
		assert.Error(t, err)
		length, err := client.Do(t.Context(), client.B().Xlen().Key("poison-dead").Build()).ToInt64()
		require.NoError(t, err)
		assert.Equal(t, int64(1), length)
		require.NoError(t, worker.Shutdown())
	})

	t.Run("oversized enqueue", func(t *testing.T) {
		worker := newIntegrationWorker(t, endpoint, "oversized", "worker")
		message := job.NewMessage(integrationMessage(bytes.Repeat(
			[]byte("x"), job.DefaultMaxMessageBytes+1,
		)))
		err := worker.Queue(&message)
		assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		require.NoError(t, worker.Shutdown())
	})

	t.Run("dead letter backend failure", func(t *testing.T) {
		first := newIntegrationWorker(t, endpoint, "dead-letter", "worker-1")
		message := job.NewMessage(integrationMessage("terminal"))
		require.NoError(t, first.Queue(&message))
		delivery, err := first.Request()
		require.NoError(t, err)
		require.NoError(t, delivery.(*job.Message).Nack())
		require.NoError(t, first.Shutdown())

		client := integrationClient(t, endpoint)
		require.NoError(t, client.Do(t.Context(), client.B().Set().Key("dead-letter-dead").Value("wrong-type").Build()).Error())
		time.Sleep(30 * time.Millisecond)
		second := newRecoveringIntegrationWorker(t, endpoint, "dead-letter", "worker-2")
		terminal, err := second.Request()
		require.NoError(t, err)
		err = terminal.(*job.Message).Nack()
		assert.Error(t, err)
		assert.NotContains(t, err.Error(), "wrong-type")
		require.NoError(t, second.Shutdown())

		require.NoError(t, client.Do(t.Context(), client.B().Del().Key("dead-letter-dead").Build()).Error())
		time.Sleep(30 * time.Millisecond)
		third := newRecoveringIntegrationWorker(t, endpoint, "dead-letter", "worker-3")
		terminal, err = third.Request()
		require.NoError(t, err)
		require.NoError(t, terminal.(*job.Message).Nack())
		stats, err := third.Stats(t.Context())
		require.NoError(t, err)
		assert.Zero(t, stats.Pending)
		assert.Equal(t, uint64(1), stats.DeadLettered)
		require.NoError(t, third.Shutdown())
	})
}

func TestValkey9ConcurrentReclaimHasOneOwner(t *testing.T) {
	_, endpoint := startValkey9(t, nil, nil)
	firstClient := integrationClient(t, endpoint)
	secondClient := integrationClient(t, endpoint)
	first := newNativeTransport(firstClient, 100, job.DefaultMaxMessageBytes)
	second := newNativeTransport(secondClient, 100, job.DefaultMaxMessageBytes)
	ctx := t.Context()
	require.NoError(t, first.EnsureGroup(ctx, "race", "workers"))
	_, err := first.Add(ctx, streamqueue.AddRequest{Stream: "race", MaxLength: 100, Body: []byte("payload")})
	require.NoError(t, err)
	deliveries, err := first.Read(ctx, streamqueue.ReadRequest{
		Stream: "race", Group: "workers", Consumer: "owner", Count: 1, Block: time.Millisecond,
	})
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	require.NoError(t, firstClient.Do(ctx, firstClient.B().Xclaim().
		Key("race").Group("workers").Consumer("owner").MinIdleTime("0").
		Id(deliveries[0].ID).Idle(time.Minute.Milliseconds()).Justid().Build()).Error())

	var claimed atomic.Int64
	var group sync.WaitGroup
	group.Add(2)
	for index, transport := range []*nativeTransport{first, second} {
		go func() {
			defer group.Done()
			result, claimErr := transport.Claim(ctx, streamqueue.ClaimRequest{
				Stream: "race", Group: "workers", Consumer: "rescuer-" + string(rune('a'+index)),
				MinIdle: 30 * time.Second, Start: "0-0", Count: 1,
			})
			assert.NoError(t, claimErr)
			claimed.Add(int64(len(result.Deliveries)))
		}()
	}
	group.Wait()
	assert.Equal(t, int64(1), claimed.Load())
}

func TestValkey9HandlerRetryAndPanicRecovery(t *testing.T) {
	_, endpoint := startValkey9(t, nil, nil)

	t.Run("bounded handler retry", func(t *testing.T) {
		var attempts atomic.Int64
		options := integrationWorkerOptions(endpoint, "handler-retry", "worker")
		options = append(options, WithRunFunc(func(context.Context, core.TaskMessage) error {
			if attempts.Add(1) < 3 {
				return errors.New("retry handler")
			}
			return nil
		}))
		worker, err := NewWorkerE(options...)
		require.NoError(t, err)
		q, err := queue.NewQueue(
			queue.WithWorker(worker), queue.WithWorkerCount(1), queue.WithLogger(queue.NewEmptyLogger()),
		)
		require.NoError(t, err)
		q.Start()
		require.NoError(t, q.Queue(integrationMessage("retry"), job.AllowOption{
			RetryCount: job.Int64(2), RetryDelay: job.Time(time.Millisecond),
		}))
		require.Eventually(t, func() bool { return q.SuccessTasks() == 1 }, 5*time.Second, time.Millisecond)
		assert.Equal(t, int64(3), attempts.Load())
		q.Release()
	})

	t.Run("handler panic is dead-lettered", func(t *testing.T) {
		panicked := make(chan struct{}, 1)
		options := integrationWorkerOptions(endpoint, "handler-panic", "worker-1")
		options = append(options, WithRunFunc(func(context.Context, core.TaskMessage) error {
			panicked <- struct{}{}
			panic("poison handler")
		}))
		worker, err := NewWorkerE(options...)
		require.NoError(t, err)
		q, err := queue.NewQueue(
			queue.WithWorker(worker), queue.WithWorkerCount(1), queue.WithLogger(queue.NewEmptyLogger()),
		)
		require.NoError(t, err)
		q.Start()
		require.NoError(t, q.Queue(integrationMessage("panic")))
		select {
		case <-panicked:
		case <-time.After(5 * time.Second):
			t.Fatal("handler did not panic")
		}
		require.Eventually(t, func() bool { return q.FailureTasks() == 1 }, 5*time.Second, time.Millisecond)
		stats, err := worker.Stats(t.Context())
		require.NoError(t, err)
		assert.Zero(t, stats.Pending)
		assert.Equal(t, uint64(1), stats.DeadLettered)
		client := integrationClient(t, endpoint)
		length, err := client.Do(t.Context(), client.B().Xlen().Key("handler-panic-dead").Build()).AsInt64()
		require.NoError(t, err)
		assert.Equal(t, int64(1), length)
		q.Release()
	})
}

func startValkey9(
	t *testing.T, command []string, files []testcontainers.ContainerFile,
) (testcontainers.Container, string) {
	t.Helper()
	if command == nil {
		command = []string{"valkey-server", "--appendonly", "yes"}
	}

	var lastEndpointErr error
	for range 3 {
		container, err := testcontainers.GenericContainer(t.Context(), testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image: valkey9Image, ExposedPorts: []string{"6379/tcp"}, Cmd: command, Files: files,
				WaitingFor: wait.ForLog("Ready to accept connections").WithStartupTimeout(2 * time.Minute),
			},
			Started: true,
		})
		require.NoError(t, err)

		endpoint, endpointErr := container.PortEndpoint(t.Context(), "6379/tcp", "")
		if endpointErr == nil && endpoint != "" {
			t.Cleanup(func() { testcontainers.CleanupContainer(t, container) })
			return container, endpoint
		}
		if endpointErr == nil {
			endpointErr = errors.New("container returned an empty endpoint")
		}
		lastEndpointErr = endpointErr
		require.NoError(t, container.Terminate(t.Context()))
	}

	require.NoError(t, lastEndpointErr, "Valkey container did not publish port 6379/tcp")
	return nil, ""
}

func waitForContainerEndpoint(t *testing.T, container testcontainers.Container) string {
	t.Helper()
	var endpoint string
	var endpointErr error
	require.Eventually(t, func() bool {
		endpoint, endpointErr = container.PortEndpoint(t.Context(), "6379/tcp", "")
		return endpointErr == nil && endpoint != ""
	}, 2*time.Minute, 100*time.Millisecond, "container did not publish its endpoint")
	require.NoError(t, endpointErr)
	return endpoint
}

func integrationWorkerOptions(endpoint, stream, consumer string) []Option {
	return []Option{
		WithAddress(endpoint), WithStreamName(stream), WithGroup("workers"),
		WithConsumer(consumer), WithBlockTime(20 * time.Millisecond),
		WithRequestTimeout(5 * time.Second), WithCommandTimeout(250 * time.Millisecond),
		WithDialTimeout(250 * time.Millisecond), WithShutdownTimeout(time.Second),
		WithReclaim(time.Hour, time.Hour, 8), WithFailureStream(stream + "-failures"),
		WithDeadLetter(stream+"-dead", 2),
		WithLogger(queue.NewEmptyLogger()),
	}
}

func newIntegrationWorker(t *testing.T, endpoint, stream, consumer string) *Worker {
	t.Helper()
	worker, err := NewWorkerE(integrationWorkerOptions(endpoint, stream, consumer)...)
	require.NoError(t, err)
	return worker
}

func newRecoveringIntegrationWorker(t *testing.T, endpoint, stream, consumer string) *Worker {
	t.Helper()
	options := integrationWorkerOptions(endpoint, stream, consumer)
	options = append(options, WithReclaim(20*time.Millisecond, 5*time.Millisecond, 8))
	var worker *Worker
	require.Eventually(t, func() bool {
		var err error
		worker, err = NewWorkerE(options...)
		return err == nil
	}, 5*time.Second, 20*time.Millisecond, "Valkey did not accept connections after restart")
	return worker
}

func integrationClient(t *testing.T, endpoint string) valkey.Client {
	t.Helper()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{endpoint}, ForceSingleClient: true,
		DisableCache: true, DisableRetry: true, AlwaysPipelining: true,
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func valkeyTLSFiles(t *testing.T) ([]testcontainers.ContainerFile, *x509.CertPool) {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "queue test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	serverDER, err := x509.CreateCertificate(
		rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey,
	)
	require.NoError(t, err)
	serverPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(caPEM))
	return []testcontainers.ContainerFile{
		{Reader: bytes.NewReader(caPEM), ContainerFilePath: "/tls/ca.crt", FileMode: 0o644},
		{Reader: bytes.NewReader(serverPEM), ContainerFilePath: "/tls/server.crt", FileMode: 0o644},
		{Reader: bytes.NewReader(keyPEM), ContainerFilePath: "/tls/server.key", FileMode: 0o644},
	}, roots
}
