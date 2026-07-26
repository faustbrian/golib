package redisdb

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingLogger struct {
	output strings.Builder
}

func (l *recordingLogger) Infof(format string, args ...any) {
	_, _ = fmt.Fprintf(&l.output, format, args...)
}
func (l *recordingLogger) Errorf(format string, args ...any) {
	_, _ = fmt.Fprintf(&l.output, format, args...)
}
func (l *recordingLogger) Fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(&l.output, format, args...)
}
func (l *recordingLogger) Info(args ...any)  { _, _ = fmt.Fprint(&l.output, args...) }
func (l *recordingLogger) Error(args ...any) { _, _ = fmt.Fprint(&l.output, args...) }
func (l *recordingLogger) Fatal(args ...any) { _, _ = fmt.Fprint(&l.output, args...) }

func TestOptionsConfigureRedisPubSub(t *testing.T) {
	logger := queue.NewEmptyLogger()
	runErr := errors.New("run")
	opts := newOptions(
		WithAddr("redis:6379"),
		WithDB(2),
		WithCluster(),
		WithSentinel(),
		WithTLS(),
		WithSkipTLSVerify(),
		WithMasterName("primary"),
		WithChannelSize(42),
		WithUsername("user"),
		WithPassword("secret"),
		WithConnectionString("redis://redis:6379/2"),
		WithChannel("jobs"),
		WithRunFunc(func(context.Context, core.TaskMessage) error { return runErr }),
		WithLogger(logger),
		WithDebug(),
		WithConnectTimeout(25*time.Millisecond),
	)

	assert.Equal(t, "redis:6379", opts.addr)
	assert.Equal(t, 2, opts.db)
	assert.True(t, opts.cluster)
	assert.True(t, opts.sentinel)
	assert.Equal(t, "primary", opts.masterName)
	assert.Equal(t, 42, opts.channelSize)
	assert.Equal(t, "user", opts.username)
	assert.Equal(t, "secret", opts.password)
	assert.Equal(t, "redis://redis:6379/2", opts.connectionString)
	assert.Equal(t, "jobs", opts.channelName)
	assert.True(t, opts.debug)
	assert.Equal(t, logger, opts.logger)
	assert.Equal(t, 25*time.Millisecond, opts.connectTimeout)
	assert.Equal(t, uint16(tls.VersionTLS12), opts.tls.MinVersion)
	assert.True(t, opts.tls.InsecureSkipVerify)
	assert.ErrorIs(t, opts.runFunc(context.Background(), nil), runErr)
	worker := &Worker{opts: opts}
	assert.Equal(t, "redis-pubsub", worker.BackendName())
	assert.Equal(t, "jobs", worker.QueueName())
}

func TestDefaultRunFunctionSucceeds(t *testing.T) {
	assert.NoError(t, newOptions().runFunc(context.Background(), nil))
}

func TestSkipTLSVerifyCreatesConfig(t *testing.T) {
	opts := newOptions(WithSkipTLSVerify())

	assert.True(t, opts.tls.InsecureSkipVerify)
}

func TestWorkerPublishesRunsReceivesAndShutsDown(t *testing.T) {
	server := miniredis.RunT(t)
	var handled []byte
	worker, err := NewWorkerE(
		WithAddr(server.Addr()),
		WithChannel("jobs"),
		WithChannelSize(2),
		WithRunFunc(func(_ context.Context, task core.TaskMessage) error {
			handled = append([]byte(nil), task.Payload()...)
			return nil
		}),
	)
	require.NoError(t, err)
	message := job.NewMessage(rawMessage("payload"))

	require.NoError(t, worker.Queue(&message))
	received, err := worker.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), received.Payload())
	require.NoError(t, worker.Run(context.Background(), received))
	assert.Equal(t, []byte("payload"), handled)
	require.NoError(t, worker.Shutdown())
	assert.ErrorIs(t, worker.Shutdown(), queue.ErrQueueShutdown)
	assert.ErrorIs(t, worker.Queue(&message), queue.ErrQueueShutdown)
}

func TestWorkerConnectsWithConnectionString(t *testing.T) {
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(WithConnectionString("redis://" + server.Addr() + "/0"))
	require.NoError(t, err)
	require.NoError(t, worker.Shutdown())
}

