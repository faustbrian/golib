package expression

import (
	"errors"
	"strings"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestParserAndEvaluatorCoverEveryBoundary(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	context, err := NewContext(map[string]jsonvalue.Value{
		"object": expressionValue(t, `{"name":"alice","items":[true]}`),
		"large":  expressionValue(t, `"large"`),
		"null":   expressionValue(t, `null`),
	})
	if err != nil {
		t.Fatal(err)
	}

	invalid := []struct {
		source string
		policy Policy
		want   error
	}{
		{source: "", policy: policy, want: ErrInvalidExpression},
		{source: string([]byte{0xff}), policy: policy, want: ErrInvalidExpression},
		{source: "${a}${a}", policy: withExpressionLimit(policy, 1), want: ErrExpressionLimit},
		{source: "${a.b}", policy: withSegmentLimit(policy, 1), want: ErrExpressionLimit},
		{source: "${a[10]}", policy: withIndexLimit(policy, 1), want: ErrInvalidExpression},
		{source: "${a[999999999999999999999999]}", policy: withIndexLimit(policy, 30), want: ErrExpressionLimit},
		{source: "${a[x]}", policy: policy, want: ErrInvalidExpression},
		{source: "${a[1}", policy: policy, want: ErrInvalidExpression},
		{source: "${a$}", policy: policy, want: ErrInvalidExpression},
		{source: "${1}", policy: policy, want: ErrInvalidExpression},
	}
	for _, test := range invalid {
		if _, err := Parse(test.source, test.policy); !errors.Is(err, test.want) {
			t.Errorf("Parse(%q) error = %v, want %v", test.source, err, test.want)
		}
	}

	for _, test := range []struct {
		source string
		want   error
	}{
		{source: "${object.items[1]}", want: ErrMissingValue},
		{source: "${object.items.name}", want: ErrMissingValue},
		{source: "${object.name[0]}", want: ErrMissingValue},
		{source: "${missing}", want: ErrMissingValue},
	} {
		template, parseErr := Parse(test.source, policy)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if _, err := template.Evaluate(context); !errors.Is(err, test.want) {
			t.Errorf("Evaluate(%q) error = %v", test.source, err)
		}
	}

	zero := Template{}
	if _, err := zero.Evaluate(context); !errors.Is(err, ErrInvalidExpression) {
		t.Fatalf("zero template error = %v", err)
	}
	template, err := Parse("${large}", withOutputLimit(policy, 4))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := template.Evaluate(context); !errors.Is(err, ErrExpressionLimit) {
		t.Fatalf("whole output error = %v", err)
	}
	for _, source := range []string{"abcde", "x${large}", "${null}x"} {
		template, err = Parse(source, withOutputLimit(policy, 4))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := template.Evaluate(context); !errors.Is(err, ErrExpressionLimit) {
			t.Errorf("Evaluate(%q) error = %v", source, err)
		}
	}
	template, err = Parse("a", withOutputLimit(policy, 2))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := template.Evaluate(context); !errors.Is(err, ErrExpressionLimit) {
		t.Fatalf("encoded output error = %v", err)
	}
}

func TestInternalExpressionInvariants(t *testing.T) {
	t.Parallel()

	if _, err := NewContext(map[string]jsonvalue.Value{"zero": {}}); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("zero binding error = %v", err)
	}
	remaining := 10
	if err := consumeNodes([]byte(`{"a":]`), &remaining); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("malformed nodes error = %v", err)
	}
	remaining = 0
	if err := consumeNodes([]byte(`1`), &remaining); !errors.Is(err, ErrExpressionLimit) {
		t.Fatalf("zero node budget error = %v", err)
	}
	if _, err := interpolationText(jsonvalue.Value{}); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("zero interpolation error = %v", err)
	}
	var output strings.Builder
	if err := appendBounded(&output, "four", 4); err != nil {
		t.Fatal(err)
	}
	if err := appendBounded(&output, "x", 4); !errors.Is(err, ErrExpressionLimit) {
		t.Fatalf("bounded append error = %v", err)
	}

	policy := DefaultPolicy()
	invalidPolicies := []Policy{
		withLengthLimit(policy, 0),
		withExpressionLimit(policy, 0),
		withSegmentLimit(policy, 0),
		withIndexLimit(policy, 0),
		withNodeLimit(policy, 0),
		withOutputLimit(policy, 0),
	}
	for _, invalid := range invalidPolicies {
		if validPolicy(invalid) {
			t.Errorf("validPolicy(%+v) = true", invalid)
		}
	}
	for _, character := range []byte{'A', 'Z', 'a', 'z', '_'} {
		if !identifierCharacter(character) {
			t.Errorf("identifierCharacter(%q) = false", character)
		}
	}
	for _, character := range []byte{'@', '0', '[', 'A' - 1, 'Z' + 1, 'a' - 1, 'z' + 1} {
		if identifierCharacter(character) {
			t.Errorf("identifierCharacter(%q) = true", character)
		}
	}
	for _, character := range []byte{'A', 'Z', 'a', 'z', '0', '9', '-', '@'} {
		if !literalCharacter(character) {
			t.Errorf("literalCharacter(%q) = false", character)
		}
	}
	for _, character := range []byte{' ', '^', '\\'} {
		if literalCharacter(character) {
			t.Errorf("literalCharacter(%q) = true", character)
		}
	}
}

