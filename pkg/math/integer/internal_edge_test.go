package integer

import (
	"bytes"
	"context"
	"math/big"
	"testing"
	"time"

	gomath "github.com/faustbrian/golib/pkg/math"
)

type cancelAfterValidation struct{ calls int }

func (c *cancelAfterValidation) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *cancelAfterValidation) Done() <-chan struct{}       { return nil }
func (c *cancelAfterValidation) Value(any) any               { return nil }
func (c *cancelAfterValidation) Err() error {
	c.calls++
	if c.calls > 1 {
		return context.Canceled
	}
	return nil
}

func TestInternalBoundaryBranches(t *testing.T) {
	limits := gomath.DefaultLimits()
	bad := limits
	bad.MaxInputDigits = 0
	if _, err := Parse("1", ParseOptions{Base: 10, Limits: bad}); err == nil {
		t.Fatal("expected parser limit validation")
	}
	tinyParse := limits
	tinyParse.MaxIntermediateBits = 1
	if _, err := Parse("9", ParseOptions{Base: 10, Limits: tinyParse}); err == nil {
		t.Fatal("expected parsed magnitude limit")
	}
	if digitValue('!') != -1 {
		t.Fatal("invalid digit accepted")
	}
	if Max(New(2), New(1)).String() != "2" {
		t.Fatal("max left branch")
	}
	var nilContext context.Context
	if _, err := New(2).Mul(nilContext, New(2), limits); err == nil {
		t.Fatal("expected multiply context error")
	}
	if _, _, err := New(2).QuoRem(nilContext, New(1), limits); err == nil {
		t.Fatal("expected quotient context error")
	}
	if _, err := New(2).Mod(nilContext, New(1), limits); err == nil {
		t.Fatal("expected modulus context error")
	}
	if _, err := New(1).Mod(context.Background(), Integer{}, limits); err == nil {
		t.Fatal("expected zero modulus")
	}
	cancelledRoot, cancelRoot := context.WithCancel(context.Background())
	cancelRoot()
	if _, err := New(2).Root(cancelledRoot, 2, limits); err == nil {
		t.Fatal("expected root context error")
	}
	if _, err := GCD(nilContext, New(2), New(4), limits); err == nil {
		t.Fatal("expected GCD context error")
	}
	if _, err := LCM(nilContext, New(2), New(4), limits); err == nil {
		t.Fatal("expected LCM context error")
	}
	tiny := limits
	tiny.MaxIntermediateBits = 3
	if _, err := LCM(context.Background(), New(7), New(5), tiny); err == nil {
		t.Fatal("expected LCM size limit")
	}
	if _, err := binary(nilContext, big.NewInt(1), big.NewInt(1), limits, (*big.Int).Add); err == nil {
		t.Fatal("expected binary context error")
	}
	if err := validateContext(context.Background(), bad); err == nil {
		t.Fatal("expected limit validation")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := nthRoot(cancelled, big.NewInt(1000), 3, limits); err == nil {
		t.Fatal("expected root cancellation")
	}
	if _, err := New(1000).Root(&cancelAfterValidation{}, 3, limits); err == nil {
		t.Fatal("expected root loop cancellation")
	}
	if root, err := nthRoot(context.Background(), big.NewInt(1<<20), 3, tiny); err != nil || root.Sign() < 0 {
		t.Fatal("bounded root failed")
	}
	if _, err := Random(context.Background(), bytes.NewReader([]byte{255, 1}), New(0), New(3), limits); err != nil {
		t.Fatalf("rejection sampling: %v", err)
	}
	if _, err := Random(&cancelAfterValidation{}, bytes.NewReader([]byte{1}), New(0), New(3), limits); err == nil {
		t.Fatal("expected random loop cancellation")
	}
}

func TestDigitValueBoundaries(t *testing.T) {
	t.Parallel()

	tests := map[byte]int{
		'/': -1, '0': 0, '9': 9, ':': -1,
		'@': -1, 'A': 10, 'Z': 35, '[': -1,
		'`': -1, 'a': 10, 'z': 35, '{': -1,
	}
	for input, want := range tests {
		if got := digitValue(input); got != want {
			t.Errorf("digitValue(%q) = %d, want %d", input, got, want)
		}
	}
}
