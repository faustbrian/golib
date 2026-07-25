package cachetest

import (
	"context"
	"testing"

	cache "github.com/faustbrian/golib/pkg/cache"
)

func TestUnavailableCheckRejectsFlattenedErrors(t *testing.T) {
	t.Parallel()

	if err := checkUnavailable(flatteningBackend{}); err == nil {
		t.Fatal("outage flattened into misses passed conformance")
	}
}

type flatteningBackend struct{}

func (flatteningBackend) Get(context.Context, string) (cache.Record, bool, error) {
	return cache.Record{}, false, nil
}

func (flatteningBackend) Set(context.Context, string, cache.Record, cache.Condition) (bool, error) {
	return false, nil
}

func (flatteningBackend) Delete(context.Context, string) (bool, error) {
	return false, nil
}