func TestLegacyConstructorReturnsConnectedWorker(t *testing.T) {
	server := miniredis.RunT(t)
	worker := NewWorker(WithAddr(server.Addr()))

	require.NoError(t, worker.Shutdown())
}

func TestWorkerDebugModeConnects(t *testing.T) {
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(WithAddr(server.Addr()), WithDebug())

	require.NoError(t, err)
	require.NoError(t, worker.Shutdown())
}

func TestWorkerDebugModeDoesNotExposeCredentials(t *testing.T) {
	logger := &recordingLogger{}
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	originalStdout := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = originalStdout })

	worker, constructorErr := NewWorkerE(
		WithConnectionString("redis://audit-user:audit-password@127.0.0.1:1/0"),
		WithLogger(logger),
		WithDebug(),
		WithConnectTimeout(time.Millisecond),
	)
	assert.Nil(t, worker)
	assert.Error(t, constructorErr)
	require.NoError(t, writer.Close())
	output, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	combinedOutput := string(output) + logger.output.String()
	assert.NotContains(t, combinedOutput, "audit-user")
	assert.NotContains(t, combinedOutput, "audit-password")
	assert.Contains(t, combinedOutput, "redis-pubsub")
}

func TestWorkerConstructorReturnsModeConnectionErrors(t *testing.T) {
	t.Run("cluster", func(t *testing.T) {
		worker, err := NewWorkerE(
			WithAddr("127.0.0.1:1"),
			WithCluster(),
			WithConnectTimeout(20*time.Millisecond),
		)
		assert.Nil(t, worker)
		assert.ErrorContains(t, err, "connect to Redis")
	})

	t.Run("sentinel", func(t *testing.T) {
		worker, err := NewWorkerE(
			WithAddr("127.0.0.1:1"),
			WithSentinel(),
			WithMasterName("primary"),
			WithConnectTimeout(20*time.Millisecond),
		)
		assert.Nil(t, worker)
		assert.ErrorContains(t, err, "connect to Redis")
	})

	t.Run("sentinel protocol failure", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				connection, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				_ = connection.Close()
			}
		}()
		t.Cleanup(func() {
			require.NoError(t, listener.Close())
			<-done
		})

		worker, err := NewWorkerE(
			WithAddr(listener.Addr().String()),
			WithSentinel(),
			WithMasterName("primary"),
			WithConnectTimeout(20*time.Millisecond),
		)
		assert.Nil(t, worker)
		assert.ErrorContains(t, err, "connect to Redis")
	})

	t.Run("sentinel master unavailable", func(t *testing.T) {
		worker, err := NewWorkerE(
			WithAddr(startSentinelStub(t)),
			WithSentinel(),
			WithMasterName("primary"),
			WithConnectTimeout(20*time.Millisecond),
		)
		assert.Nil(t, worker)
		assert.ErrorContains(t, err, "connect to Redis")
	})
}

func TestProbeRedisSentinels(t *testing.T) {
	t.Run("accepts a reachable fallback", func(t *testing.T) {
		address := startSentinelStub(t)
		err := probeRedisSentinels(
			t.Context(), []string{"127.0.0.1:1", address}, "primary", nil, time.Second,
		)
		require.NoError(t, err)
	})

	t.Run("reports unavailable endpoints", func(t *testing.T) {
		err := probeRedisSentinels(
			t.Context(), []string{"127.0.0.1:1"}, "primary", nil, 20*time.Millisecond,
		)
		assert.Error(t, err)
	})

	t.Run("rejects an empty endpoint list", func(t *testing.T) {
		assert.Error(t, probeRedisSentinels(t.Context(), nil, "primary", nil, time.Second))
		assert.Error(t, probeRedisSentinels(
			t.Context(), []string{"127.0.0.1:1"}, "primary", nil, 0,
		))
	})

	t.Run("honors caller cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := probeRedisSentinels(
			ctx, []string{"192.0.2.1:6379"}, "primary", nil, time.Second,
		)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func startSentinelStub(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = connection.Close() }()
		reader := bufio.NewReader(connection)
		for range 5 {
			if _, readErr := reader.ReadString('\n'); readErr != nil {
				return
			}
		}
		if _, writeErr := io.WriteString(connection, "-ERR unknown command 'hello'\r\n"); writeErr != nil {
			return
		}
		for range 7 {
			if _, readErr := reader.ReadString('\n'); readErr != nil {
				return
			}
		}
		_, _ = io.WriteString(
			connection,
			"*2\r\n$9\r\n127.0.0.1\r\n$4\r\n6379\r\n",
		)
	}()
	t.Cleanup(func() {
		require.NoError(t, listener.Close())
		<-done
	})
	return listener.Addr().String()
}

