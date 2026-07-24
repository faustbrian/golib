package reference_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
)

func TestPointerParsesEscapesAndEvaluatesRawTargets(t *testing.T) {
	t.Parallel()

	value, err := jsonvalue.Parse([]byte(`{
		"a/b":{"m~n":123456789012345678901234567890},
		"array":["zero",{"":true}],
		"€":"currency"
	}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		pointer string
		tokens  []string
		want    string
	}{
		{pointer: "/a~1b/m~0n", tokens: []string{"a/b", "m~n"}, want: "123456789012345678901234567890"},
		{pointer: "/array/0", tokens: []string{"array", "0"}, want: `"zero"`},
		{pointer: "/array/1/", tokens: []string{"array", "1", ""}, want: "true"},
	}
	for _, test := range tests {
		pointer, err := reference.ParsePointer(test.pointer, reference.DefaultPointerPolicy())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(pointer.Tokens(), test.tokens) {
			t.Fatalf("Tokens(%q) = %#v", test.pointer, pointer.Tokens())
		}
		target, err := pointer.Evaluate(value, jsonvalue.DefaultPolicy())
		if err != nil {
			t.Fatal(err)
		}
		if string(target.Bytes()) != test.want {
			t.Fatalf("Evaluate(%q) = %s, want %s", test.pointer, target.Bytes(), test.want)
		}
	}

	fragment, err := reference.ParseFragment("#/%E2%82%AC", reference.DefaultPointerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	target, err := fragment.Evaluate(value, jsonvalue.DefaultPolicy())
	if err != nil || string(target.Bytes()) != `"currency"` {
		t.Fatalf("fragment target = %s, error = %v", target.Bytes(), err)
	}
}

func TestPointerRejectsMalformedOrUnresolvablePaths(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"not-a-pointer", "/bad~2escape", "/trailing~"} {
		_, err := reference.ParsePointer(input, reference.DefaultPointerPolicy())
		if !errors.Is(err, reference.ErrInvalidPointer) {
			t.Errorf("ParsePointer(%q) error = %v", input, err)
		}
	}
	if _, err := reference.ParseFragment("#/%ZZ", reference.DefaultPointerPolicy()); !errors.Is(err, reference.ErrInvalidPointer) {
		t.Fatalf("ParseFragment error = %v", err)
	}

	value, err := jsonvalue.Parse([]byte(`{"array":[1],"scalar":true}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"/missing", "/array/01", "/array/-", "/array/2", "/scalar/child"} {
		pointer, err := reference.ParsePointer(input, reference.DefaultPointerPolicy())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pointer.Evaluate(value, jsonvalue.DefaultPolicy()); !errors.Is(err, reference.ErrPointerTarget) {
			t.Errorf("Evaluate(%q) error = %v", input, err)
		}
	}
}

func TestPointerPolicyAndTokensAreOwnershipSafe(t *testing.T) {
	t.Parallel()

	policy := reference.PointerPolicy{MaxLength: 4, MaxTokens: 1, MaxIndexDigits: 2}
	if _, err := reference.ParsePointer("/long", policy); !errors.Is(err, reference.ErrPointerLimit) {
		t.Fatalf("length error = %v", err)
	}
	if _, err := reference.ParsePointer("/a/b", policy); !errors.Is(err, reference.ErrPointerLimit) {
		t.Fatalf("token error = %v", err)
	}
	pointer, err := reference.ParsePointer("/a", policy)
	if err != nil {
		t.Fatal(err)
	}
	tokens := pointer.Tokens()
	tokens[0] = "changed"
	if pointer.Tokens()[0] != "a" {
		t.Fatal("Tokens exposed mutable pointer storage")
	}
}

func FuzzPointerNeverPanics(f *testing.F) {
	for _, seed := range []string{"", "/a~1b", "/m~0n", "/0", "/~2"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		policy := reference.PointerPolicy{MaxLength: 256, MaxTokens: 32, MaxIndexDigits: 8}
		pointer, err := reference.ParsePointer(input, policy)
		if err != nil {
			return
		}
		value, err := jsonvalue.Parse([]byte(`{"a":[1,2,3]}`), jsonvalue.DefaultPolicy())
		if err != nil {
			t.Fatal(err)
		}
		_, _ = pointer.Evaluate(value, jsonvalue.DefaultPolicy())
	})
}
