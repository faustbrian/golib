package cloudevents

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestEncodeHTTPRejectsUnsupportedCountsAndMapsOptionalBinaryFields(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		mode   ContentMode
		events []Event
		want   error
	}{
		{mode: BinaryMode, want: ErrUnsupportedMode},
		{mode: StructuredMode, want: ErrUnsupportedMode},
		{mode: BinaryMode, events: []Event{{}}, want: ErrInvalidEvent},
		{mode: BatchMode, events: []Event{{}}, want: ErrInvalidEvent},
		{mode: 255, want: ErrUnsupportedMode},
	} {
		if _, _, err := EncodeHTTP(test.events, test.mode); !errors.Is(err, test.want) {
			t.Fatalf("EncodeHTTP(%v) error = %v", test.mode, err)
		}
	}

	occurredAt := time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)
	data, err := NewJSONData([]byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	extension, err := NewStringAttribute("value")
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewEvent(Attributes{
		ID: "1", Source: "/source", Type: "example",
		DataSchema: "https://schemas.example/event.json", Subject: "subject", Time: &occurredAt,
		Extensions: map[string]Attribute{"custom": extension},
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	header, body, err := EncodeHTTP([]Event{event}, BinaryMode)
	if err != nil {
		t.Fatal(err)
	}
	if header.Get("Content-Type") != "application/json" || header.Get("Ce-Dataschema") == "" ||
		header.Get("Ce-Subject") != "subject" || header.Get("Ce-Time") == "" ||
		header.Get("Ce-Custom") != "value" || !bytes.Equal(body, []byte(`{"ok":true}`)) {
		t.Fatalf("binary mapping = headers %#v, body %s", header, body)
	}

	absent, err := NewEvent(Attributes{ID: "2", Source: "/source", Type: "example"}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	_, body, err = EncodeHTTP([]Event{absent}, BinaryMode)
	if err != nil || body != nil {
		t.Fatalf("absent binary body = %v, %v", body, err)
	}
}

func TestHTTPAttributeEncodingPreservesAndEscapesExactOctetBoundaries(t *testing.T) {
	t.Parallel()

	value := string([]byte{0x21, 0x7e, 0x20, '"', '%', 0x7f})
	if got := encodeHTTPAttribute(value); got != "!~%20%22%25%7F" {
		t.Fatalf("encodeHTTPAttribute() = %q", got)
	}
}

func TestDecodeHTTPRejectsModeBodyAndStructuredMetadataFailures(t *testing.T) {
	t.Parallel()

	//lint:ignore SA1012 A nil context is the public contract under test.
	//nolint:staticcheck // A nil context is the public contract under test.
	if _, err := DecodeHTTP(nil, nil, nil, DefaultLimits()); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("nil context error = %v", err)
	}
	for _, header := range []http.Header{
		{"Content-Type": {"a", "b"}},
		{"Content-Type": {";"}},
	} {
		if _, err := DecodeHTTP(context.Background(), header, nil, DefaultLimits()); !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("content type %#v error = %v", header, err)
		}
	}

	structuredHeader := http.Header{"Content-Type": {JSONMediaType}}
	structuredLimits := DefaultLimits()
	structuredLimits.MaxDataBytes = 0
	if _, err := DecodeHTTP(context.Background(), structuredHeader, strings.NewReader(`{"specversion":"1.0","id":"1","source":"/source","type":"example"}`), structuredLimits); err != nil {
		t.Fatalf("structured message incorrectly used binary data limit: %v", err)
	}
	if _, err := DecodeHTTP(context.Background(), structuredHeader, strings.NewReader("{"), DefaultLimits()); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("invalid structured body error = %v", err)
	}
	batchHeader := http.Header{"Content-Type": {JSONBatchMediaType}, "Ce-Id": {"1"}}
	if _, err := DecodeHTTP(context.Background(), batchHeader, strings.NewReader("[]"), DefaultLimits()); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("batch metadata error = %v", err)
	}
	if _, err := DecodeHTTP(context.Background(), http.Header{"Content-Type": {JSONBatchMediaType}}, strings.NewReader("{}"), DefaultLimits()); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("invalid batch error = %v", err)
	}

	value := []byte(`{"specversion":"1.0","id":"1","source":"/source","type":"example"}`)
	for _, test := range []struct {
		name   string
		header http.Header
		limits func() Limits
	}{
		{name: "duplicate aliases", header: http.Header{"Content-Type": {JSONMediaType}, "Ce-Id": {"1"}, "ce-id": {"1"}}},
		{name: "oversize", header: http.Header{"Content-Type": {JSONMediaType}, "Ce-Id": {"1111"}}, limits: func() Limits { l := DefaultLimits(); l.MaxAttributeValueBytes = 1; return l }},
		{name: "invalid encoding", header: http.Header{"Content-Type": {JSONMediaType}, "Ce-Id": {"%zz"}}},
		{name: "unknown", header: http.Header{"Content-Type": {JSONMediaType}, "Ce-Unknown": {"value"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			if test.limits != nil {
				limits = test.limits()
			}
			if _, err := DecodeHTTP(context.Background(), test.header, bytes.NewReader(value), limits); err == nil {
				t.Fatal("DecodeHTTP() error = nil")
			}
		})
	}
}

