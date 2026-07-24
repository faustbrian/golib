package jsonschema

import (
	"context"
	"errors"
	"testing"
)

func TestCallbackPanicContainmentPreservesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	compiler := KeywordCompilerFunc(func(
		context.Context,
		Dialect,
		Value,
	) (KeywordEvaluator, error) {
		cancel()
		panic("must be redacted")
	})

	_, err := callKeywordCompiler(ctx, compiler, Draft202012, Value{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context cancellation", err)
	}
	if errors.Is(err, ErrCallbackPanic) {
		t.Fatalf("cancellation was hidden by callback panic: %v", err)
	}
}
