package parse_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/parse"
)

func FuzzJSONParserDeterminism(f *testing.F) {
	for _, seed := range []string{
		`null`,
		`{"openapi":"3.2.0","paths":{}}`,
		`[true,-0.0e+2,"value"]`,
		`{"duplicate":1,"duplicate":2}`,
		`[`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		limits := parse.DefaultLimits()
		limits.MaxBytes = 1 << 20
		first, firstErr := parse.JSON(context.Background(), strings.NewReader(raw), limits)
		second, secondErr := parse.JSON(context.Background(), strings.NewReader(raw), limits)
		if !sameParseError(firstErr, secondErr) {
			t.Fatalf("nondeterministic errors: %v and %v", firstErr, secondErr)
		}
		if firstErr != nil {
			return
		}
		firstJSON, err := first.MarshalJSON()
		if err != nil || !json.Valid(firstJSON) {
			t.Fatalf("invalid semantic JSON %q: %v", firstJSON, err)
		}
		secondJSON, err := second.MarshalJSON()
		if err != nil || !reflect.DeepEqual(firstJSON, secondJSON) {
			t.Fatalf("nondeterministic values %q and %q: %v", firstJSON, secondJSON, err)
		}
	})
}

func FuzzYAMLParserProducesJSONSemantics(f *testing.F) {
	for _, seed := range []string{
		"null\n",
		"openapi: 3.2.0\npaths: {}\n",
		"values: [true, -0.0e+2, text]\n",
		"key: &anchor value\ncopy: *anchor\n",
		"---\none: 1\n---\ntwo: 2\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		limits := parse.DefaultLimits()
		limits.MaxBytes = 1 << 20
		value, err := parse.YAML(context.Background(), strings.NewReader(raw), limits)
		if err != nil {
			return
		}
		encoded, err := value.MarshalJSON()
		if err != nil || !json.Valid(encoded) {
			t.Fatalf("invalid semantic JSON %q: %v", encoded, err)
		}
	})
}

func sameParseError(left error, right error) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	var leftParse *parse.Error
	var rightParse *parse.Error
	if errors.As(left, &leftParse) && errors.As(right, &rightParse) {
		return leftParse.Code == rightParse.Code && leftParse.Offset == rightParse.Offset
	}
	return left.Error() == right.Error()
}