func TestStructuredHTTPMetadataCoversOptionalFieldsAndDirectLimits(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)
	extension, err := NewStringAttribute("value")
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewEvent(Attributes{
		ID: "1", Source: "/source", Type: "example", DataContentType: "application/json",
		DataSchema: "https://schemas.example/event.json", Subject: "subject", Time: &occurredAt,
		Extensions: map[string]Attribute{"custom": extension},
	}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	header := http.Header{
		"Ce-Datacontenttype": {"application/json"},
		"Ce-Dataschema":      {"https://schemas.example/event.json"},
		"Ce-Subject":         {"subject"},
		"Ce-Time":            {occurredAt.Format(time.RFC3339Nano)},
		"Ce-Custom":          {"value"},
	}
	if err := validateStructuredHTTPMetadata(header, event, DefaultLimits()); err != nil {
		t.Fatalf("matching optional metadata error = %v", err)
	}
	exactEvent, err := NewEvent(Attributes{ID: "A", Source: "/s", Type: "x"}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	exactLimits := DefaultLimits()
	exactLimits.MaxAttributeValueBytes = 1
	if err := validateStructuredHTTPMetadata(http.Header{"Ce-Id": {"%41"}}, exactEvent, exactLimits); err != nil {
		t.Fatalf("exact encoded metadata limit error = %v", err)
	}
	if exceedsHTTPEncodedAttributeLimit("value", int(^uint(0)>>1)) {
		t.Fatal("maximum attribute limit overflowed")
	}
	if !exceedsHTTPEncodedAttributeLimit("", 0) {
		t.Fatal("zero attribute limit accepted")
	}
	limits := DefaultLimits()
	limits.MaxAttributeValueBytes = 1
	if err := validateStructuredHTTPMetadata(http.Header{"Ce-Id": {"1111"}}, event, limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("metadata limit error = %v", err)
	}
	limits = DefaultLimits()
	limits.MaxAttributeNameBytes = 0
	if err := validateStructuredHTTPMetadata(http.Header{"Ce-Id": {"1"}}, event, limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("metadata name limit error = %v", err)
	}
	if err := validateStructuredHTTPMetadata(http.Header{"Ce-": {"value"}}, event, limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("metadata zero-only name limit error = %v", err)
	}
	limits = DefaultLimits()
	limits.MaxAttributeNameBytes = 1
	if err := validateStructuredHTTPMetadata(http.Header{"Ce-Id": {"1"}}, event, limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("metadata oversized-only name limit error = %v", err)
	}
	limits.MaxAttributeNameBytes = len("id")
	if err := validateStructuredHTTPMetadata(http.Header{"Ce-Id": {"1"}}, event, limits); err != nil {
		t.Fatalf("metadata exact name limit error = %v", err)
	}
}

type cancelAfterRead struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (reader cancelAfterRead) Read(value []byte) (int, error) {
	count, err := reader.reader.Read(value)
	reader.cancel()
	return count, err
}

func TestHTTPBodyLimitsCancellationAndNilOwnership(t *testing.T) {
	t.Parallel()

	if value, err := readHTTPBody(context.Background(), nil, 1); err != nil || value != nil {
		t.Fatalf("nil body = %v, %v", value, err)
	}
	if _, err := readHTTPBody(context.Background(), nil, 0); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero limit with nil body error = %v", err)
	}
	for _, limit := range []int64{0, int64(^uint64(0) >> 1)} {
		if _, err := readHTTPBody(context.Background(), strings.NewReader("x"), limit); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("limit %d error = %v", limit, err)
		}
	}
	if _, err := readHTTPBody(context.Background(), strings.NewReader("xx"), 1); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("oversize body error = %v", err)
	}
	if value, err := readHTTPBody(context.Background(), strings.NewReader("x"), 1); err != nil || string(value) != "x" {
		t.Fatalf("exact-limit body = %q, %v", value, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := readHTTPBody(ctx, cancelAfterRead{reader: strings.NewReader("x"), cancel: cancel}, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel-after-read error = %v", err)
	}
}

func TestDecodeHTTPBinaryRejectsMalformedAttributesAndData(t *testing.T) {
	t.Parallel()

	base := http.Header{
		"Ce-Specversion": {"1.0"}, "Ce-Id": {"1"}, "Ce-Source": {"/source"}, "Ce-Type": {"example"},
	}
	clone := func() http.Header { return base.Clone() }
	tests := []struct {
		name   string
		header http.Header
		body   string
		limits func() Limits
	}{
		{name: "wrong spec", header: http.Header{"Ce-Specversion": {"0.3"}}},
		{name: "invalid time", header: func() http.Header { h := clone(); h.Set("Ce-Time", "not-time"); return h }()},
		{name: "invalid extension Unicode", header: func() http.Header { h := clone(); h["Ce-X"] = []string{string([]byte{0xff})}; return h }()},
		{name: "invalid extension control", header: func() http.Header { h := clone(); h.Set("Ce-X", "%0A"); return h }()},
		{name: "invalid JSON body", header: func() http.Header { h := clone(); h.Set("Content-Type", "application/json"); return h }(), body: "{"},
		{name: "duplicate", header: func() http.Header { h := clone(); h["Ce-Id"] = []string{"1", "1"}; return h }()},
		{name: "encoded over limit", header: func() http.Header { h := clone(); h.Set("Ce-Id", "%41%41"); return h }(), limits: func() Limits { l := DefaultLimits(); l.MaxAttributeValueBytes = 1; return l }},
		{name: "too many", header: func() http.Header { h := clone(); h.Set("Ce-X", "x"); return h }(), limits: func() Limits { l := DefaultLimits(); l.MaxAttributes = 4; return l }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			if test.limits != nil {
				limits = test.limits()
			}
			if _, err := DecodeHTTP(context.Background(), test.header, strings.NewReader(test.body), limits); err == nil {
				t.Fatal("DecodeHTTP() error = nil")
			}
		})
	}

	message, err := DecodeHTTP(context.Background(), base, strings.NewReader(""), DefaultLimits())
	if err != nil || len(message.Events) != 1 || message.Events[0].Data().Present() {
		t.Fatalf("absent data message = %#v, %v", message, err)
	}
	emptyBinaryHeader := base.Clone()
	emptyBinaryHeader.Set("Content-Type", "application/octet-stream")
	message, err = DecodeHTTP(context.Background(), emptyBinaryHeader, strings.NewReader(""), DefaultLimits())
	if err != nil || !message.Events[0].Data().Present() || message.Events[0].Data().Kind() != DataBinary || len(message.Events[0].Data().Bytes()) != 0 {
		t.Fatalf("present empty binary data message = %#v, %v", message, err)
	}
	limits := DefaultLimits()
	limits.MaxAttributeValueBytes = 1
	if _, err := decodeHTTPBinary(http.Header{"Ce-X": {"AAAA"}}, "", nil, limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("encoded attribute pre-limit error = %v", err)
	}
	if _, err := decodeHTTPBinary(http.Header{"Ce-X": {"AA"}}, "", nil, limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("decoded attribute limit error = %v", err)
	}
	zeroNameLimits := DefaultLimits()
	zeroNameLimits.MaxAttributeNameBytes = 0
	if _, err := decodeHTTPBinary(http.Header{"Ce-": {"value"}}, "", nil, zeroNameLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero-only attribute name limit error = %v", err)
	}
	exact := http.Header{
		"Ce-Specversion": {"1.0"}, "Ce-Id": {"AAAA"}, "Ce-Source": {"/src"}, "Ce-Type": {"type"},
	}
	exactLimits := DefaultLimits()
	exactLimits.MaxAttributeValueBytes = 4
	exactLimits.MaxAttributeNameBytes = len("specversion")
	exactLimits.MaxAttributes = 4
	if _, err := decodeHTTPBinary(exact, "", nil, exactLimits); err != nil {
		t.Fatalf("exact attribute limits error = %v", err)
	}
	binaryHeader := base.Clone()
	binaryHeader.Set("Content-Type", "application/octet-stream")
	message, err = DecodeHTTP(context.Background(), binaryHeader, strings.NewReader("binary"), DefaultLimits())
	if err != nil || message.Events[0].Data().Kind() != DataBinary {
		t.Fatalf("binary body message = %#v, %v", message, err)
	}
}

