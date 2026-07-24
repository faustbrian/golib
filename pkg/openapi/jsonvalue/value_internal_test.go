package jsonvalue

import (
	"errors"
	"testing"
)

func TestMarshalContainerBudgetAccounting(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		count  int
		object bool
		bytes  int
	}{
		{name: "empty array", bytes: 2},
		{name: "one array value", count: 1, bytes: 2},
		{name: "two array values", count: 2, bytes: 3},
		{name: "one object member", count: 1, object: true, bytes: 3},
		{name: "two object members", count: 2, object: true, bytes: 5},
	} {
		budget := marshalBudget{bytes: test.bytes}
		if err := budget.takeContainer(test.count, test.object); err != nil || budget.bytes != 0 {
			t.Errorf("%s exact budget = %d, %v", test.name, budget.bytes, err)
		}
		budget.bytes = test.bytes - 1
		if err := budget.takeContainer(test.count, test.object); !errors.Is(err, ErrMarshalLimit) {
			t.Errorf("%s short budget error = %v", test.name, err)
		}
	}
	budget := marshalBudget{bytes: 1}
	if err := budget.takeBytes(-1); !errors.Is(err, ErrMarshalLimit) {
		t.Fatalf("negative byte count error = %v", err)
	}
	budget.bytes = 0
	if err := budget.takeBytes(0); err != nil {
		t.Fatalf("zero byte count error = %v", err)
	}
}
