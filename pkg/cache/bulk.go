package cache

import "context"

// Entry pairs a logical key with a value for SetMany.
type Entry[K, V any] struct {
	Key   K
	Value V
}

// BulkResult reports one GetMany result without flattening per-key errors.
type BulkResult[K, V any] struct {
	Key K
	Result[V]
	Err error
}

// MutationResult reports one bulk mutation error in input order.
type MutationResult[K any] struct {
	Key K
	Err error
}

// GetMany reads keys in input order and records per-key failures in the result.
func (c *Cache[K, V]) GetMany(ctx context.Context, keys []K) ([]BulkResult[K, V], error) {
	if err := c.validateBatch(len(keys)); err != nil {
		return nil, err
	}
	results := make([]BulkResult[K, V], len(keys))
	for index, key := range keys {
		result, err := c.Get(ctx, key)
		results[index] = BulkResult[K, V]{Key: key, Result: result, Err: err}
	}
	return results, nil
}

// SetMany writes entries in input order and records per-key failures.
func (c *Cache[K, V]) SetMany(ctx context.Context, entries []Entry[K, V]) ([]MutationResult[K], error) {
	if err := c.validateBatch(len(entries)); err != nil {
		return nil, err
	}
	results := make([]MutationResult[K], len(entries))
	for index, entry := range entries {
		results[index] = MutationResult[K]{Key: entry.Key, Err: c.Set(ctx, entry.Key, entry.Value)}
	}
	return results, nil
}

// DeleteMany deletes keys in input order and records per-key failures.
func (c *Cache[K, V]) DeleteMany(ctx context.Context, keys []K) ([]MutationResult[K], error) {
	if err := c.validateBatch(len(keys)); err != nil {
		return nil, err
	}
	results := make([]MutationResult[K], len(keys))
	for index, key := range keys {
		results[index] = MutationResult[K]{Key: key, Err: c.Delete(ctx, key)}
	}
	return results, nil
}

func (c *Cache[K, V]) validateBatch(size int) error {
	if size > c.maxBatch {
		return ErrBatchTooLarge
	}
	return nil
}
