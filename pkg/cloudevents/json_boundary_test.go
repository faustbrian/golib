package cloudevents

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"
)

func TestDecodeJSONEnforcesDecodedAndExactResourceBoundaries(t *testing.T) {
	t.Parallel()

	base := `{"specversion":"1.0","id":"1","source":"/source","type":"example"}`
	zeroEventBytes := DefaultLimits()
	zeroEventBytes.MaxEventBytes = 0
	if _, err := DecodeJSON(nil, zeroEventBytes); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero event limit error = %v", err)
	}
	limits := DefaultLimits()
	limits.MaxEventBytes = int64(len(base))
	if _, err := DecodeJSON([]byte(base), limits); err != nil {
		t.Fatalf("exact event limit error = %v", err)
	}

	zeroAttributes := DefaultLimits()
	zeroAttributes.MaxAttributes = 0
	if _, err := DecodeJSON([]byte(`{}`), zeroAttributes); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero attribute limit error = %v", err)
	}
	fiveAttributes := `{"specversion":"1.0","id":"1","source":"/source","type":"example","extra":"x"}`
	fourAttributes := DefaultLimits()
	fourAttributes.MaxAttributes = 4
	if _, err := DecodeJSON([]byte(base), fourAttributes); err != nil {
		t.Fatalf("exact context attribute count error = %v", err)
	}
	if _, err := DecodeJSON([]byte(fiveAttributes), fourAttributes); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("context attribute count error = %v", err)
	}
	exactName := `{"specversion":"1.0","id":"1","source":"/","type":"x","x":null}`
	exactNameLimits := DefaultLimits()
	exactNameLimits.MaxAttributeNameBytes = 1
	if _, err := DecodeJSON([]byte(exactName), exactNameLimits); err != nil {
		t.Fatalf("exact extension name limit error = %v", err)
	}
	emptyName := `{"specversion":"1.0","id":"1","source":"/","type":"x","":null}`
	exactNameLimits.MaxAttributeNameBytes = 0
	if _, err := DecodeJSON([]byte(emptyName), exactNameLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero extension name limit error = %v", err)
	}

	escapedID := `{"specversion":"1.0","id":"\u0031\u0032\u0033","source":"/","type":"x"}`
	threeByteAttributes := DefaultLimits()
	threeByteAttributes.MaxAttributeValueBytes = 3
	if _, err := DecodeJSON([]byte(escapedID), threeByteAttributes); err != nil {
		t.Fatalf("decoded exact-size attribute error = %v", err)
	}
	fourByteID := `{"specversion":"1.0","id":"1234","source":"/","type":"x"}`
	if _, err := DecodeJSON([]byte(fourByteID), threeByteAttributes); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("decoded attribute overflow error = %v", err)
	}

	for _, test := range []struct {
		name  string
		input string
		limit int64
		want  error
	}{
		{name: "base64 exact", input: `{"specversion":"1.0","id":"1","source":"/","type":"x","data_base64":"YQ=="}`, limit: 1},
		{name: "base64 zero", input: `{"specversion":"1.0","id":"1","source":"/","type":"x","data_base64":""}`, limit: 0, want: ErrLimitExceeded},
		{name: "json exact", input: `{"specversion":"1.0","id":"1","source":"/","type":"x","datacontenttype":"application/json","data":1}`, limit: 1},
		{name: "text escaped exact", input: `{"specversion":"1.0","id":"1","source":"/","type":"x","datacontenttype":"text/plain","data":"\u0061"}`, limit: 1},
		{name: "text decoded overflow", input: `{"specversion":"1.0","id":"1","source":"/","type":"x","datacontenttype":"text/plain","data":"aa"}`, limit: 1, want: ErrLimitExceeded},
		{name: "text encoded overflow", input: `{"specversion":"1.0","id":"1","source":"/","type":"x","datacontenttype":"text/plain","data":"1234567"}`, limit: 1, want: ErrLimitExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			dataLimits := DefaultLimits()
			dataLimits.MaxDataBytes = test.limit
			_, err := DecodeJSON([]byte(test.input), dataLimits)
			if !errors.Is(err, test.want) {
				t.Fatalf("DecodeJSON() error = %v, want %v", err, test.want)
			}
		})
	}

	if got := base64DecodedLength("YQ=="); got != 1 {
		t.Fatalf("base64DecodedLength(YQ==) = %d", got)
	}
	if got := base64DecodedLength("YWI="); got != 2 {
		t.Fatalf("base64DecodedLength(YWI=) = %d", got)
	}
	if !exceedsJSONEncodedStringLimit(nil, 0) {
		t.Fatal("zero encoded-string limit accepted")
	}
	if exceedsJSONEncodedStringLimit([]byte(`"123456"`), 1) {
		t.Fatal("exact encoded-string expansion rejected")
	}
	if !exceedsJSONEncodedStringLimit([]byte(`"1234567"`), 1) {
		t.Fatal("encoded-string expansion overflow accepted")
	}
	dataLimits := DefaultLimits()
	dataLimits.MaxDataBytes = 0
	if _, err := decodeJSONData(map[string]json.RawMessage{"data": {}}, "application/json", dataLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero JSON data limit error = %v", err)
	}
}

