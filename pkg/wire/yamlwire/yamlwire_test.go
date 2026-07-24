package yamlwire_test

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/yamlwire"
	"go.yaml.in/yaml/v4"
)

type manifest struct {
	Service string            `yaml:"service"`
	Image   string            `yaml:"image"`
	Labels  map[string]string `yaml:"labels"`
}

func TestDecodeFixture(t *testing.T) {
	t.Parallel()
	var got manifest
	if err := yamlwire.Decode(readFixture(t, "manifest.yaml"), &got, yamlwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Service != "tracking" || got.Image != "registry.invalid/tracking:v1" || got.Labels["region"] != "eu-north" {
		t.Fatalf("Decode() = %#v", got)
	}
}

func TestDecodeReaderRejectsUnknownFieldsWhenRequested(t *testing.T) {
	t.Parallel()
	var got manifest
	err := yamlwire.DecodeReader(strings.NewReader("service: tracking\nunknown: true\n"), &got, yamlwire.DecodeOptions{DisallowUnknownFields: true})
	assertKind(t, err, wire.ErrValidation)
}

func TestDecodeRejectsMalformedDuplicateAndMultipleDocuments(t *testing.T) {
	t.Parallel()
	for name, payload := range map[string]string{
		"malformed":          "service: [broken\n",
		"duplicate key":      "service: first\nservice: second\n",
		"multiple documents": "service: first\n---\nservice: second\n",
	} {
		t.Run(name, func(t *testing.T) {
			var got manifest
			assertKind(t, yamlwire.Decode([]byte(payload), &got, yamlwire.DecodeOptions{}), wire.ErrParse)
		})
	}
}

func TestDecodeCanExplicitlyAllowDuplicateKeys(t *testing.T) {
	t.Parallel()
	var got manifest
	err := yamlwire.Decode(
		[]byte("service: first\nservice: second\n"),
		&got,
		yamlwire.DecodeOptions{AllowDuplicateKeys: true},
	)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Service != "second" {
		t.Fatalf("Decode() service = %q", got.Service)
	}
}

func TestDecodeMultipleDocumentsRequiresSliceTarget(t *testing.T) {
	t.Parallel()
	payload := []byte("service: first\n---\nservice: second\n")
	var got []manifest
	if err := yamlwire.Decode(payload, &got, yamlwire.DecodeOptions{AllowMultipleDocuments: true}); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(got) != 2 || got[0].Service != "first" || got[1].Service != "second" {
		t.Fatalf("Decode() = %#v", got)
	}
	var single manifest
	assertKind(t, yamlwire.Decode(payload, &single, yamlwire.DecodeOptions{AllowMultipleDocuments: true}), wire.ErrTarget)
}

func TestDecodeDefinesAliasAnchorAndMergeBehavior(t *testing.T) {
	t.Parallel()
	payload := []byte("defaults: &defaults\n  image: stable\nservice:\n  <<: *defaults\n")
	var got map[string]map[string]string
	if err := yamlwire.Decode(payload, &got, yamlwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() aliases error = %v", err)
	}
	if got["service"]["image"] != "stable" {
		t.Fatalf("Decode() merge = %#v", got)
	}
	assertKind(t, yamlwire.Decode(payload, &got, yamlwire.DecodeOptions{DisallowAliases: true}), wire.ErrUnsupportedFormat)
	assertKind(t, yamlwire.Decode(payload, &got, yamlwire.DecodeOptions{DisallowMergeKeys: true}), wire.ErrUnsupportedFormat)
	manyAliases := []byte("defaults: &defaults\n  image: stable\none: *defaults\ntwo: *defaults\n")
	assertKind(t, yamlwire.Decode(manyAliases, &got, yamlwire.DecodeOptions{MaxAliases: 1}), wire.ErrSizeLimit)

	var scalar map[string]string
	if err := yamlwire.Decode(
		[]byte("value: '<<'\n"),
		&scalar,
		yamlwire.DecodeOptions{DisallowMergeKeys: true},
	); err != nil {
		t.Fatalf("Decode() quoted merge-like scalar error = %v", err)
	}
	if scalar["value"] != "<<" {
		t.Fatalf("Decode() quoted merge-like scalar = %#v", scalar)
	}
}

func TestDecodeDefinesTagsImplicitTypesAndNonJSONKeys(t *testing.T) {
	t.Parallel()

	var tagged yaml.Node
	if err := yamlwire.Decode([]byte("value: !vendor abc\n"), &tagged, yamlwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	value := tagged.Content[0].Content[1]
	if value.Tag != "!vendor" || value.Value != "abc" {
		t.Fatalf("tagged value = %#v", value)
	}

	var implicit map[string]any
	if err := yamlwire.Decode([]byte("enabled: true\ncount: 42\n"), &implicit, yamlwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if implicit["enabled"] != true || implicit["count"] != int(42) {
		t.Fatalf("implicit values = %#v", implicit)
	}

	var numericKey map[int]string
	if err := yamlwire.Decode([]byte("1: one\n"), &numericKey, yamlwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if numericKey[1] != "one" {
		t.Fatalf("numeric-key map = %#v", numericKey)
	}
}

func TestDecodeEnforcesDepthLimit(t *testing.T) {
	t.Parallel()
	var got any
	err := yamlwire.Decode([]byte("a:\n  b:\n    c: value\n"), &got, yamlwire.DecodeOptions{MaxDepth: 2})
	assertKind(t, err, wire.ErrSizeLimit)
}

func TestDecodeClassifiesBuiltInResourceProtectionAsSizeLimit(t *testing.T) {
	t.Parallel()

	for name, payload := range map[string][]byte{
		"alias expansion": []byte(`{a: &a [{a}` +
			strings.Repeat(`,{a}`, 1000*1024/4-100) +
			`], b: &b [*a` + strings.Repeat(`,*a`, 99) + `]}`),
		"nesting depth": []byte(strings.Repeat("[", 10001) +
			"x" + strings.Repeat("]", 10001)),
	} {
		t.Run(name, func(t *testing.T) {
			var got any
			assertKind(t, yamlwire.Decode(payload, &got, yamlwire.DecodeOptions{
				MaxBytes: 2 << 20,
			}), wire.ErrSizeLimit)
		})
	}
}

func TestDecodeRejectsInvalidTargetAndOptions(t *testing.T) {
	t.Parallel()
	var typedNil *manifest
	tests := []struct {
		target  any
		options yamlwire.DecodeOptions
		kind    error
	}{
		{target: nil, kind: wire.ErrTarget},
		{target: manifest{}, kind: wire.ErrTarget},
		{target: typedNil, kind: wire.ErrTarget},
		{target: &manifest{}, options: yamlwire.DecodeOptions{MaxBytes: -1}, kind: wire.ErrValidation},
		{target: &manifest{}, options: yamlwire.DecodeOptions{MaxDepth: -1}, kind: wire.ErrValidation},
		{target: &manifest{}, options: yamlwire.DecodeOptions{MaxAliases: -1}, kind: wire.ErrValidation},
	}
	for _, tt := range tests {
		assertKind(t, yamlwire.Decode([]byte("service: tracking\n"), tt.target, tt.options), tt.kind)
	}
}

func TestDecodeReaderEnforcesLimitsAndClassifiesReadFailures(t *testing.T) {
	t.Parallel()
	var got manifest
	assertKind(t, yamlwire.DecodeReader(strings.NewReader("service: tracking\n"), &got, yamlwire.DecodeOptions{MaxBytes: 4}), wire.ErrSizeLimit)
	assertKind(t, yamlwire.DecodeReader(errorReader{}, &got, yamlwire.DecodeOptions{}), wire.ErrParse)
	assertKind(t, yamlwire.DecodeReader(nil, &got, yamlwire.DecodeOptions{}), wire.ErrValidation)
	if err := yamlwire.DecodeReader(strings.NewReader("service: tracking\n"), &got, yamlwire.DecodeOptions{MaxBytes: math.MaxInt64}); err != nil {
		t.Fatalf("DecodeReader() maximum limit error = %v", err)
	}
}

func TestEncodeIsDeterministicAndConfigurable(t *testing.T) {
	t.Parallel()
	value := map[string]any{"z": []string{"last"}, "a": "first"}
	first, err := yamlwire.Encode(value, yamlwire.EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	second, err := yamlwire.Encode(value, yamlwire.EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode() repeat error = %v", err)
	}
	if !bytes.Equal(first, second) || string(first) != "a: first\nz:\n- last\n" {
		t.Fatalf("Encode() = %q and %q", first, second)
	}
	indented, err := yamlwire.Encode(value, yamlwire.EncodeOptions{Indent: 4, DefaultSequenceIndent: true})
	if err != nil {
		t.Fatalf("Encode() configured error = %v", err)
	}
	if string(indented) != "a: first\nz:\n    - last\n" {
		t.Fatalf("Encode() configured = %q", indented)
	}
}

func TestEncodeClassifiesUnsupportedAndEncodeFailures(t *testing.T) {
	t.Parallel()
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	_, err := yamlwire.Encode(cyclic, yamlwire.EncodeOptions{})
	assertKind(t, err, wire.ErrEncode)
	_, err = yamlwire.Encode(make(chan int), yamlwire.EncodeOptions{})
	assertKind(t, err, wire.ErrUnsupportedFormat)
	_, err = yamlwire.Encode(failingMarshaler{}, yamlwire.EncodeOptions{})
	assertKind(t, err, wire.ErrEncode)
	_, err = yamlwire.Encode(map[string]string{"ok": "yes"}, yamlwire.EncodeOptions{Indent: 1})
	assertKind(t, err, wire.ErrValidation)
}

func TestEncodeRoundTripsControlCharactersAtLineBoundaries(t *testing.T) {
	t.Parallel()

	want := struct {
		Text string `yaml:"text"`
	}{Text: "\t\n0"}
	payload, err := yamlwire.Encode(want, yamlwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Text string `yaml:"text"`
	}
	if err := yamlwire.Decode(payload, &got, yamlwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode(%q) error = %v", payload, err)
	}
	if got != want {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
	if !bytes.Contains(payload, []byte("|2-")) {
		t.Fatalf("Encode() = %q, want explicit block indentation", payload)
	}
	if _, err := yamlwire.Encode(want, yamlwire.EncodeOptions{MaxBytes: int64(len(payload) - 1)}); !errors.Is(err, wire.ErrSizeLimit) {
		t.Fatalf("rewritten boundary error = %v", err)
	}
}

func TestEncodeDoesNotRewritePlainScalarSuffix(t *testing.T) {
	t.Parallel()

	want := map[string]string{"text": "000000000000000 >"}
	payload, err := yamlwire.Encode(want, yamlwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := yamlwire.Decode(payload, &got, yamlwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode(%q) error = %v", payload, err)
	}
	if got["text"] != want["text"] {
		t.Fatalf("round trip = %q, want %q", got["text"], want["text"])
	}
}

func TestEncodeWriterWritesAndClassifiesFailures(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	if err := yamlwire.EncodeWriter(&output, map[string]string{"status": "ok"}, yamlwire.EncodeOptions{}); err != nil {
		t.Fatalf("EncodeWriter() error = %v", err)
	}
	if output.String() != "status: ok\n" {
		t.Fatalf("EncodeWriter() = %q", output.String())
	}
	assertKind(t, yamlwire.EncodeWriter(errorWriter{}, manifest{}, yamlwire.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, yamlwire.EncodeWriter(shortWriter{}, manifest{}, yamlwire.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, yamlwire.EncodeWriter(nil, manifest{}, yamlwire.EncodeOptions{}), wire.ErrValidation)
	assertKind(t, yamlwire.EncodeWriter(&bytes.Buffer{}, make(chan int), yamlwire.EncodeOptions{}), wire.ErrUnsupportedFormat)
}

func FuzzDecode(f *testing.F) {
	f.Add(readFixture(f, "manifest.yaml"))
	f.Add([]byte("service: [broken\n"))
	f.Add([]byte{})
	f.Add([]byte(" \t\r\n"))
	f.Add([]byte("key: first\nkey: second\n"))
	f.Add([]byte("first: value\n---\nsecond: value\n"))
	f.Add([]byte("base: &base value\none: *base\ntwo: *base\n"))
	f.Add([]byte("recursive: &recursive [*recursive]\n"))
	f.Add([]byte("tagged: !vendor value\n"))
	f.Add([]byte("text: |2-\n  \t\n  0\n"))
	f.Fuzz(func(t *testing.T, payload []byte) {
		var target any
		_ = yamlwire.Decode(payload, &target, yamlwire.DecodeOptions{MaxBytes: 64 << 10})
	})
}

func BenchmarkDecode(b *testing.B) {
	payload := readFixture(b, "manifest.yaml")
	b.ReportAllocs()
	for b.Loop() {
		var target manifest
		if err := yamlwire.Decode(payload, &target, yamlwire.DecodeOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	value := manifest{Service: "tracking", Image: "registry.invalid/tracking:v1"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := yamlwire.Encode(value, yamlwire.EncodeOptions{}); err != nil {
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
	if !errors.As(err, &wireErr) || wireErr.Format != wire.FormatYAML {
		t.Fatalf("error = %#v, want YAML *wire.Error", err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("fixture reader failed") }

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("fixture writer failed") }

type shortWriter struct{}

func (shortWriter) Write(payload []byte) (int, error) { return len(payload) - 1, nil }

type failingMarshaler struct{}

func (failingMarshaler) MarshalYAML() (any, error) { return nil, errors.New("fixture marshal failed") }
