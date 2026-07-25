package redisdb

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/safeerr"
	"github.com/faustbrian/golib/pkg/queue/job"

	"github.com/redis/go-redis/v9"
)

var _ core.Worker = (*Worker)(nil)
var _ core.WorkerMetadata = (*Worker)(nil)

var pingRedisSubscription = func(ctx context.Context, subscription *redis.PubSub) error {
	_, err := subscription.Receive(ctx)
	return err
}

// BackendName identifies Redis Pub/Sub in lifecycle events.
func (*Worker) BackendName() string { return "redis-pubsub" }

// QueueName returns the configured Redis channel.
func (w *Worker) QueueName() string { return w.opts.channelName }

// Worker for Redis
type Worker struct {
	// redis config
	rdb      redis.Cmdable
	pubsub   *redis.PubSub
	channel  <-chan *redis.Message
	stopFlag int32
	stopOnce sync.Once
	stop     chan struct{}
	opts     options
}

// NewWorker creates a new Worker instance with the provided options.
// It initializes a Redis client based on the options and establishes a connection to the Redis server.
// The Worker is responsible for subscribing to a Redis channel and receiving messages from it.
// It returns the created Worker instance.
func NewWorker(opts ...Option) *Worker {
	w, err := NewWorkerE(opts...)
	if err != nil {
		panic(err)
	}

	return w
}

// NewWorkerE creates a worker and returns connection and configuration errors.
func NewWorkerE(opts ...Option) (*Worker, error) {
	var err error
	w := &Worker{
		opts: newOptions(opts...),
		stop: make(chan struct{}),
	}

	if w.opts.debug {
		w.opts.logger.Infof(
			"redis-pubsub debug: cluster=%t sentinel=%t tls=%t channel_size=%d request_timeout=%s connect_timeout=%s",
			w.opts.cluster,
			w.opts.sentinel,
			w.opts.tls != nil,
			w.opts.channelSize,
			w.opts.requestTimeout,
			w.opts.connectTimeout,
		)
	}

	options := &redis.Options{
		Addr:                  w.opts.addr,
		Username:              w.opts.username,
		Password:              w.opts.password,
		DB:                    w.opts.db,
		TLSConfig:             w.opts.tls,
		DialTimeout:           w.opts.connectTimeout,
		DialerRetries:         1,
		MaxRetries:            -1,
		ContextTimeoutEnabled: true,
	}

	if w.opts.connectionString != "" {
		options, err = redis.ParseURL(w.opts.connectionString)
		if err != nil {
			return nil, safeerr.Wrap("parse Redis connection string", err)
		}
		options.DialTimeout = w.opts.connectTimeout
		options.DialerRetries = 1
		options.MaxRetries = -1
		options.ContextTimeoutEnabled = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), w.opts.connectTimeout)
	defer cancel()
	if w.opts.sentinel {
		if err = probeRedisSentinels(
			ctx,
			strings.Split(w.opts.addr, ","),
			w.opts.masterName,
			w.opts.tls,
			w.opts.connectTimeout,
		); err != nil {
			return nil, safeerr.Wrap("connect to Redis Sentinel", err)
		}
	}

	switch {
	case w.opts.sentinel:
		w.rdb = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:            w.opts.masterName,
			SentinelAddrs:         strings.Split(w.opts.addr, ","),
			Username:              w.opts.username,
			Password:              w.opts.password,
			DB:                    w.opts.db,
			TLSConfig:             w.opts.tls,
			DialTimeout:           w.opts.connectTimeout,
			DialerRetries:         1,
			MaxRetries:            -1,
			ContextTimeoutEnabled: true,
		})
	case w.opts.cluster:
		w.rdb = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:                 strings.Split(w.opts.addr, ","),
			Username:              w.opts.username,
			Password:              w.opts.password,
			TLSConfig:             w.opts.tls,
			DialTimeout:           w.opts.connectTimeout,
			DialerRetries:         1,
			MaxRedirects:          -1,
			ContextTimeoutEnabled: true,
		})
	default:
		w.rdb = redis.NewClient(options)
	}

	_, err = w.rdb.Ping(ctx).Result()
	if err != nil {
		closeRedisClient(w.rdb)
		return nil, fmt.Errorf("connect to Redis: %w", err)
	}

	w.pubsub = subscribeRedis(ctx, w.rdb, w.opts.channelName)
	// Wait for Redis to acknowledge SUBSCRIBE before publishers can use the worker.
	if err := pingRedisSubscription(ctx, w.pubsub); err != nil {
		_ = w.pubsub.Close()
		closeRedisClient(w.rdb)
		return nil, fmt.Errorf("subscribe to Redis channel: %w", err)
	}

	var ropts []redis.ChannelOption

	if w.opts.channelSize > 1 {
		ropts = append(ropts, redis.WithChannelSize(w.opts.channelSize))
	}

	w.channel = w.pubsub.Channel(ropts...)

	return w, nil
}

