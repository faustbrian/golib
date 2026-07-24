package cache_test

import (
	"context"
	"fmt"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
	"github.com/faustbrian/golib/pkg/cache/backend/memory"
)

func ExampleCache_GetOrLoad() {
	backend, _ := memory.New(memory.Config{
		MaxEntries: 100,
		MaxBytes:   1 << 20,
		Clock:      cache.SystemClock{},
	})
	keys, _ := cache.NewKeySpace("example", "greeting", 1, cache.StringKeyEncoder{}, 128)
	store, _ := cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     keys,
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    cache.SystemClock{},
		MaxValue: 1024,
	})
	defer func() { _ = store.Close() }()

	result, err := store.GetOrLoad(context.Background(), "hello",
		func(context.Context, string) (cache.LoadResult[string], error) {
			return cache.LoadResult[string]{Value: "world", Found: true}, nil
		})
	fmt.Println(result.State == cache.Hit, result.Value, err)
	// Output: true world <nil>
}