func TestHTTPAttributeQuotedStringGrammar(t *testing.T) {
	t.Parallel()

	if got, err := decodeHTTPAttribute(`"escaped\value"`); err != nil || got != "escapedvalue" {
		t.Fatalf("escaped attribute = %q, %v", got, err)
	}
	for _, test := range []struct {
		value string
		want  string
	}{
		{value: `""`, want: ""},
		{value: "\" \"", want: " "},
	} {
		if got, err := decodeHTTPAttribute(test.value); err != nil || got != test.want {
			t.Fatalf("attribute %q = %q, %v", test.value, got, err)
		}
	}
	for _, value := range []string{`"unterminated`, `"trailing\"`, `"embedded"quote"`, "\"control\x1f\"", "%FF"} {
		if _, err := decodeHTTPAttribute(value); !errors.Is(err, ErrInvalidAttribute) {
			t.Fatalf("attribute %q error = %v", value, err)
		}
	}
}

func TestDecodeHTTPBoundsContextAttributeNamesInEveryMode(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxAttributeNameBytes = len("specversion")
	longName := "extensionname"
	exactStructured := http.Header{
		"Content-Type":   {JSONMediaType},
		"Ce-" + longName: {"value"},
	}
	exactBody := `{"specversion":"1.0","id":"1","source":"/","type":"x","extensionname":"value"}`
	exactLimits := DefaultLimits()
	exactLimits.MaxAttributeNameBytes = len(longName)
	if _, err := DecodeHTTP(context.Background(), exactStructured, strings.NewReader(exactBody), exactLimits); err != nil {
		t.Fatalf("exact structured attribute name error = %v", err)
	}

	binary := http.Header{
		"Ce-Specversion": {"1.0"},
		"Ce-Id":          {"1"},
		"Ce-Source":      {"/"},
		"Ce-Type":        {"x"},
		"Ce-" + longName: {"value"},
	}
	if _, err := DecodeHTTP(context.Background(), binary, nil, limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("binary oversized attribute name error = %v", err)
	}

	structured := http.Header{
		"Content-Type":   {JSONMediaType},
		"Ce-" + longName: {"value"},
	}
	if _, err := DecodeHTTP(context.Background(), structured, strings.NewReader(exactBody), limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("structured oversized attribute name error = %v", err)
	}
	zeroLimits := DefaultLimits()
	zeroLimits.MaxAttributeNameBytes = 0
	if _, err := DecodeHTTP(context.Background(), structured, strings.NewReader(exactBody), zeroLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("structured zero attribute name limit error = %v", err)
	}
}