func probeRedisSentinels(
	ctx context.Context,
	addresses []string,
	masterName string,
	tlsConfig *tls.Config,
	timeout time.Duration,
) error {
	if len(addresses) == 0 || timeout <= 0 {
		return errors.New("redis Sentinel probe requires an address and timeout")
	}
	attemptTimeout := max(timeout/time.Duration(len(addresses)), time.Nanosecond)
	var failures []error
	for _, address := range addresses {
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		client := redis.NewSentinelClient(&redis.Options{
			Addr:                  address,
			Protocol:              2,
			TLSConfig:             tlsConfig,
			DialTimeout:           attemptTimeout,
			DialerRetries:         1,
			ReadTimeout:           attemptTimeout,
			WriteTimeout:          attemptTimeout,
			MaxRetries:            -1,
			ContextTimeoutEnabled: true,
			DisableIdentity:       true,
		})
		_, err := client.GetMasterAddrByName(attemptCtx, masterName).Result()
		_ = client.Close()
		cancel()
		if err == nil {
			return nil
		}
		failures = append(failures, err)
		if ctx.Err() != nil {
			failures = append(failures, ctx.Err())
			break
		}
	}
	return errors.Join(failures...)
}

func subscribeRedis(ctx context.Context, client redis.Cmdable, channel string) *redis.PubSub {
	switch value := client.(type) {
	case *redis.Client:
		return value.Subscribe(ctx, channel)
	case *redis.ClusterClient:
		return value.Subscribe(ctx, channel)
	default:
		return nil
	}
}

func closeRedisClient(client redis.Cmdable) {
	switch value := client.(type) {
	case *redis.Client:
		_ = value.Close()
	case *redis.ClusterClient:
		_ = value.Close()
	}
}

// Run to execute new task
func (w *Worker) Run(ctx context.Context, task core.TaskMessage) error {
	return w.opts.runFunc(ctx, task)
}

// Shutdown worker
func (w *Worker) Shutdown() error {
	if !atomic.CompareAndSwapInt32(&w.stopFlag, 0, 1) {
		return queue.ErrQueueShutdown
	}

	w.stopOnce.Do(func() {
		_ = w.pubsub.Close()
		closeRedisClient(w.rdb)
		close(w.stop)
	})
	return nil
}

// Queue send notification to queue
func (w *Worker) Queue(job core.TaskMessage) error {
	if atomic.LoadInt32(&w.stopFlag) == 1 {
		return queue.ErrQueueShutdown
	}

	ctx := context.Background()

	// Publish a message.
	err := w.rdb.Publish(ctx, w.opts.channelName, job.Bytes()).Err()
	if err != nil {
		return err
	}

	return nil
}

// Request a new task
func (w *Worker) Request() (core.TaskMessage, error) {
	timer := time.NewTimer(w.opts.requestTimeout)
	defer timer.Stop()
	select {
	case task, ok := <-w.channel:
		if !ok {
			return nil, queue.ErrQueueHasBeenClosed
		}
		data, err := job.DecodeE([]byte(task.Payload), job.DefaultMaxMessageBytes)
		if err != nil {
			return nil, fmt.Errorf("decode Redis Pub/Sub message: %w", err)
		}
		return data, nil
	case <-timer.C:
		return nil, queue.ErrNoTaskInQueue
	}
}
