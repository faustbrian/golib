package redis

import (
	"context"
	"errors"
	"strings"
	"time"

	redisclient "github.com/redis/go-redis/v9"

	cache "github.com/faustbrian/golib/pkg/cache"
	"github.com/faustbrian/golib/pkg/cache/internal/wire"
)

const oversizedReply = "GOCACHE_RECORD_TOO_LARGE"

var boundedGet = redisclient.NewScript(`
local size = redis.call('STRLEN', KEYS[1])
if size == 0 and redis.call('EXISTS', KEYS[1]) == 0 then
  return nil
end
if size > tonumber(ARGV[1]) then
  return redis.error_reply('GOCACHE_RECORD_TOO_LARGE')
end
return redis.call('GET', KEYS[1])
`)

// Config supplies the native client, clock, and maximum wire record size.
type Config struct {
	Client        redisclient.UniversalClient
	Clock         cache.Clock
	MaxRecordSize int
}

// Backend implements cache.Backend with go-redis/v9.
type Backend struct {
	client        redisclient.UniversalClient
	clock         cache.Clock
	maxRecordSize int
}

// New validates config and constructs a Redis backend.
func New(config Config) (*Backend, error) {
	if config.Client == nil || config.Clock == nil || config.MaxRecordSize <= 0 {
		return nil, cache.ErrInvalidConfig
	}
	return &Backend{
		client:        config.Client,
		clock:         config.Clock,
		maxRecordSize: config.MaxRecordSize,
	}, nil
}

// Get reads and validates one bounded wire record.
func (b *Backend) Get(ctx context.Context, key string) (cache.Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return cache.Record{}, false, err
	}
	value, err := boundedGet.Run(ctx, b.client, []string{key}, b.maxRecordSize).Text()
	if errors.Is(err, redisclient.Nil) {
		return cache.Record{}, false, nil
	}
	if err != nil {
		if strings.Contains(err.Error(), oversizedReply) {
			return cache.Record{}, false, cache.ErrValueTooLarge
		}
		return cache.Record{}, false, err
	}
	record, err := wire.Decode([]byte(value), b.maxRecordSize)
	if err != nil {
		return cache.Record{}, false, err
	}
	return record, true, nil
}

// Set atomically writes a record with its stale deadline as server expiry.
func (b *Backend) Set(
	ctx context.Context,
	key string,
	record cache.Record,
	condition cache.Condition,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := record.Validate(); err != nil {
		return false, err
	}
	ttl := record.StaleAt.Round(0).Sub(b.clock.Now().Round(0))
	if ttl <= 0 {
		return false, cache.ErrInvalidTTL
	}
	if ttl < time.Millisecond {
		ttl = time.Millisecond
	}
	encoded, err := wire.Encode(record, b.maxRecordSize)
	if err != nil {
		return false, err
	}
	mode, err := conditionMode(condition)
	if err != nil {
		return false, err
	}
	_, err = b.client.SetArgs(ctx, key, encoded, redisclient.SetArgs{
		Mode: mode,
		TTL:  ttl,
	}).Result()
	if errors.Is(err, redisclient.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Delete removes key and reports whether Redis deleted it.
func (b *Backend) Delete(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	deleted, err := b.client.Del(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func conditionMode(condition cache.Condition) (string, error) {
	switch condition {
	case cache.Unconditional:
		return "", nil
	case cache.IfAbsent:
		return "NX", nil
	case cache.IfPresent:
		return "XX", nil
	default:
		return "", cache.ErrInvalidPolicy
	}
}