func TestDecodeJSONBatchAndDepthAcceptExactLimits(t *testing.T) {
	t.Parallel()

	event := `{"specversion":"1.0","id":"1","source":"/","type":"x"}`
	batch := fmt.Sprintf("[%s]", event)
	zeroEventBytes := DefaultLimits()
	zeroEventBytes.MaxEventBytes = 0
	if _, err := DecodeJSONBatch(nil, zeroEventBytes); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero batch byte limit error = %v", err)
	}
	limits := DefaultLimits()
	limits.MaxEventBytes = int64(len(batch))
	limits.MaxBatchEvents = 1
	limits.MaxDepth = 1
	if _, err := DecodeJSONBatch([]byte(batch), limits); err != nil {
		t.Fatalf("exact batch limits error = %v", err)
	}
	limits.MaxBatchEvents = 0
	if _, err := DecodeJSONBatch([]byte(`[]`), limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero batch limit error = %v", err)
	}
	zeroDepth := DefaultLimits()
	zeroDepth.MaxDepth = 0
	if _, err := DecodeJSONBatch([]byte(`[]`), zeroDepth); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero empty-batch depth error = %v", err)
	}
	if err := inspectJSON([]byte(`{}`), 1); err != nil {
		t.Fatalf("exact depth error = %v", err)
	}
	if err := inspectJSON([]byte(`{"nested":{}}`), 1); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("depth overflow error = %v", err)
	}
}

