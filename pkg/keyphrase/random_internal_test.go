package keyphrase

import (
	"context"
	"errors"
	"math"
	"math/big"
	"testing"
)

type internalSource struct {
	bytes    []byte
	err      error
	count    int
	oversize bool
}

func (s *internalSource) ReadContext(_ context.Context, destination []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.oversize {
		return len(destination) + 1, nil
	}
	count := s.count
	if count == 0 || count > len(destination) {
		count = len(destination)
	}
	if count > len(s.bytes) {
		count = len(s.bytes)
	}
	copy(destination, s.bytes[:count])
	s.bytes = s.bytes[count:]
	return count, nil
}

type cancelSource struct{}

func (cancelSource) ReadContext(ctx context.Context, _ []byte) (int, error) {
	<-ctx.Done()
	return 0, ctx.Err()
}

type cancelingErrorSource struct {
	cancel context.CancelFunc
}

func (s cancelingErrorSource) ReadContext(_ context.Context, _ []byte) (int, error) {
	s.cancel()
	return 0, errors.New("canceled source")
}

func TestSelectorConstructionFailures(t *testing.T) {
	t.Parallel()

	if _, err := NewSelector(nil); errorCode(err) != CodeInvalidSource {
		t.Fatalf("NewSelector(nil) code = %q", errorCode(err))
	}
	if _, err := NewSelector(&internalSource{}, nil); errorCode(err) != CodeInvalidOption {
		t.Fatalf("NewSelector(nil option) code = %q", errorCode(err))
	}
	if _, err := NewSelector(&internalSource{}, WithMaxAttempts(0)); errorCode(err) != CodeInvalidOption {
		t.Fatalf("WithMaxAttempts(0) code = %q", errorCode(err))
	}
	selector, err := NewSelector(&internalSource{}, WithMaxAttempts(1))
	if err != nil || selector.maxAttempts != 1 {
		t.Fatalf("valid construction = %#v, %v", selector, err)
	}

	generationError := &Error{Code: CodeSource, Cause: errors.New("cause")}
	if generationError.Error() != "keyphrase: generation failed (source_failure)" || generationError.Unwrap() == nil {
		t.Fatal("Error contract mismatch")
	}
}

func TestUninitializedSelectorsReturnTypedErrors(t *testing.T) {
	t.Parallel()

	selectors := []*Selector{nil, {}}
	for _, selector := range selectors {
		if _, err := selector.Index(context.Background(), 2); errorCode(err) != CodeInvalidSource {
			t.Fatalf("Index(uninitialized) code = %q", errorCode(err))
		}
		if _, err := selector.BigInt(context.Background(), big.NewInt(2)); errorCode(err) != CodeInvalidSource {
			t.Fatalf("BigInt(uninitialized) code = %q", errorCode(err))
		}
		destination := []byte{9}
		if err := selector.Fill(context.Background(), destination); errorCode(err) != CodeInvalidSource || destination[0] != 0 {
			t.Fatalf("Fill(uninitialized) = %v, %v", destination, err)
		}
	}
}

