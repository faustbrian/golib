package jsonwire_test

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/jsonwire"
)

type message struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

func TestDecodeFixture(t *testing.T) {
	t.Parallel()

	payload := readFixture(t, "valid.json")
	var got message

	if err := jsonwire.Decode(payload, &got, jsonwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got != (message{ID: 42, Label: "carrier"}) {
		t.Fatalf("Decode() = %#v", got)
	}
}

func TestDecodeReaderRejectsUnknownFieldsWhenRequested(t *testing.T) {
	t.Parallel()

	var got message
	err := jsonwire.DecodeReader(
		strings.NewReader(`{"id":42,"label":"carrier","vendor_code":"x"}`),
		&got,
		jsonwire.DecodeOptions{DisallowUnknownFields: true},
	)

	assertKind(t, err, wire.ErrValidation)
}

func TestDecodeRejectsMalformedAndTrailingValues(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"malformed.json", "trailing.json"} {
		var got message
		err := jsonwire.Decode(readFixture(t, name), &got, jsonwire.DecodeOptions{})
		assertKind(t, err, wire.ErrParse)
	}
}

func TestDecodeRejectsInvalidUTF8InsteadOfReplacingIt(t *testing.T) {
	t.Parallel()

	payload := []byte{'{', '"', 'v', 'a', 'l', 'u', 'e', '"', ':', '"', 0xff, '"', '}'}
	var target map[string]string
	assertKind(t, jsonwire.Decode(payload, &target, jsonwire.DecodeOptions{}), wire.ErrParse)
	if len(target) != 0 {
		t.Fatalf("Decode() mutated target = %#v", target)
	}
	_, err := jsonwire.Normalize(payload, jsonwire.NormalizeOptions{})
	assertKind(t, err, wire.ErrParse)
}

func TestDecodeRejectsInvalidTargetAndOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		target  any
		options jsonwire.DecodeOptions
		kind    error
	}{
		{name: "nil target", target: nil, kind: wire.ErrTarget},
		{name: "non-pointer target", target: message{}, kind: wire.ErrTarget},
		{name: "negative limit", target: &message{}, options: jsonwire.DecodeOptions{MaxBytes: -1}, kind: wire.ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := jsonwire.Decode([]byte(`{"id":42}`), tt.target, tt.options)
			assertKind(t, err, tt.kind)
		})
	}
}

func TestDecodeReaderEnforcesSizeLimitAndReportsReadFailures(t *testing.T) {
	t.Parallel()

	var got message
	err := jsonwire.DecodeReader(strings.NewReader(`{"id":42}`), &got, jsonwire.DecodeOptions{MaxBytes: 4})
	if !errors.Is(err, jsonwire.ErrPayloadTooLarge) {
		t.Fatalf("DecodeReader() error = %v, want payload too large", err)
	}
	assertKind(t, err, wire.ErrSizeLimit)

	err = jsonwire.DecodeReader(failingReader{}, &got, jsonwire.DecodeOptions{})
	assertKind(t, err, wire.ErrParse)

	err = jsonwire.DecodeReader(nil, &got, jsonwire.DecodeOptions{})
	assertKind(t, err, wire.ErrValidation)

	err = jsonwire.DecodeReader(strings.NewReader(`{"id":42}`), &got, jsonwire.DecodeOptions{MaxBytes: math.MaxInt64})
	if err != nil {
		t.Fatalf("DecodeReader() with maximum limit error = %v", err)
	}
}

