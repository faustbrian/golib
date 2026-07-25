package redis_test

import (
	"errors"
	"testing"

	redisclient "github.com/redis/go-redis/v9"

	cache "github.com/faustbrian/golib/pkg/cache"
	redisbackend "github.com/faustbrian/golib/pkg/cache/backend/redis"
)

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	client := redisclient.NewClient(&redisclient.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = client.Close() })
	tests := map[string]redisbackend.Config{
		"nil client":   {Clock: cache.SystemClock{}, MaxRecordSize: 128},
		"nil clock":    {Client: client, MaxRecordSize: 128},
		"invalid size": {Client: client, Clock: cache.SystemClock{}},
	}
	for name, config := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := redisbackend.New(config); !errors.Is(err, cache.ErrInvalidConfig) {
				t.Fatalf("New returned %v, want ErrInvalidConfig", err)
			}
		})
	}
}