func TestLegacyConstructorPanicsOnConnectionError(t *testing.T) {
	assert.Panics(t, func() {
		NewWorker(WithAddr("127.0.0.1:1"), WithConnectTimeout(20*time.Millisecond))
	})
}

func TestRequestReturnsDecodeAndClosedChannelErrors(t *testing.T) {
	t.Run("decode", func(t *testing.T) {
		messages := make(chan *redis.Message, 1)
		messages <- &redis.Message{Payload: "not-json"}
		worker := &Worker{channel: messages, opts: newOptions()}

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.Error(t, err)
	})

	t.Run("oversized", func(t *testing.T) {
		messages := make(chan *redis.Message, 1)
		messages <- &redis.Message{Payload: strings.Repeat("x", job.DefaultMaxMessageBytes+1)}
		worker := &Worker{channel: messages, opts: newOptions()}

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorIs(t, err, job.ErrMessageTooLarge)
	})

	t.Run("closed", func(t *testing.T) {
		messages := make(chan *redis.Message)
		close(messages)
		worker := &Worker{channel: messages, opts: newOptions()}

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorIs(t, err, queue.ErrQueueHasBeenClosed)
	})
}

func TestRequestUsesConfiguredTimeout(t *testing.T) {
	worker := &Worker{
		channel: make(chan *redis.Message),
		opts:    newOptions(WithRequestTimeout(time.Millisecond)),
	}

	started := time.Now()
	message, err := worker.Request()

	assert.Nil(t, message)
	assert.ErrorIs(t, err, queue.ErrNoTaskInQueue)
	assert.Less(t, time.Since(started), 100*time.Millisecond)
}

func TestClusterWorkerShutdownClosesResources(t *testing.T) {
	client := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}})
	worker := &Worker{
		rdb:    client,
		pubsub: client.Subscribe(context.Background(), "jobs"),
		stop:   make(chan struct{}),
		opts:   newOptions(),
	}

	require.NoError(t, worker.Shutdown())
}

func TestSubscribeRedisSupportsStandaloneAndClusterClients(t *testing.T) {
	ctx := context.Background()
	standalone := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	cluster := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}})
	ring := redis.NewRing(&redis.RingOptions{
		Addrs: map[string]string{"local": "127.0.0.1:1"},
	})

	standaloneSubscription := subscribeRedis(ctx, standalone, "jobs")
	clusterSubscription := subscribeRedis(ctx, cluster, "jobs")

	assert.NotNil(t, standaloneSubscription)
	assert.NotNil(t, clusterSubscription)
	assert.Nil(t, subscribeRedis(ctx, ring, "jobs"))
	require.NoError(t, standaloneSubscription.Close())
	require.NoError(t, clusterSubscription.Close())
	require.NoError(t, standalone.Close())
	require.NoError(t, cluster.Close())
	require.NoError(t, ring.Close())
}

func TestWorkerReturnsSubscriptionValidationError(t *testing.T) {
	server := miniredis.RunT(t)
	expected := errors.New("subscription unavailable")
	original := pingRedisSubscription
	pingRedisSubscription = func(context.Context, *redis.PubSub) error {
		return expected
	}
	t.Cleanup(func() { pingRedisSubscription = original })

	worker, err := NewWorkerE(WithAddr(server.Addr()))

	assert.Nil(t, worker)
	assert.ErrorIs(t, err, expected)
}

func TestQueueReturnsPublishError(t *testing.T) {
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(WithAddr(server.Addr()))
	require.NoError(t, err)
	require.NoError(t, worker.rdb.(*redis.Client).Close())
	message := job.NewMessage(rawMessage("payload"))

	assert.Error(t, worker.Queue(&message))
	require.NoError(t, worker.Shutdown())
}

type rawMessage string

func (m rawMessage) Bytes() []byte { return []byte(m) }
