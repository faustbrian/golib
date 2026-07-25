package expression_test

import (
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/expression"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestEvaluateLinkParamsPreservesExpressionTypesAndConstants(t *testing.T) {
	t.Parallel()

	params := value(t, `{
		"username":"${result.owner.username}",
		"id":"${result.id}",
		"label":"user-${result.owner.username}",
		"constant":true
	}`)
	link, err := openrpc.NewLink(openrpc.LinkInput{Params: &params})
	if err != nil {
		t.Fatal(err)
	}
	context, err := expression.NewContext(map[string]jsonvalue.Value{
		"result": value(t, `{"id":42,"owner":{"username":"alice"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, present, err := expression.EvaluateLinkParams(link, context, expression.DefaultPolicy())
	if err != nil || !present {
		t.Fatalf("present = %t, error = %v", present, err)
	}
	if got := string(result.Bytes()); got != `{"constant":true,"id":42,"label":"user-alice","username":"alice"}` {
		t.Fatalf("params = %s", got)
	}
}

func TestEvaluateLinkParamsHandlesAbsenceAndMissingValues(t *testing.T) {
	t.Parallel()

	link, err := openrpc.NewLink(openrpc.LinkInput{})
	if err != nil {
		t.Fatal(err)
	}
	context, err := expression.NewContext(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := expression.EvaluateLinkParams(link, context, expression.DefaultPolicy()); err != nil || present {
		t.Fatalf("present = %t, error = %v", present, err)
	}
	params := value(t, `{"id":"${missing}"}`)
	link, err = openrpc.NewLink(openrpc.LinkInput{Params: &params})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := expression.EvaluateLinkParams(link, context, expression.DefaultPolicy()); !errors.Is(err, expression.ErrMissingValue) {
		t.Fatalf("error = %v", err)
	}
}
