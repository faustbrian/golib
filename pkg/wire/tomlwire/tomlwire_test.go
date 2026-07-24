package tomlwire_test

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/tomlwire"
)

type service struct {
	Name       string            `toml:"service"`
	Retries    int8              `toml:"retries"`
	Enabled    bool              `toml:"enabled"`
	DeployedAt time.Time         `toml:"deployed_at"`
	Labels     map[string]string `toml:"labels"`
}

func TestDecodeFixturePreservesDatetimeAndNumericTypes(t *testing.T) {
	t.Parallel()
	var got service
	if err := tomlwire.Decode(readFixture(t, "service.toml"), &got, tomlwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	wantTime := time.Date(2026, 7, 14, 9, 30, 0, 0, time.UTC)
	if got.Name != "billing" || got.Retries != 3 || got.DeployedAt != wantTime || got.Labels["region"] != "eu-north" {
		t.Fatalf("Decode() = %#v", got)
	}
}

func TestDecodeRejectsMalformedDuplicateAndTrailingData(t *testing.T) {
	t.Parallel()
	for name, payload := range map[string]string{
		"malformed":       "service = [broken\n",
		"duplicate key":   "service = 'first'\nservice = 'second'\n",
		"dotted conflict": "service = 'first'\nservice.name = 'second'\n",
		"trailing data":   "service = 'ok'\nnot toml\n",
	} {
		t.Run(name, func(t *testing.T) {
			var got service
			assertKind(t, tomlwire.Decode([]byte(payload), &got, tomlwire.DecodeOptions{}), wire.ErrParse)
		})
	}
}

func TestDecodePreservesSpecialFloatsAndArraysOfTables(t *testing.T) {
	t.Parallel()

	var got struct {
		Positive  float64 `toml:"positive"`
		Negative  float64 `toml:"negative"`
		NotNumber float64 `toml:"not_number"`
		Workers   []struct {
			Name string `toml:"name"`
		} `toml:"workers"`
	}
	payload := []byte("positive = inf\nnegative = -inf\nnot_number = nan\n[[workers]]\nname = 'a'\n[[workers]]\nname = 'b'\n")
	if err := tomlwire.Decode(payload, &got, tomlwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !math.IsInf(got.Positive, 1) || !math.IsInf(got.Negative, -1) || !math.IsNaN(got.NotNumber) {
		t.Fatalf("special floats = %#v", got)
	}
	if len(got.Workers) != 2 || got.Workers[0].Name != "a" || got.Workers[1].Name != "b" {
		t.Fatalf("workers = %#v", got.Workers)
	}
}

func TestDecodeRejectsUnknownFieldsAndNumericLossWhenRequested(t *testing.T) {
	t.Parallel()
	var got service
	assertKind(t, tomlwire.Decode(
		[]byte("service = 'billing'\nunknown = true\n"),
		&got,
		tomlwire.DecodeOptions{DisallowUnknownFields: true},
	), wire.ErrValidation)
	assertKind(t, tomlwire.Decode([]byte("retries = 300\n"), &got, tomlwire.DecodeOptions{}), wire.ErrValidation)
	var scalar int
	assertKind(t, tomlwire.Decode([]byte("value = 1\n"), &scalar, tomlwire.DecodeOptions{}), wire.ErrValidation)
}

func TestDecodeRejectsInvalidTargetAndOptions(t *testing.T) {
	t.Parallel()
	var typedNil *service
	for _, test := range []struct {
		target  any
		options tomlwire.DecodeOptions
		kind    error
	}{
		{target: nil, kind: wire.ErrTarget},
		{target: service{}, kind: wire.ErrTarget},
		{target: typedNil, kind: wire.ErrTarget},
		{target: &service{}, options: tomlwire.DecodeOptions{MaxBytes: -1}, kind: wire.ErrValidation},
	} {
		assertKind(t, tomlwire.Decode([]byte("service = 'billing'\n"), test.target, test.options), test.kind)
	}
}

func TestDecodeReaderEnforcesLimitsAndClassifiesReadFailures(t *testing.T) {
	t.Parallel()
	var got service
	assertKind(t, tomlwire.DecodeReader(strings.NewReader("service = 'billing'\n"), &got, tomlwire.DecodeOptions{MaxBytes: 4}), wire.ErrSizeLimit)
	assertKind(t, tomlwire.DecodeReader(errorReader{}, &got, tomlwire.DecodeOptions{}), wire.ErrParse)
	assertKind(t, tomlwire.DecodeReader(nil, &got, tomlwire.DecodeOptions{}), wire.ErrValidation)
	if err := tomlwire.DecodeReader(strings.NewReader("service = 'billing'\n"), &got, tomlwire.DecodeOptions{MaxBytes: math.MaxInt64}); err != nil {
		t.Fatalf("DecodeReader() maximum limit error = %v", err)
	}
}

func TestEncodeIsDeterministicAndPreservesNativeTypes(t *testing.T) {
	t.Parallel()
	value := map[string]any{
		"z":           "last",
		"a":           "first",
		"deployed_at": time.Date(2026, 7, 14, 9, 30, 0, 0, time.UTC),
	}
	first, err := tomlwire.Encode(value, tomlwire.EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	second, err := tomlwire.Encode(value, tomlwire.EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode() repeat error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("Encode() is not deterministic: %q != %q", first, second)
	}
	want := "a = \"first\"\ndeployed_at = 2026-07-14T09:30:00Z\nz = \"last\"\n"
	if string(first) != want {
		t.Fatalf("Encode() = %q, want %q", first, want)
	}
}

func TestEncodeSupportsIndentAndClassifiesFailures(t *testing.T) {
	t.Parallel()
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	_, err := tomlwire.Encode(cyclic, tomlwire.EncodeOptions{})
	assertKind(t, err, wire.ErrEncode)
	value := map[string]any{"outer": map[string]string{"value": "nested"}}
	got, err := tomlwire.Encode(value, tomlwire.EncodeOptions{Indent: "    "})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if !strings.Contains(string(got), "    value = \"nested\"") {
		t.Fatalf("Encode() configured = %q", got)
	}
	_, err = tomlwire.Encode(make(chan int), tomlwire.EncodeOptions{})
	assertKind(t, err, wire.ErrUnsupportedFormat)
	_, err = tomlwire.Encode(map[string]any{"broken": failingMarshaler{}}, tomlwire.EncodeOptions{})
	assertKind(t, err, wire.ErrEncode)
	_, err = tomlwire.Encode(map[string]string{"ok": "yes"}, tomlwire.EncodeOptions{Indent: "\n"})
	assertKind(t, err, wire.ErrValidation)
}

func TestEncodeWriterWritesAndClassifiesFailures(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	if err := tomlwire.EncodeWriter(&output, map[string]string{"status": "ok"}, tomlwire.EncodeOptions{}); err != nil {
		t.Fatalf("EncodeWriter() error = %v", err)
	}
	if output.String() != "status = \"ok\"\n" {
		t.Fatalf("EncodeWriter() = %q", output.String())
	}
	assertKind(t, tomlwire.EncodeWriter(errorWriter{}, service{}, tomlwire.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, tomlwire.EncodeWriter(shortWriter{}, service{}, tomlwire.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, tomlwire.EncodeWriter(nil, service{}, tomlwire.EncodeOptions{}), wire.ErrValidation)
	assertKind(t, tomlwire.EncodeWriter(&bytes.Buffer{}, make(chan int), tomlwire.EncodeOptions{}), wire.ErrUnsupportedFormat)
}

func FuzzDecode(f *testing.F) {
	f.Add(readFixture(f, "service.toml"))
	f.Add([]byte("service = [broken\n"))
	f.Add([]byte{})
	f.Add([]byte(" \t\r\n"))
	f.Add([]byte("key = 1\nkey = 2\n"))
	f.Add([]byte("a.b = 1\n[a]\nb = 2\n"))
	f.Add([]byte("integer = 9223372036854775808\n"))
	f.Add([]byte("datetime = 1979-05-27T07:32:00.999999999Z\n"))
	f.Fuzz(func(t *testing.T, payload []byte) {
		var target any
		_ = tomlwire.Decode(payload, &target, tomlwire.DecodeOptions{MaxBytes: 64 << 10})
	})
}

func BenchmarkDecode(b *testing.B) {
	payload := readFixture(b, "service.toml")
	b.ReportAllocs()
	for b.Loop() {
		var target service
		if err := tomlwire.Decode(payload, &target, tomlwire.DecodeOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	value := service{Name: "billing", Retries: 3, Enabled: true}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := tomlwire.Encode(value, tomlwire.EncodeOptions{}); err != nil {
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
	if !errors.As(err, &wireErr) || wireErr.Format != wire.FormatTOML {
		t.Fatalf("error = %#v, want TOML *wire.Error", err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("fixture reader failed") }

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("fixture writer failed") }

type shortWriter struct{}

func (shortWriter) Write(payload []byte) (int, error) { return len(payload) - 1, nil }

type failingMarshaler struct{}

func (failingMarshaler) MarshalTOML() ([]byte, error) {
	return nil, errors.New("fixture marshal failed")
}
