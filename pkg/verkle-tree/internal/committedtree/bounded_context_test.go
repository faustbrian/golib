package committedtree

import (
	"context"
	"testing"
	"time"
)

const testOperationTimeout = 5 * time.Second

func boundedTestContext(t testing.TB) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testOperationTimeout)
	t.Cleanup(cancel)

	return ctx
}
