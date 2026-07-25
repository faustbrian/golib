package keyphrase_test

import (
	"context"
	"errors"
	"math/big"
	"testing"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
)

type byteSource struct {
	bytes []byte
	err   error
	calls int
}

func (s *byteSource) ReadContext(_ context.Context, destination []byte) (int, error) {
	s.calls++
	if s.err != nil {
		return 0, s.err
	}

	if len(s.bytes) == 0 {
		return 0, nil
	}

	destination[0] = s.bytes[0]
	s.bytes = s.bytes[1:]

	return 1, nil
}

func TestSelectorRejectsOutOfRangeSamples(t *testing.T) {
	t.Parallel()

	source := &byteSource{bytes: []byte{0xff, 0x07}}
	selector, err := keyphrase.NewSelector(source, keyphrase.WithMaxAttempts(2))
	if err != nil {
		t.Fatalf("NewSelector() error = %v", err)
	}

	index, err := selector.Index(context.Background(), 10)
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}

	if index != 7 {
		t.Fatalf("Index() = %d, want 7", index)
	}
	if source.calls != 2 {
		t.Fatalf("source calls = %d, want 2", source.calls)
	}
}

func TestSelectorReportsTypedFailuresWithoutSamples(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source *byteSource
		upper  uint64
		code   keyphrase.ErrorCode
	}{
		{name: "invalid upper bound", source: &byteSource{}, code: keyphrase.CodeInvalidBound},
		{name: "source failure", source: &byteSource{err: errors.New("device failed")}, upper: 2, code: keyphrase.CodeSource},
		{name: "short read", source: &byteSource{}, upper: 2, code: keyphrase.CodeShortRead},
		{name: "attempt limit", source: &byteSource{bytes: []byte{0xff, 0xff}}, upper: 10, code: keyphrase.CodeAttemptsExceeded},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			selector, err := keyphrase.NewSelector(test.source, keyphrase.WithMaxAttempts(2))
			if err != nil {
				t.Fatalf("NewSelector() error = %v", err)
			}

			_, err = selector.Index(context.Background(), test.upper)
			var generationError *keyphrase.Error
			if !errors.As(err, &generationError) {
				t.Fatalf("Index() error = %v, want *keyphrase.Error", err)
			}
			if generationError.Code != test.code {
				t.Fatalf("error code = %q, want %q", generationError.Code, test.code)
			}
		})
	}
}

func TestSelectorHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	selector := keyphrase.DefaultSelector()
	_, err := selector.Index(ctx, 2)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Index() error = %v, want context.Canceled", err)
	}
}

func TestSelectorSamplesArbitraryPrecisionBounds(t *testing.T) {
	t.Parallel()

	upper := new(big.Int).Lsh(big.NewInt(1), 80)
	upper.Add(upper, big.NewInt(7))
	source := &byteSource{bytes: append([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, make([]byte, 11)...)}
	selector, err := keyphrase.NewSelector(source, keyphrase.WithMaxAttempts(2))
	if err != nil {
		t.Fatalf("NewSelector() error = %v", err)
	}

	value, err := selector.BigInt(context.Background(), upper)
	if err != nil {
		t.Fatalf("BigInt() error = %v", err)
	}
	if value.Sign() != 0 {
		t.Fatalf("BigInt() = %s, want 0", value)
	}
	if source.calls != 22 {
		t.Fatalf("source calls = %d, want 22", source.calls)
	}
}