func TestEncodeJSONCoversOptionalAttributesAndEveryDataRepresentation(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)
	uri, err := NewURIAttribute("https://example.com/extension")
	if err != nil {
		t.Fatal(err)
	}
	reference, err := NewURIReferenceAttribute("/reference")
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewEvent(Attributes{
		ID: "1", Source: "/source", Type: "example",
		DataSchema: "https://schemas.example/event.json", Subject: "subject", Time: &occurredAt,
		Extensions: map[string]Attribute{
			"uri": uri, "reference": reference, "timestamp": NewTimestampAttribute(occurredAt),
		},
	}, NewBinaryData([]byte("binary")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeJSON(event); err != nil {
		t.Fatalf("binary EncodeJSON() error = %v", err)
	}
	text, err := NewTextData("text")
	if err != nil {
		t.Fatal(err)
	}
	event, err = NewEvent(Attributes{ID: "2", Source: "/source", Type: "example"}, text)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeJSON(event); err != nil {
		t.Fatalf("text EncodeJSON() error = %v", err)
	}
	if _, err := EncodeJSONBatch([]Event{{}}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("invalid batch event error = %v", err)
	}
}

func TestDecodeJSONRejectsEveryContextAndExtensionBoundary(t *testing.T) {
	t.Parallel()

	base := `{"specversion":"1.0","id":"1","source":"/source","type":"example"}`
	tests := []struct {
		name   string
		input  string
		limits func() Limits
		want   error
	}{
		{name: "zero event limit", input: base, limits: func() Limits { l := DefaultLimits(); l.MaxEventBytes = 0; return l }, want: ErrLimitExceeded},
		{name: "scalar top level", input: `"event"`, want: ErrInvalidEvent},
		{name: "zero attribute count", input: base, limits: func() Limits { l := DefaultLimits(); l.MaxAttributes = 0; return l }, want: ErrLimitExceeded},
		{name: "missing specversion", input: `{"id":"1","source":"/source","type":"example"}`, want: ErrInvalidEvent},
		{name: "null id", input: `{"specversion":"1.0","id":null,"source":"/source","type":"example"}`, want: ErrInvalidEvent},
		{name: "unsupported specversion", input: `{"specversion":"0.3","id":"1","source":"/source","type":"example"}`, want: ErrInvalidEvent},
		{name: "numeric id", input: `{"specversion":"1.0","id":1,"source":"/source","type":"example"}`, want: ErrInvalidEvent},
		{name: "numeric source", input: `{"specversion":"1.0","id":"1","source":1,"type":"example"}`, want: ErrInvalidEvent},
		{name: "numeric type", input: `{"specversion":"1.0","id":"1","source":"/source","type":1}`, want: ErrInvalidEvent},
		{name: "numeric content type", input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","datacontenttype":1}`, want: ErrInvalidEvent},
		{name: "numeric schema", input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","dataschema":1}`, want: ErrInvalidEvent},
		{name: "numeric subject", input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","subject":1}`, want: ErrInvalidEvent},
		{name: "numeric time", input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","time":1}`, want: ErrInvalidEvent},
		{name: "invalid time", input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","time":"today"}`, want: ErrInvalidEvent},
		{name: "attribute value limit", input: base, limits: func() Limits { l := DefaultLimits(); l.MaxAttributeValueBytes = 0; return l }, want: ErrLimitExceeded},
		{name: "extension name limit", input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","extension":"x"}`, limits: func() Limits { l := DefaultLimits(); l.MaxAttributeNameBytes = 1; return l }, want: ErrLimitExceeded},
		{name: "fraction extension", input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","attempt":1.5}`, want: ErrInvalidEvent},
		{name: "integer overflow extension", input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","attempt":2147483648}`, want: ErrInvalidEvent},
		{name: "invalid string extension", input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","extension":"\u0001"}`, want: ErrInvalidEvent},
		{name: "null extension", input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","extension":null}`},
		{name: "valid text data", input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","datacontenttype":"text/plain","data":"text"}`},
		{name: "array extension", input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","extension":[]}`, want: ErrInvalidEvent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			if test.limits != nil {
				limits = test.limits()
			}
			if _, err := DecodeJSON([]byte(test.input), limits); !errors.Is(err, test.want) {
				t.Fatalf("DecodeJSON() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDecodeJSONRejectsEveryDataAndBatchBoundary(t *testing.T) {
	t.Parallel()

	prefix := `{"specversion":"1.0","id":"1","source":"/source","type":"example",`
	tests := []struct {
		name   string
		input  string
		limits func() Limits
		want   error
	}{
		{name: "base64 nonstring", input: prefix + `"data_base64":1}`, want: ErrInvalidEvent},
		{name: "base64 invalid", input: prefix + `"data_base64":"!!!!"}`, want: ErrInvalidEvent},
		{name: "base64 zero limit", input: prefix + `"data_base64":"Zg=="}`, limits: func() Limits { l := DefaultLimits(); l.MaxDataBytes = 0; return l }, want: ErrLimitExceeded},
		{name: "raw zero limit", input: prefix + `"data":null}`, limits: func() Limits { l := DefaultLimits(); l.MaxDataBytes = 0; return l }, want: ErrLimitExceeded},
		{name: "nontext data", input: prefix + `"datacontenttype":"text/plain","data":1}`, want: ErrInvalidEvent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			if test.limits != nil {
				limits = test.limits()
			}
			if _, err := DecodeJSON([]byte(test.input), limits); !errors.Is(err, test.want) {
				t.Fatalf("DecodeJSON() error = %v, want %v", err, test.want)
			}
		})
	}

	valid := `[{"specversion":"1.0","id":"1","source":"/source","type":"example"}]`
	batchTests := []struct {
		name   string
		input  string
		limits func() Limits
		want   error
	}{
		{name: "event limit", input: valid, limits: func() Limits { l := DefaultLimits(); l.MaxEventBytes = 0; return l }, want: ErrLimitExceeded},
		{name: "depth zero", input: valid, limits: func() Limits { l := DefaultLimits(); l.MaxDepth = 0; return l }, want: ErrLimitExceeded},
		{name: "depth overflow guard", input: valid, limits: func() Limits { l := DefaultLimits(); l.MaxDepth = math.MaxInt; return l }, want: ErrLimitExceeded},
		{name: "not array", input: `{}`, want: ErrInvalidEvent},
		{name: "malformed array", input: `[`, want: ErrInvalidEvent},
		{name: "zero batch limit", input: `[]`, limits: func() Limits { l := DefaultLimits(); l.MaxBatchEvents = 0; return l }, want: ErrLimitExceeded},
		{name: "invalid member", input: `[{}]`, want: ErrInvalidEvent},
	}
	for _, test := range batchTests {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			if test.limits != nil {
				limits = test.limits()
			}
			if _, err := DecodeJSONBatch([]byte(test.input), limits); !errors.Is(err, test.want) {
				t.Fatalf("DecodeJSONBatch() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestJSONInspectionRejectsMalformedAndTrailingValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", `{} {}`, `{"`, `{"a":`, `{"a":1`, `}`, `[{]`} {
		if err := inspectJSON([]byte(value), DefaultLimits().MaxDepth); !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("inspectJSON(%q) error = %v", value, err)
		}
	}
	if err := inspectJSON([]byte(`null`), 0); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero depth error = %v", err)
	}
	if isJSONMediaType("invalid;") {
		t.Fatal("invalid media type reported as JSON")
	}
	if isJSONMediaType(";") {
		t.Fatal("malformed media type reported as JSON")
	}
	if _, err := decodeJSONAttribute([]byte("{")); err == nil {
		t.Fatal("malformed extension error = nil")
	}
}
