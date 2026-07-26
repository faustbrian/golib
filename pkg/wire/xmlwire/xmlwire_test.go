package xmlwire_test

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/xmlwire"
)

type shipment struct {
	XMLName xml.Name `xml:"urn:vendor Shipment"`
	ID      int      `xml:"urn:vendor ID"`
	Label   string   `xml:"urn:vendor Label"`
}

func TestDecodeNamespaceAwareFixture(t *testing.T) {
	t.Parallel()

	var got shipment
	err := xmlwire.Decode(readFixture(t, "shipment.xml"), &got, xmlwire.DecodeOptions{
		ExpectedRoot: xml.Name{Space: "urn:vendor", Local: "Shipment"},
	})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.ID != 42 || got.Label != "carrier" || got.XMLName.Space != "urn:vendor" {
		t.Fatalf("Decode() = %#v", got)
	}
}

func TestRootReturnsResolvedNamespace(t *testing.T) {
	t.Parallel()

	got, err := xmlwire.Root(readFixture(t, "shipment.xml"), xmlwire.DecodeOptions{})
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	if want := (xml.Name{Space: "urn:vendor", Local: "Shipment"}); got != want {
		t.Fatalf("Root() = %#v, want %#v", got, want)
	}
}

func TestRootRejectsInvalidDocumentsAndLimits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		payload []byte
		options xmlwire.DecodeOptions
	}{
		{payload: []byte(`<!-- no root -->`)},
		{payload: []byte(`<root/>`), options: xmlwire.DecodeOptions{MaxBytes: 3}},
	} {
		_, err := xmlwire.Root(tc.payload, tc.options)
		if err == nil {
			t.Fatal("Root() error = nil")
		}
	}
}

func TestDecodeValidatesExpectedRoot(t *testing.T) {
	t.Parallel()

	var got shipment
	err := xmlwire.Decode(readFixture(t, "shipment.xml"), &got, xmlwire.DecodeOptions{
		ExpectedRoot: xml.Name{Space: "urn:other", Local: "Shipment"},
	})
	assertKind(t, err, wire.ErrValidation)
}

func TestDecodeEnforcesTokenDepthLimit(t *testing.T) {
	t.Parallel()

	payload := []byte(`<a><b><c/></b></a>`)
	var target any
	if err := xmlwire.Decode(payload, &target, xmlwire.DecodeOptions{MaxDepth: 3}); err != nil {
		t.Fatalf("exact depth error = %v", err)
	}
	err := xmlwire.Decode(payload, &target, xmlwire.DecodeOptions{MaxDepth: 2})
	if !errors.Is(err, wire.ErrSizeLimit) || !errors.Is(err, xmlwire.ErrNestingTooDeep) {
		t.Fatalf("depth error = %v", err)
	}
	if err := xmlwire.Decode(payload, &target, xmlwire.DecodeOptions{MaxDepth: -1}); !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("negative depth error = %v", err)
	}
	if _, err := xmlwire.Root(payload, xmlwire.DecodeOptions{MaxDepth: -1}); !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("negative root depth error = %v", err)
	}
}

func TestDecodeRejectsDepthBeyondSafeDefault(t *testing.T) {
	t.Parallel()

	payload := []byte(strings.Repeat("<a>", xmlwire.DefaultMaxDepth+1) + strings.Repeat("</a>", xmlwire.DefaultMaxDepth+1))
	var target any
	if err := xmlwire.Decode(payload, &target, xmlwire.DecodeOptions{}); !errors.Is(err, wire.ErrSizeLimit) {
		t.Fatalf("default depth error = %v", err)
	}
}

func TestDecodeRejectsMalformedAndTrailingDocuments(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"malformed.xml", "trailing.xml"} {
		var got shipment
		err := xmlwire.Decode(readFixture(t, name), &got, xmlwire.DecodeOptions{})
		assertKind(t, err, wire.ErrParse)
	}
}

func TestDecodeDoesNotExpandDeclaredEntities(t *testing.T) {
	t.Parallel()

	var target struct {
		Value string `xml:",chardata"`
	}
	if err := xmlwire.Decode([]byte("<!DOCTYPE root><root>ok</root>"), &target, xmlwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() harmless directive error = %v", err)
	}
	if target.Value != "ok" {
		t.Fatalf("Decode() value = %q", target.Value)
	}
	entity := []byte("<!DOCTYPE root [<!ENTITY x 'expanded'>]><root>&x;</root>")
	assertKind(t, xmlwire.Decode(entity, &target, xmlwire.DecodeOptions{}), wire.ErrParse)
	assertKind(t, xmlwire.Decode([]byte("<root>\x00</root>"), &target, xmlwire.DecodeOptions{}), wire.ErrParse)
}

