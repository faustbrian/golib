package mathtest_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/math/mathtest"
)

type number int

func (n number) Equal(other number) bool { return n == other }

func TestLawHelpersAcceptValidOperations(t *testing.T) {
	t.Parallel()

	values := []number{-2, 0, 3}
	add := func(left, right number) (number, error) { return left + right, nil }
	mathtest.Commutative(t, values, add)
	mathtest.Associative(t, values, add)
	mathtest.Identity(t, values, 0, add)
	mathtest.RoundTrip(t, values,
		func(value number) ([]byte, error) { return []byte{byte(value + 2)}, nil },
		func(data []byte) (number, error) { return number(data[0]) - 2, nil },
	)
}

func TestLawHelpersReportOperationErrors(t *testing.T) {
	t.Parallel()

	recorder := &recordingT{}
	mathtest.Commutative(recorder, []number{1}, func(number, number) (number, error) {
		return 0, errors.New("operation failed")
	})
	if recorder.failures == 0 {
		t.Fatal("Commutative() did not report an operation error")
	}
}

type recordingT struct{ failures int }

func (r *recordingT) Helper() {}

func (r *recordingT) Errorf(string, ...any) { r.failures++ }