func TestExactExpressionLimitsRemainValid(t *testing.T) {
	t.Parallel()

	context, err := NewContext(map[string]jsonvalue.Value{
		"a":     expressionValue(t, `{"b":[0]}`),
		"large": expressionValue(t, `"large"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		source string
		policy Policy
	}{
		{source: "a", policy: withLengthLimit(DefaultPolicy(), 1)},
		{source: "${a}", policy: withExpressionLimit(DefaultPolicy(), 1)},
		{source: "${a.b}", policy: withSegmentLimit(DefaultPolicy(), 2)},
		{source: "${a.b[0]}", policy: withIndexLimit(DefaultPolicy(), 1)},
		{source: "a", policy: withOutputLimit(DefaultPolicy(), 3)},
		{source: "${large}", policy: withOutputLimit(DefaultPolicy(), 7)},
	}
	for _, test := range tests {
		template, err := Parse(test.source, test.policy)
		if err != nil {
			t.Errorf("Parse(%q) error = %v", test.source, err)
			continue
		}
		if _, err := template.Evaluate(context); err != nil {
			t.Errorf("Evaluate(%q) error = %v", test.source, err)
		}
	}
}

func TestLinkEvaluationCoversNestedValuesAndLimits(t *testing.T) {
	t.Parallel()

	context, err := NewContext(map[string]jsonvalue.Value{"value": expressionValue(t, `7`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		params string
		policy Policy
		want   error
	}{
		{params: `[]`, policy: DefaultPolicy(), want: ErrUnsupportedValue},
		{params: `{"nested":["${value}",{"plain":true}]}`, policy: DefaultPolicy()},
		{params: `{"bad":"${}"}`, policy: DefaultPolicy(), want: ErrInvalidExpression},
		{params: `{"nested":["${missing}"]}`, policy: DefaultPolicy(), want: ErrMissingValue},
		{params: `{"one":1}`, policy: withNodeLimit(DefaultPolicy(), 1), want: ErrExpressionLimit},
		{params: `{"large":"abcd"}`, policy: withOutputLimit(DefaultPolicy(), 4), want: ErrExpressionLimit},
	} {
		params := expressionValue(t, test.params)
		link, linkErr := openrpc.NewLink(openrpc.LinkInput{Params: &params})
		if linkErr != nil {
			t.Fatal(linkErr)
		}
		_, present, err := EvaluateLinkParams(link, context, test.policy)
		if !present || !errors.Is(err, test.want) {
			t.Errorf("params %s: present = %t, error = %v, want %v", test.params, present, err, test.want)
		}
	}
	link, err := openrpc.NewLink(openrpc.LinkInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := EvaluateLinkParams(link, context, Policy{}); !errors.Is(err, ErrExpressionPolicy) {
		t.Fatalf("invalid policy error = %v", err)
	}
	empty := expressionValue(t, `{}`)
	link, err = openrpc.NewLink(openrpc.LinkInput{Params: &empty})
	if err != nil {
		t.Fatal(err)
	}
	policy := withNodeLimit(withOutputLimit(DefaultPolicy(), 2), 1)
	if _, _, err := EvaluateLinkParams(link, context, policy); err != nil {
		t.Fatalf("exact link limits error = %v", err)
	}
}

func TestServerEvaluationRejectsInvalidTemplate(t *testing.T) {
	t.Parallel()

	server, err := openrpc.NewServer(openrpc.ServerInput{URL: "invalid value"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateServer(server, nil, DefaultPolicy()); !errors.Is(err, ErrInvalidExpression) {
		t.Fatalf("invalid server template error = %v", err)
	}
}

func expressionValue(t *testing.T, source string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Parse([]byte(source), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func withLengthLimit(policy Policy, limit int) Policy { policy.MaxLength = limit; return policy }
func withExpressionLimit(policy Policy, limit int) Policy {
	policy.MaxExpressions = limit
	return policy
}
func withSegmentLimit(policy Policy, limit int) Policy { policy.MaxSegments = limit; return policy }
func withIndexLimit(policy Policy, limit int) Policy   { policy.MaxIndexDigits = limit; return policy }
func withNodeLimit(policy Policy, limit int) Policy    { policy.MaxNodes = limit; return policy }
func withOutputLimit(policy Policy, limit int) Policy  { policy.MaxOutputBytes = limit; return policy }