func TestSelectorIndexBoundaries(t *testing.T) {
	t.Parallel()

	selector, _ := NewSelector(&internalSource{bytes: make([]byte, 8)})
	if value, err := selector.Index(context.Background(), 1); err != nil || value != 0 {
		t.Fatalf("Index(1) = %d, %v", value, err)
	}
	if value, err := selector.Index(context.Background(), math.MaxUint64); err != nil || value != 0 {
		t.Fatalf("Index(MaxUint64) = %d, %v", value, err)
	}
	selector, _ = NewSelector(&internalSource{bytes: []byte{2}})
	if value, err := selector.Index(context.Background(), 2); err != nil || value != 0 {
		t.Fatalf("Index(2) masked value = %d, %v", value, err)
	}
	selector, _ = NewSelector(
		&internalSource{bytes: []byte{10, 9}},
		WithMaxAttempts(2),
	)
	if value, err := selector.Index(context.Background(), 10); err != nil || value != 9 {
		t.Fatalf("Index(10) boundary rejection = %d, %v", value, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := selector.Index(ctx, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("Index(canceled) error = %v", err)
	}
}

func TestSelectorBigIntFailureBoundaries(t *testing.T) {
	t.Parallel()

	selector, _ := NewSelector(&internalSource{})
	for _, upper := range []*big.Int{nil, big.NewInt(0), big.NewInt(-1)} {
		if _, err := selector.BigInt(context.Background(), upper); errorCode(err) != CodeInvalidBound {
			t.Fatalf("BigInt(%v) code = %q", upper, errorCode(err))
		}
	}
	if value, err := selector.BigInt(context.Background(), big.NewInt(1)); err != nil || value.Sign() != 0 {
		t.Fatalf("BigInt(1) = %v, %v", value, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := selector.BigInt(ctx, big.NewInt(2)); !errors.Is(err, context.Canceled) {
		t.Fatalf("BigInt(canceled) error = %v", err)
	}
	oversized := new(big.Int).Lsh(big.NewInt(1), maxSampleBits+1)
	if _, err := selector.BigInt(context.Background(), oversized); errorCode(err) != CodeOversized {
		t.Fatalf("BigInt(oversized) code = %q", errorCode(err))
	}
	boundary := new(big.Int).Lsh(big.NewInt(1), maxSampleBits-1)
	boundary.Add(boundary, big.NewInt(1))
	selector, _ = NewSelector(&internalSource{err: errors.New("source")})
	if _, err := selector.BigInt(context.Background(), boundary); errorCode(err) != CodeSource {
		t.Fatalf("BigInt(maximum bits) code = %q", errorCode(err))
	}

	rejecting, _ := NewSelector(&internalSource{bytes: []byte{0xff}}, WithMaxAttempts(1))
	if _, err := rejecting.BigInt(context.Background(), big.NewInt(3)); errorCode(err) != CodeAttemptsExceeded {
		t.Fatalf("BigInt(rejecting) code = %q", errorCode(err))
	}
}

func TestFillClearsEveryFailureAndHandlesPartialReads(t *testing.T) {
	t.Parallel()

	destination := []byte{9, 9, 9}
	selector, _ := NewSelector(&internalSource{bytes: []byte{1, 2, 3}, count: 1})
	if err := selector.Fill(context.Background(), destination); err != nil || destination[0] != 1 || destination[2] != 3 {
		t.Fatalf("Fill(partial reads) = %v, %v", destination, err)
	}

	destination = []byte{9}
	selector, _ = NewSelector(&internalSource{oversize: true})
	if err := selector.Fill(context.Background(), destination); errorCode(err) != CodeShortRead || destination[0] != 0 {
		t.Fatalf("Fill(oversize read) = %v, %v", destination, err)
	}

	destination = make([]byte, maxSampleBits/8+1)
	if err := selector.Fill(context.Background(), destination); errorCode(err) != CodeOversized {
		t.Fatalf("Fill(oversized) code = %q", errorCode(err))
	}
	destination = make([]byte, maxSampleBits/8)
	selector, _ = NewSelector(&internalSource{err: errors.New("source")})
	if err := selector.Fill(context.Background(), destination); errorCode(err) != CodeSource {
		t.Fatalf("Fill(maximum size) code = %q", errorCode(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	selector, _ = NewSelector(cancelSource{})
	destination = []byte{9}
	done := make(chan error, 1)
	go func() { done <- selector.Fill(ctx, destination) }()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || destination[0] != 0 {
		t.Fatalf("Fill(blocked cancellation) = %v, %v", destination, err)
	}
}

func TestSourceErrorAndCryptoSourceBranches(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (cryptoSource{}).ReadContext(ctx, make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cryptoSource canceled error = %v", err)
	}
	if count, err := (cryptoSource{}).ReadContext(context.Background(), make([]byte, 1)); count != 1 || err != nil {
		t.Fatalf("cryptoSource read = %d, %v", count, err)
	}

	selector, _ := NewSelector(&internalSource{err: errors.New("source")})
	if _, err := selector.BigInt(context.Background(), big.NewInt(2)); errorCode(err) != CodeSource {
		t.Fatalf("BigInt(source error) code = %q", errorCode(err))
	}
	ctx, cancel = context.WithCancel(context.Background())
	selector, _ = NewSelector(cancelingErrorSource{cancel: cancel})
	if _, err := selector.Index(ctx, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("Index(canceling source) error = %v", err)
	}
}

func errorCode(err error) ErrorCode {
	var generationError *Error
	if errors.As(err, &generationError) {
		return generationError.Code
	}
	return ""
}