func TestEncodeIsDeterministicAndConfigurable(t *testing.T) {
	t.Parallel()

	value := map[string]string{"z": "<unsafe>", "a": "first"}

	got, err := jsonwire.Encode(value, jsonwire.EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if want := `{"a":"first","z":"\u003cunsafe\u003e"}`; string(got) != want {
		t.Fatalf("Encode() = %q, want %q", got, want)
	}

	got, err = jsonwire.Encode(value, jsonwire.EncodeOptions{Indent: "  ", DisableHTMLEscaping: true})
	if err != nil {
		t.Fatalf("Encode() configured error = %v", err)
	}
	if want := "{\n  \"a\": \"first\",\n  \"z\": \"<unsafe>\"\n}"; string(got) != want {
		t.Fatalf("Encode() configured = %q, want %q", got, want)
	}
}

func TestEncodeClassifiesUnsupportedValues(t *testing.T) {
	t.Parallel()
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	_, err := jsonwire.Encode(cyclic, jsonwire.EncodeOptions{})
	assertKind(t, err, wire.ErrValidation)

	for _, options := range []jsonwire.EncodeOptions{
		{},
		{Indent: "  "},
		{DisableHTMLEscaping: true},
	} {
		_, err := jsonwire.Encode(make(chan int), options)
		assertKind(t, err, wire.ErrValidation)
	}
}

func TestEncodeWriterWritesDeterministicJSON(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	err := jsonwire.EncodeWriter(&output, map[string]int{"z": 2, "a": 1}, jsonwire.EncodeOptions{})
	if err != nil {
		t.Fatalf("EncodeWriter() error = %v", err)
	}
	if got, want := output.String(), `{"a":1,"z":2}`; got != want {
		t.Fatalf("EncodeWriter() = %q, want %q", got, want)
	}
}

func TestEncodeWriterClassifiesWriterFailures(t *testing.T) {
	t.Parallel()

	for _, writer := range []interface{ Write([]byte) (int, error) }{errorWriter{}, shortWriter{}} {
		err := jsonwire.EncodeWriter(writer, message{ID: 42}, jsonwire.EncodeOptions{})
		assertKind(t, err, wire.ErrWrite)
	}
	assertKind(t, jsonwire.EncodeWriter(nil, message{}, jsonwire.EncodeOptions{}), wire.ErrValidation)
	assertKind(t, jsonwire.EncodeWriter(&bytes.Buffer{}, make(chan int), jsonwire.EncodeOptions{}), wire.ErrValidation)
}

func TestNormalizeMakesVendorJSONCanonical(t *testing.T) {
	t.Parallel()

	payload := append([]byte{0xef, 0xbb, 0xbf}, []byte("  { \"z\" : 1.20, \"a\": [ true ] } \n")...)
	got, err := jsonwire.Normalize(payload, jsonwire.NormalizeOptions{})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if want := `{"a":[true],"z":1.20}`; string(got) != want {
		t.Fatalf("Normalize() = %q, want %q", got, want)
	}
}

func TestNormalizeRejectsInvalidPayloadAndOptions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		payload []byte
		options jsonwire.NormalizeOptions
	}{
		{payload: []byte(`{"broken":}`)},
		{payload: []byte(`{} {}`)},
		{payload: []byte(`{"large":true}`), options: jsonwire.NormalizeOptions{MaxBytes: 3}},
		{payload: []byte(`{}`), options: jsonwire.NormalizeOptions{MaxBytes: -1}},
	} {
		_, err := jsonwire.Normalize(tc.payload, tc.options)
		if err == nil {
			t.Fatal("Normalize() error = nil")
		}
	}
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte(`{"id":42,"label":"carrier"}`))
	f.Add(readFixture(f, "malformed.json"))
	f.Add([]byte{'{', '"', 'v', '"', ':', '"', 0xff, '"', '}'})
	f.Add([]byte{})
	f.Add([]byte(" \t\r\n"))
	f.Add([]byte("\xef\xbb\xbf{\"bom\":true}"))
	f.Add([]byte(`{"duplicate":1,"duplicate":2}`))
	f.Add([]byte(`{"number":01}`))
	f.Add([]byte(`{}[]`))
	f.Add([]byte(strings.Repeat("[", 10_001) + "0" + strings.Repeat("]", 10_001)))

	f.Fuzz(func(t *testing.T, payload []byte) {
		var target any
		_ = jsonwire.Decode(payload, &target, jsonwire.DecodeOptions{MaxBytes: 64 << 10})
	})
}

func BenchmarkDecode(b *testing.B) {
	payload := readFixture(b, "valid.json")
	b.ReportAllocs()

	for b.Loop() {
		var target message
		if err := jsonwire.Decode(payload, &target, jsonwire.DecodeOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	value := message{ID: 42, Label: "carrier"}
	b.ReportAllocs()

	for b.Loop() {
		if _, err := jsonwire.Encode(value, jsonwire.EncodeOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func readFixture(tb testing.TB, name string) []byte {
	tb.Helper()

	payload, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		tb.Fatal(err)
	}

	return payload
}

func assertKind(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want errors.Is(_, %v)", err, target)
	}

	var wireErr *wire.Error
	if !errors.As(err, &wireErr) || wireErr.Format != wire.FormatJSON {
		t.Fatalf("error = %#v, want JSON *wire.Error", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("vendor stream failed")
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("vendor stream failed")
}

type shortWriter struct{}

func (shortWriter) Write(payload []byte) (int, error) {
	return len(payload) - 1, nil
}

func TestNormalizeDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	payload := []byte(" \t{\"ok\":true}\n")
	original := bytes.Clone(payload)
	if _, err := jsonwire.Normalize(payload, jsonwire.NormalizeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, original) {
		t.Fatalf("Normalize() mutated input: %q", payload)
	}
}
