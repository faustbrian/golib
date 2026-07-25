package jsonvalue_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestParsePreservesExactJSONAndOwnsMemory(t *testing.T) {
	t.Parallel()

	input := []byte(` { "large": 123456789012345678901234567890, "value": null } `)
	want := append([]byte(nil), input...)

	value, err := jsonvalue.Parse(input, jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	input[1] = '['
	if !bytes.Equal(value.Bytes(), want) {
		t.Fatalf("Bytes() = %q, want %q", value.Bytes(), want)
	}

	returned := value.Bytes()
	returned[1] = '['
	if !bytes.Equal(value.Bytes(), want) {
		t.Fatal("Bytes exposed mutable internal storage")
	}
}

func TestParseRejectsAmbiguousOrMalformedJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []byte
		want  error
	}{
		{name: "duplicate", input: []byte(`{"a":1,"a":2}`), want: jsonvalue.ErrDuplicateName},
		{name: "invalid utf8", input: []byte{'"', 0xff, '"'}, want: jsonvalue.ErrInvalidUTF8},
		{name: "trailing", input: []byte(`true false`), want: jsonvalue.ErrTrailingData},
		{name: "malformed trailing", input: []byte(`true ]`), want: jsonvalue.ErrInvalidJSON},
		{name: "unexpected delimiter", input: []byte(`]`), want: jsonvalue.ErrInvalidJSON},
		{name: "malformed", input: []byte(`{"a":`), want: jsonvalue.ErrInvalidJSON},
		{name: "malformed name", input: []byte(`{"\uZZZZ":1}`), want: jsonvalue.ErrInvalidJSON},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := jsonvalue.Parse(test.input, jsonvalue.DefaultPolicy())
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseEnforcesResourcePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		policy jsonvalue.Policy
		want   error
	}{
		{
			name:   "bytes",
			input:  `"12345"`,
			policy: jsonvalue.Policy{MaxBytes: 3, MaxDepth: 8, MaxTokens: 8},
			want:   jsonvalue.ErrByteLimit,
		},
		{
			name:   "depth",
			input:  `[[[0]]]`,
			policy: jsonvalue.Policy{MaxBytes: 16, MaxDepth: 2, MaxTokens: 16},
			want:   jsonvalue.ErrDepthLimit,
		},
		{
			name:   "object depth",
			input:  `{"a":{"b":{}}}`,
			policy: jsonvalue.Policy{MaxBytes: 32, MaxDepth: 2, MaxTokens: 16},
			want:   jsonvalue.ErrDepthLimit,
		},
		{
			name:   "tokens",
			input:  `[1,2,3]`,
			policy: jsonvalue.Policy{MaxBytes: 16, MaxDepth: 8, MaxTokens: 3},
			want:   jsonvalue.ErrTokenLimit,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := jsonvalue.Parse([]byte(test.input), test.policy)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()

	for _, policy := range []jsonvalue.Policy{
		{MaxBytes: 0, MaxDepth: 1, MaxTokens: 1},
		{MaxBytes: 4, MaxDepth: 0, MaxTokens: 1},
		{MaxBytes: 4, MaxDepth: 1, MaxTokens: 0},
	} {
		_, err := jsonvalue.Parse([]byte(`null`), policy)
		if !errors.Is(err, jsonvalue.ErrInvalidPolicy) {
			t.Fatalf("policy %#v error = %v, want ErrInvalidPolicy", policy, err)
		}
	}
}

func TestParseAcceptsValuesExactlyAtResourceBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input  string
		policy jsonvalue.Policy
	}{
		{input: `null`, policy: jsonvalue.Policy{MaxBytes: 4, MaxDepth: 1, MaxTokens: 1}},
		{input: `{"a":{}}`, policy: jsonvalue.Policy{MaxBytes: 8, MaxDepth: 2, MaxTokens: 5}},
		{input: `[[]]`, policy: jsonvalue.Policy{MaxBytes: 4, MaxDepth: 2, MaxTokens: 4}},
	}
	for _, test := range tests {
		if _, err := jsonvalue.Parse([]byte(test.input), test.policy); err != nil {
			t.Errorf("Parse(%s, %#v): %v", test.input, test.policy, err)
		}
	}
}

func TestValueMarshalsWithoutSharingStorage(t *testing.T) {
	t.Parallel()

	value, err := jsonvalue.Parse([]byte(`{"value":12345678901234567890}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"value":12345678901234567890}` {
		t.Fatalf("Marshal(value) = %s", encoded)
	}
	encoded[0] = '['
	if value.Bytes()[0] != '{' {
		t.Fatal("MarshalJSON exposed mutable internal storage")
	}
}

func TestZeroValueCannotMarshal(t *testing.T) {
	t.Parallel()

	_, err := json.Marshal(jsonvalue.Value{})
	if !errors.Is(err, jsonvalue.ErrInvalidJSON) {
		t.Fatalf("error = %v, want ErrInvalidJSON", err)
	}
}

func TestParseAcceptsEveryJSONValueKind(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		`null`,
		`true`,
		`false`,
		`0`,
		`-1.25e+30`,
		`"text"`,
		`[]`,
		`{}`,
		`{"nested":[{"value":null}]}`,
	} {
		if _, err := jsonvalue.Parse([]byte(input), jsonvalue.DefaultPolicy()); err != nil {
			t.Errorf("Parse(%s): %v", input, err)
		}
	}
}

func TestParseRejectsIncompleteContainers(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		``,
		`[`,
		`[1`,
		`{`,
		`{"name"`,
		`{"name":`,
	} {
		_, err := jsonvalue.Parse([]byte(input), jsonvalue.DefaultPolicy())
		if !errors.Is(err, jsonvalue.ErrInvalidJSON) {
			t.Errorf("Parse(%q) error = %v, want ErrInvalidJSON", input, err)
		}
	}
}

func FuzzParseNeverPanics(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`null`),
		[]byte(`{"a":[1,true,null]}`),
		[]byte(`{"a":1,"a":2}`),
		{'"', 0xff, '"'},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		policy := jsonvalue.Policy{
			MaxBytes:  4096,
			MaxDepth:  32,
			MaxTokens: 1024,
		}
		value, err := jsonvalue.Parse(input, policy)
		if err != nil {
			return
		}
		if !bytes.Equal(value.Bytes(), input) {
			t.Fatal("accepted value did not preserve its input")
		}
	})
}