func TestDecodeCanExplicitlyRecoverNonStrictXML(t *testing.T) {
	t.Parallel()

	var got struct {
		Value string `xml:"value"`
	}
	err := xmlwire.Decode([]byte(`<root><value>ok</root>`), &got, xmlwire.DecodeOptions{AllowNonStrict: true})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Value != "ok" {
		t.Fatalf("Decode() value = %q", got.Value)
	}
}

func TestDecodeSupportsCommonVendorCharsets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		charset string
		value   byte
		want    string
	}{
		{charset: "ISO-8859-1", value: 0xe4, want: "ä"},
		{charset: "windows-1252", value: 0x80, want: "€"},
		{charset: "US-ASCII", value: 'A', want: "A"},
	}

	for _, tt := range tests {
		t.Run(tt.charset, func(t *testing.T) {
			t.Parallel()
			payload := []byte(`<?xml version="1.0" encoding="` + tt.charset + `"?><root><value>`)
			payload = append(payload, tt.value)
			payload = append(payload, []byte(`</value></root>`)...)

			var got struct {
				Value string `xml:"value"`
			}
			if err := xmlwire.Decode(payload, &got, xmlwire.DecodeOptions{}); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got.Value != tt.want {
				t.Fatalf("Decode() value = %q, want %q", got.Value, tt.want)
			}
		})
	}
}

func TestDecodeRejectsUnsupportedAndInvalidCharsetBytes(t *testing.T) {
	t.Parallel()

	payloads := [][]byte{
		[]byte(`<?xml version="1.0" encoding="KOI8-R"?><root/>`),
		append([]byte(`<?xml version="1.0" encoding="US-ASCII"?><root>`), append([]byte{0xff}, []byte(`</root>`)...)...),
	}
	for _, payload := range payloads {
		var got any
		err := xmlwire.Decode(payload, &got, xmlwire.DecodeOptions{})
		assertKind(t, err, wire.ErrParse)
	}
}

func TestDecodeOptionsAndReaderFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		reader  io.Reader
		target  any
		options xmlwire.DecodeOptions
		kind    error
	}{
		{name: "nil reader", reader: nil, target: &shipment{}, kind: wire.ErrValidation},
		{name: "nil target", reader: strings.NewReader(`<root/>`), target: nil, kind: wire.ErrTarget},
		{name: "non-pointer", reader: strings.NewReader(`<root/>`), target: shipment{}, kind: wire.ErrTarget},
		{name: "negative limit", reader: strings.NewReader(`<root/>`), target: &shipment{}, options: xmlwire.DecodeOptions{MaxBytes: -1}, kind: wire.ErrValidation},
		{name: "too large", reader: strings.NewReader(`<root/>`), target: &shipment{}, options: xmlwire.DecodeOptions{MaxBytes: 3}, kind: wire.ErrSizeLimit},
		{name: "reader failure", reader: failingReader{}, target: &shipment{}, kind: wire.ErrParse},
		{name: "maximum limit", reader: strings.NewReader(`<root/>`), target: &struct{}{}, options: xmlwire.DecodeOptions{MaxBytes: math.MaxInt64}},
		{name: "shape mismatch", reader: strings.NewReader(`<Shipment xmlns="urn:vendor"><ID>not-a-number</ID></Shipment>`), target: &shipment{}, kind: wire.ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := xmlwire.DecodeReader(tt.reader, tt.target, tt.options)
			if tt.kind == nil {
				if err != nil {
					t.Fatalf("DecodeReader() error = %v", err)
				}
				return
			}
			assertKind(t, err, tt.kind)
		})
	}
}

func TestDecodeUsesCustomCharsetReader(t *testing.T) {
	t.Parallel()

	payload := []byte(`<?xml version="1.0" encoding="vendor"?><root><value>ok</value></root>`)
	var got struct {
		Value string `xml:"value"`
	}
	err := xmlwire.Decode(payload, &got, xmlwire.DecodeOptions{
		CharsetReader: func(_ string, input io.Reader) (io.Reader, error) { return input, nil },
	})
	if err != nil || got.Value != "ok" {
		t.Fatalf("Decode() = %#v, %v", got, err)
	}

	err = xmlwire.Decode(payload, &got, xmlwire.DecodeOptions{
		CharsetReader: func(string, io.Reader) (io.Reader, error) { return nil, errors.New("bad vendor encoding") },
	})
	assertKind(t, err, wire.ErrParse)
}

func TestCharsetReaderContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		label   string
		input   []byte
		want    string
		wantErr bool
	}{
		{name: "UTF-8", label: " UTF8 ", input: []byte("ä"), want: "ä"},
		{name: "invalid UTF-8", label: "utf-8", input: []byte{0xff}, wantErr: true},
		{name: "Windows ASCII", label: "cp1252", input: []byte("A"), want: "A"},
		{name: "undefined Windows byte", label: "windows-1252", input: []byte{0x81}, wantErr: true},
		{name: "unknown", label: "KOI8-R", input: []byte("x"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reader, err := xmlwire.CharsetReader(tt.label, strings.NewReader(string(tt.input)))
			if tt.wantErr {
				if err == nil {
					t.Fatal("CharsetReader() error = nil")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			if err != nil || string(got) != tt.want {
				t.Fatalf("CharsetReader() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}

	if _, err := xmlwire.CharsetReader("latin1", failingReader{}); err == nil {
		t.Fatal("CharsetReader() read error = nil")
	}
}

func TestEncodeIsDeterministicAndConfigurable(t *testing.T) {
	t.Parallel()

	value := shipment{ID: 42, Label: "carrier"}
	got, err := xmlwire.Encode(value, xmlwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := `<Shipment xmlns="urn:vendor"><ID xmlns="urn:vendor">42</ID><Label xmlns="urn:vendor">carrier</Label></Shipment>`; string(got) != want {
		t.Fatalf("Encode() = %q, want %q", got, want)
	}

	got, err = xmlwire.Encode(value, xmlwire.EncodeOptions{Indent: "  ", IncludeHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), xml.Header+"<Shipment") || !strings.Contains(string(got), "\n  <ID") {
		t.Fatalf("Encode() configured = %q", got)
	}
}

func TestEncodeClassifiesUnsupportedValues(t *testing.T) {
	t.Parallel()
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	_, err := xmlwire.Encode(cyclic, xmlwire.EncodeOptions{})
	assertKind(t, err, wire.ErrValidation)

	for _, options := range []xmlwire.EncodeOptions{{}, {Indent: "  "}} {
		_, err := xmlwire.Encode(make(chan int), options)
		assertKind(t, err, wire.ErrValidation)
	}
}

func TestEncodeWriterWritesDeterministicXML(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	err := xmlwire.EncodeWriter(&output, shipment{ID: 42, Label: "carrier"}, xmlwire.EncodeOptions{})
	if err != nil {
		t.Fatalf("EncodeWriter() error = %v", err)
	}
	if got, want := output.String(), `<Shipment xmlns="urn:vendor"><ID xmlns="urn:vendor">42</ID><Label xmlns="urn:vendor">carrier</Label></Shipment>`; got != want {
		t.Fatalf("EncodeWriter() = %q, want %q", got, want)
	}
}

func TestEncodeWriterClassifiesWriterFailures(t *testing.T) {
	t.Parallel()

	assertKind(t, xmlwire.EncodeWriter(errorWriter{}, shipment{}, xmlwire.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, xmlwire.EncodeWriter(shortWriter{}, shipment{}, xmlwire.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, xmlwire.EncodeWriter(nil, shipment{}, xmlwire.EncodeOptions{}), wire.ErrValidation)
	assertKind(t, xmlwire.EncodeWriter(&bytes.Buffer{}, make(chan int), xmlwire.EncodeOptions{}), wire.ErrValidation)
}

func FuzzDecode(f *testing.F) {
	f.Add(readFixture(f, "shipment.xml"))
	f.Add(readFixture(f, "malformed.xml"))
	f.Add([]byte{})
	f.Add([]byte(" \t\r\n"))
	f.Add([]byte("\xef\xbb\xbf<root/>"))
	f.Add([]byte("<root>\x00</root>"))
	f.Add([]byte("<one/><two/>"))
	f.Add([]byte("<!DOCTYPE root [<!ENTITY x 'expanded'>]><root>&x;</root>"))
	f.Add([]byte(strings.Repeat("<a>", xmlwire.DefaultMaxDepth+1) + strings.Repeat("</a>", xmlwire.DefaultMaxDepth+1)))

	f.Fuzz(func(t *testing.T, payload []byte) {
		var target any
		_ = xmlwire.Decode(payload, &target, xmlwire.DecodeOptions{MaxBytes: 64 << 10})
	})
}

func BenchmarkDecode(b *testing.B) {
	payload := readFixture(b, "shipment.xml")
	b.ReportAllocs()
	for b.Loop() {
		var target shipment
		if err := xmlwire.Decode(payload, &target, xmlwire.DecodeOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	value := shipment{ID: 42, Label: "carrier"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := xmlwire.Encode(value, xmlwire.EncodeOptions{}); err != nil {
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
	if !errors.As(err, &wireErr) || wireErr.Format != wire.FormatXML {
		t.Fatalf("error = %#v, want XML *wire.Error", err)
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
