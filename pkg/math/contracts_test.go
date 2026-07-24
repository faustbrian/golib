package gomath_test

import (
	"errors"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
)

func TestSharedContracts(t *testing.T) {
	t.Parallel()

	conditions := gomath.ConditionRounded | gomath.ConditionInexact
	if !conditions.Has(gomath.ConditionRounded) || conditions.String() != "rounded,inexact" {
		t.Fatalf("unexpected conditions: %s", conditions)
	}

	limits := gomath.DefaultLimits()
	if err := limits.Validate(); err != nil {
		t.Fatalf("DefaultLimits().Validate() error = %v", err)
	}
	limits.MaxInputDigits = 0
	if err := limits.Validate(); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("Limits.Validate() error = %v, want ErrInvalidArgument", err)
	}

	if gomath.RoundHalfEven.String() != "half_even" {
		t.Fatalf("RoundHalfEven.String() = %q", gomath.RoundHalfEven)
	}
}

func TestErrorMessagesUseCanonicalPackageName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		want string
	}{
		"invalid argument": {
			err:  gomath.ErrInvalidArgument,
			want: "math: invalid argument",
		},
		"invalid syntax": {
			err:  gomath.ErrInvalidSyntax,
			want: "math: invalid syntax",
		},
		"limit exceeded": {
			err:  gomath.ErrLimitExceeded,
			want: "math: resource limit exceeded",
		},
		"division by zero": {
			err:  gomath.ErrDivisionByZero,
			want: "math: division by zero",
		},
		"domain": {
			err:  gomath.ErrDomain,
			want: "math: domain error",
		},
		"conversion": {
			err:  gomath.ErrConversion,
			want: "math: inexact conversion",
		},
		"overflow": {
			err:  gomath.ErrOverflow,
			want: "math: overflow",
		},
		"underflow": {
			err:  gomath.ErrUnderflow,
			want: "math: underflow",
		},
		"trapped condition": {
			err:  gomath.ErrTrappedCondition,
			want: "math: trapped condition",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := test.err.Error(); got != test.want {
				t.Fatalf("error = %q, want %q", got, test.want)
			}
		})
	}
}
