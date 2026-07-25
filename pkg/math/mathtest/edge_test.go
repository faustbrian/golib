package mathtest

import (
	"errors"
	"fmt"
	"testing"
)

type recorder struct{ messages []string }

func (*recorder) Helper() {}
func (r *recorder) Errorf(format string, arguments ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, arguments...))
}

type value int

func (v value) Equal(other value) bool { return v == other }

func TestLawHelpersReportEveryFailurePath(t *testing.T) {
	failure := errors.New("failure")
	cases := []func(*recorder){
		func(r *recorder) {
			Commutative[value](r, []value{1}, func(_, _ value) (value, error) { return 0, failure })
		},
		func(r *recorder) {
			calls := 0
			Commutative[value](r, []value{1}, func(_, _ value) (value, error) {
				calls++
				if calls == 2 {
					return 0, failure
				}
				return value(calls), nil
			})
		},
		func(r *recorder) {
			calls := 0
			Commutative[value](r, []value{1}, func(_, _ value) (value, error) { calls++; return value(calls), nil })
		},
		func(r *recorder) { Associative[value](r, []value{1}, stagedOperation(1, failure)) },
		func(r *recorder) { Associative[value](r, []value{1}, stagedOperation(2, failure)) },
		func(r *recorder) { Associative[value](r, []value{1}, stagedOperation(3, failure)) },
		func(r *recorder) { Associative[value](r, []value{1}, stagedOperation(4, failure)) },
		func(r *recorder) {
			Associative[value](r, []value{1}, func(left, right value) (value, error) { return left - right, nil })
		},
		func(r *recorder) { Identity[value](r, []value{1}, 0, stagedOperation(1, failure)) },
		func(r *recorder) { Identity[value](r, []value{1}, 0, stagedOperation(2, failure)) },
		func(r *recorder) {
			Identity[value](r, []value{1}, 0, func(_, _ value) (value, error) { return 9, nil })
		},
		func(r *recorder) {
			RoundTrip[value](r, []value{1}, func(value) ([]byte, error) { return nil, failure }, func([]byte) (value, error) { return 0, nil })
		},
		func(r *recorder) {
			RoundTrip[value](r, []value{1}, func(value) ([]byte, error) { return nil, nil }, func([]byte) (value, error) { return 0, failure })
		},
		func(r *recorder) {
			RoundTrip[value](r, []value{1}, func(value) ([]byte, error) { return nil, nil }, func([]byte) (value, error) { return 2, nil })
		},
	}
	for index, run := range cases {
		r := &recorder{}
		run(r)
		if len(r.messages) == 0 {
			t.Fatalf("case %d did not report a failure", index)
		}
	}
}

func stagedOperation(failAt int, failure error) Operation[value] {
	calls := 0
	return func(left, right value) (value, error) {
		calls++
		if calls == failAt {
			return 0, failure
		}
		return left + right, nil
	}
}
