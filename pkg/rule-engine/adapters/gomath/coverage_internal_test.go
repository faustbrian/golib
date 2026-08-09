package ruleenginemath

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

func TestDecimalOperatorTruthAndFailureTable(t *testing.T) {
	t.Parallel()

	left := Decimal(decimal.MustParse("1.5"))
	equal := Decimal(decimal.MustParse("1.5"))
	lower := Decimal(decimal.MustParse("1.4"))
	higher := Decimal(decimal.MustParse("1.6"))
	tests := []struct {
		index int
		right ruleengine.Value
		want  bool
	}{
		{0, equal, true},
		{1, higher, true},
		{2, equal, true},
		{3, lower, true},
		{4, equal, true},
	}
	operators := Operators()
	for _, test := range tests {
		operator := operators[test.index]
		if operator.Name() == "" || len(operator.Signatures()) != 1 {
			t.Fatalf("operator metadata = %q, %#v", operator.Name(), operator.Signatures())
		}
		got, err := operator.Evaluate(context.Background(), left, test.right)
		if err != nil || got != test.want {
			t.Fatalf("%s Evaluate() = %v, %v", operator.Name(), got, err)
		}
	}
	if got, err := operators[0].Evaluate(context.Background(), left, higher); err != nil || got {
		t.Fatalf("unequal Evaluate() = %v, %v", got, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := operators[0].Evaluate(canceled, left, equal); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Evaluate() error = %v", err)
	}
	for _, pair := range [][2]ruleengine.Value{
		{{}, equal},
		{ruleengine.String(EncodingV1Prefix + "invalid"), equal},
		{left, ruleengine.Int(1)},
		{left, ruleengine.String(EncodingV1Prefix + "invalid")},
	} {
		if _, err := operators[0].Evaluate(context.Background(), pair[0], pair[1]); err == nil {
			t.Fatalf("Evaluate(%#v, %#v) error = nil", pair[0], pair[1])
		}
	}
}

func TestDecimalOperatorObservesCancellationBetweenBoundedParsingSteps(t *testing.T) {
	t.Parallel()

	value := Decimal(decimal.MustParse("1"))
	for _, cancelAt := range []int{2, 3} {
		ctx := &cancelOnErrCall{cancelAt: cancelAt}
		if _, err := Operators()[0].Evaluate(ctx, value, value); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel at Err call %d: error = %v", cancelAt, err)
		}
	}
}

type cancelOnErrCall struct {
	calls    int
	cancelAt int
}

func (*cancelOnErrCall) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelOnErrCall) Done() <-chan struct{}       { return nil }
func (*cancelOnErrCall) Value(any) any               { return nil }
func (ctx *cancelOnErrCall) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}

	return nil
}
