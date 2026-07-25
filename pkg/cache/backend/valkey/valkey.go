package valkey

import (
	"context"
	"strconv"
	"strings"
	"time"

	valkeyclient "github.com/valkey-io/valkey-go"

	cache "github.com/faustbrian/golib/pkg/cache"
	"github.com/faustbrian/golib/pkg/cache/internal/wire"
)

const (
	oversizedReply = "GOCACHE_RECORD_TOO_LARGE"
	boundedGet     = `
local size = redis.call('STRLEN', KEYS[1])
if size == 0 and redis.call('EXISTS', KEYS[1]) == 0 then
  return nil
end
if size > tonumber(ARGV[1]) then
  return redis.error_reply('GOCACHE_RECORD_TOO_LARGE')
end
return redis.call('GET', KEYS[1])
`
)

// Config supplies the native client, clock, and maximum wire record size.
type Config struct {
	Client        valkeyclient.CommandClient
	Clock         cache.Clock
	MaxRecordSize int
}

// Backend implements cache.Backend with valkey-go.
type Backend struct {
	client        valkeyclient.CommandClient
	clock         cache.Clock
	maxRecordSize int
}

// New validates config and constructs a Valkey backend.
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
	command := b.client.B().Eval().Script(boundedGet).Numkeys(1).Key(key).
		Arg(strconv.Itoa(b.maxRecordSize)).Build()
	value, err := b.client.Do(ctx, command).ToString()
	if valkeyclient.IsValkeyNil(err) {
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
	value := valkeyclient.BinaryString(encoded)
	set := b.client.B().Set().Key(key).Value(value)
	var command valkeyclient.Completed
	switch condition {
	case cache.Unconditional:
		command = set.Px(ttl).Build()
	case cache.IfAbsent:
		command = set.Nx().Px(ttl).Build()
	case cache.IfPresent:
		command = set.Xx().Px(ttl).Build()
	default:
		return false, cache.ErrInvalidPolicy
	}
	err = b.client.Do(ctx, command).Error()
	if valkeyclient.IsValkeyNil(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Delete removes key and reports whether Valkey deleted it.
func (b *Backend) Delete(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	deleted, err := b.client.Do(ctx, b.client.B().Del().Key(key).Build()).ToInt64()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}
