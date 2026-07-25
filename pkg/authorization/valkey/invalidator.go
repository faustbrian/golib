// Package valkey distributes monotonic policy revision invalidations through
// Valkey without relying on lossy pub/sub delivery for correctness.
package valkey

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	native "github.com/valkey-io/valkey-go"
)

const (
	DefaultPrefix       = "authorization"
	DefaultPollInterval = 30 * time.Second
)

var (
	ErrNilClient           = errors.New("authorization valkey client is nil")
	ErrInvalidPrefix       = errors.New("authorization valkey prefix is invalid")
	ErrInvalidPollInterval = errors.New("authorization valkey poll interval is invalid")
	ErrInvalidRevision     = errors.New("authorization valkey revision is invalid")
	ErrInvalidResponse     = errors.New("authorization valkey response is invalid")
	ErrNilHandler          = errors.New("authorization valkey handler is nil")
)

const publishScript = `local current = redis.call('GET', KEYS[1])
local next = tonumber(ARGV[2])
if current and tonumber(current) >= next then
    return 0
end
redis.call('SET', KEYS[1], ARGV[2])
redis.call('PUBLISH', ARGV[1], ARGV[2])
return 1`

type Options struct {
	Prefix       string
	PollInterval time.Duration
}

type Invalidator struct {
	client       native.Client
	key          string
	channel      string
	pollInterval time.Duration
}

func New(client native.Client, options Options) (*Invalidator, error) {
	if client == nil {
		return nil, ErrNilClient
	}
	if options.Prefix == "" {
		options.Prefix = DefaultPrefix
	}
	if strings.TrimSpace(options.Prefix) == "" {
		return nil, ErrInvalidPrefix
	}
	if options.PollInterval == 0 {
		options.PollInterval = DefaultPollInterval
	}
	if options.PollInterval < 0 {
		return nil, ErrInvalidPollInterval
	}
	return &Invalidator{
		client:       client,
		key:          options.Prefix + ":revision",
		channel:      options.Prefix + ":invalidate",
		pollInterval: options.PollInterval,
	}, nil
}

// Publish advances the durable invalidation revision and publishes a wakeup
// atomically. It returns false when the same or a newer revision already exists.
func (invalidator *Invalidator) Publish(
	ctx context.Context,
	revision authorization.Revision,
) (bool, error) {
	if revision == 0 {
		return false, ErrInvalidRevision
	}
	encoded := strconv.FormatUint(uint64(revision), 10)
	result, err := invalidator.client.Do(
		ctx,
		invalidator.client.B().Eval().Script(publishScript).Numkeys(1).
			Key(invalidator.key).Arg(invalidator.channel, encoded).Build(),
	).ToInt64()
	if err != nil {
		return false, err
	}
	if result != 0 && result != 1 {
		return false, ErrInvalidResponse
	}
	return result == 1, nil
}

// Revision reads the durable invalidation revision. A missing key means no
// policy has been published through this invalidator yet.
func (invalidator *Invalidator) Revision(
	ctx context.Context,
) (authorization.Revision, error) {
	encoded, err := invalidator.client.Do(
		ctx,
		invalidator.client.B().Get().Key(invalidator.key).Build(),
	).ToString()
	if native.IsValkeyNil(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseUint(encoded, 10, 64)
	if err != nil || parsed == 0 {
		return 0, ErrInvalidRevision
	}
	return authorization.Revision(parsed), nil
}

// Watch observes newer revisions. Pub/sub only wakes the watcher; every
// notification is verified against the durable key, and periodic polling
// continues if pub/sub disconnects or drops messages.
func (invalidator *Invalidator) Watch(
	ctx context.Context,
	after authorization.Revision,
	handler func(authorization.Revision) error,
) error {
	if handler == nil {
		return ErrNilHandler
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	watchCtx, cancel := context.WithCancel(ctx)
	wakeup := make(chan struct{}, 1)
	receiveDone := make(chan error, 1)
	subscriptionDone := make(chan struct{})
	go func() {
		defer close(subscriptionDone)
		receiveDone <- invalidator.client.Receive(
			watchCtx,
			invalidator.client.B().Subscribe().Channel(invalidator.channel).Build(),
			func(native.PubSubMessage) {
				select {
				case wakeup <- struct{}{}:
				default:
				}
			},
		)
	}()
	defer func() {
		cancel()
		<-subscriptionDone
	}()

	ticker := time.NewTicker(invalidator.pollInterval)
	defer ticker.Stop()
	observe := func() error {
		revision, err := invalidator.Revision(watchCtx)
		if err != nil {
			return err
		}
		if revision <= after {
			return nil
		}
		if err := handler(revision); err != nil {
			return err
		}
		after = revision
		return nil
	}
	if err := observe(); err != nil {
		return err
	}

	for {
		select {
		case <-watchCtx.Done():
			return watchCtx.Err()
		case <-ticker.C:
			if err := observe(); err != nil {
				return err
			}
		case <-wakeup:
			if err := observe(); err != nil {
				return err
			}
		case <-receiveDone:
			receiveDone = nil
		}
	}
}
