package keyphrasetest_test

import (
	"context"
	"errors"
	"io"
	"math"
	"testing"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/keyphrasetest"
)

func TestSourceIsDeterministicFiniteAndCallerIsolated(t *testing.T) {
	t.Parallel()

	input := []byte{1, 2, 3}
	source := keyphrasetest.NewSource(input)
	input[0] = 9
	destination := make([]byte, 2)
	if count, err := source.ReadContext(context.Background(), destination); count != 2 || err != nil || destination[0] != 1 {
		t.Fatalf("first read = %v, %d, %v", destination, count, err)
	}
	if count, err := source.ReadContext(context.Background(), destination); count != 1 || !errors.Is(err, io.ErrUnexpectedEOF) || destination[0] != 3 {
		t.Fatalf("final read = %v, %d, %v", destination, count, err)
	}
}

func TestCounterSourceExercisesUniformRejectionSampling(t *testing.T) {
	t.Parallel()

	selector, err := keyphrase.NewSelector(keyphrasetest.NewCounterSource())
	if err != nil {
		t.Fatalf("NewSelector() error = %v", err)
	}
	counts := make([]uint64, 10)
	for range 10_000 {
		index, selectionErr := selector.Index(context.Background(), uint64(len(counts)))
		if selectionErr != nil {
			t.Fatalf("Index() error = %v", selectionErr)
		}
		counts[index]++
	}

	statistic, err := keyphrasetest.ChiSquared(counts)
	if err != nil {
		t.Fatalf("ChiSquared() error = %v", err)
	}
	// 27.877 is the p=0.001 upper critical value for nine degrees of freedom.
	// This catches obvious selection mistakes without certifying randomness.
	if statistic > 27.877 {
		t.Fatalf("chi-squared = %.3f, counts = %v", statistic, counts)
	}
}

func TestCounterSourceIncrementsExactly(t *testing.T) {
	t.Parallel()

	source := keyphrasetest.NewCounterSource()
	first := make([]byte, 3)
	second := make([]byte, 2)
	if _, err := source.ReadContext(context.Background(), first); err != nil {
		t.Fatalf("first read error = %v", err)
	}
	if _, err := source.ReadContext(context.Background(), second); err != nil {
		t.Fatalf("second read error = %v", err)
	}
	if first[0] != 0 || first[1] != 1 || first[2] != 2 || second[0] != 3 || second[1] != 4 {
		t.Fatalf("counter bytes = %v then %v", first, second)
	}
}

func TestChiSquaredRejectsInvalidSamples(t *testing.T) {
	t.Parallel()

	for _, counts := range [][]uint64{nil, {1}, {0, 0}} {
		if statistic, err := keyphrasetest.ChiSquared(counts); err == nil || !math.IsNaN(statistic) {
			t.Fatalf("ChiSquared(%v) = %f, %v", counts, statistic, err)
		}
	}
	statistic, err := keyphrasetest.ChiSquared([]uint64{10, 20})
	if err != nil || math.Abs(statistic-(10.0/3.0)) > 1e-12 {
		t.Fatalf("ChiSquared([10 20]) = %.12f, %v", statistic, err)
	}
}

func TestSourcesHonorCancellationAndExhaustion(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if count, err := keyphrasetest.NewSource([]byte{1}).ReadContext(ctx, make([]byte, 1)); count != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("Source canceled read = %d, %v", count, err)
	}
	if count, err := keyphrasetest.NewCounterSource().ReadContext(ctx, make([]byte, 1)); count != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("CounterSource canceled read = %d, %v", count, err)
	}
	if count, err := keyphrasetest.NewSource(nil).ReadContext(context.Background(), make([]byte, 1)); count != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("empty Source read = %d, %v", count, err)
	}
}
