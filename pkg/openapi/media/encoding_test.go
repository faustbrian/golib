package media_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/media"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestPositionalEncodingsAppliesPrefixThenItemEncoding(t *testing.T) {
	t.Parallel()

	mediaType := mustMediaValue(t, `{
		"prefixEncoding":[{"contentType":"first"},{"contentType":"second"}],
		"itemEncoding":{"contentType":"remaining"}
	}`)
	applied, err := media.PositionalEncodings(mediaType, 4, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 4 {
		t.Fatalf("applied encodings = %d", len(applied))
	}
	want := []string{"first", "second", "remaining", "remaining"}
	for index, encoding := range applied {
		if encoding.Index != index {
			t.Fatalf("encoding %d index = %d", index, encoding.Index)
		}
		contentType, exists := encoding.Value.Lookup("contentType")
		value, valid := contentType.Text()
		if !exists || !valid || value != want[index] {
			t.Fatalf("encoding %d = %#v", index, encoding)
		}
	}
}

func TestPositionalEncodingsIgnoresUnusedPrefixEntries(t *testing.T) {
	t.Parallel()

	applied, err := media.PositionalEncodings(mustMediaValue(t, `{
		"prefixEncoding":[{},1,2],"itemEncoding":{}
	}`), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0].Index != 0 {
		t.Fatalf("applied = %#v", applied)
	}
}

func TestPositionalEncodingsValidatesValuesAndBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		mediaType string
		itemCount int
		maxItems  int
		limit     bool
	}{
		{name: "non-object", mediaType: `1`, maxItems: 1},
		{name: "negative items", mediaType: `{}`, itemCount: -1, maxItems: 1},
		{name: "invalid maximum", mediaType: `{}`, maxItems: 0},
		{name: "limit", mediaType: `{}`, itemCount: 2, maxItems: 1, limit: true},
		{name: "prefix type", mediaType: `{"prefixEncoding":{}}`, maxItems: 1},
		{name: "prefix entry", mediaType: `{"prefixEncoding":[1]}`,
			itemCount: 1, maxItems: 1},
		{name: "item type", mediaType: `{"itemEncoding":1}`,
			itemCount: 1, maxItems: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := media.PositionalEncodings(
				mustMediaValue(t, test.mediaType), test.itemCount, test.maxItems,
			)
			want := media.ErrInvalidPositionalEncoding
			if test.limit {
				want = media.ErrPositionalEncodingLimit
			}
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
}

func TestPositionalEncodingsHandlesAbsentAndSingleForms(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		mediaType string
		want      int
	}{
		{mediaType: `{}`, want: 0},
		{mediaType: `{"prefixEncoding":[{}]}`, want: 1},
		{mediaType: `{"itemEncoding":{}}`, want: 3},
	} {
		applied, err := media.PositionalEncodings(
			mustMediaValue(t, test.mediaType), 3, 3,
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(applied) != test.want {
			t.Fatalf("PositionalEncodings(%s) = %d, want %d",
				test.mediaType, len(applied), test.want)
		}
	}
}

func TestPositionalEncodingsAcceptsZeroItems(t *testing.T) {
	t.Parallel()

	applied, err := media.PositionalEncodings(mustMediaValue(t, `{}`), 0, 1)
	if err != nil || len(applied) != 0 {
		t.Fatalf("PositionalEncodings() = %#v, %v", applied, err)
	}
}

func TestNamedEncodingValuesMapsObjectProperties(t *testing.T) {
	t.Parallel()

	properties := mustMediaValue(t, `{
		"tags":{"type":"array"},"profile":{"type":"object"}
	}`)
	encodings := mustMediaValue(t, `{
		"tags":{"contentType":"text/plain"},"missing":{},"profile":{}
	}`)
	instance := mustMediaValue(t, `{
		"tags":["one","two"],"profile":{"id":1},"missing":"ignored"
	}`)
	applied, err := media.NamedEncodingValues(properties, encodings, instance, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 3 {
		t.Fatalf("applied = %#v", applied)
	}
	wantNames := []string{"tags", "tags", "profile"}
	wantValues := []string{`"one"`, `"two"`, `{"id":1}`}
	for index, value := range applied {
		encoded, marshalErr := value.Value.MarshalJSON()
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if value.Name != wantNames[index] || value.ItemIndex != -1 ||
			string(encoded) != wantValues[index] {
			t.Fatalf("value %d = %#v (%s)", index, value, encoded)
		}
	}
}

func TestNamedEncodingValuesKeepsNestedArrayValuesWhole(t *testing.T) {
	t.Parallel()

	applied, err := media.NamedEncodingValues(
		mustMediaValue(t, `{"tags":{"type":"array"}}`),
		mustMediaValue(t, `{"tags":{}}`),
		mustMediaValue(t, `[{"tags":["one","two"]},{"tags":["three"]}]`),
		10,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied = %#v", applied)
	}
	want := []string{`["one","two"]`, `["three"]`}
	for index, value := range applied {
		encoded, marshalErr := value.Value.MarshalJSON()
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if value.Name != "tags" || string(encoded) != want[index] {
			t.Fatalf("value %d = %#v (%s)", index, value, encoded)
		}
	}
}

func TestSerializeNamedEncodingValuesMapsKeysToFormParameters(t *testing.T) {
	t.Parallel()

	want := "tags=one&tags=two&note=a+b"
	encoded, err := media.SerializeNamedEncodingValues(
		mustMediaValue(t, `{
			"tags":{"type":"array"},"note":{"type":"string"}
		}`),
		mustMediaValue(t, `{"tags":{},"note":{"explode":false}}`),
		mustMediaValue(t, `{"tags":["one","two"],"note":"a b"}`),
		10,
		len(want),
	)
	if err != nil {
		t.Fatal(err)
	}
	if encoded != want {
		t.Fatalf("encoded = %q", encoded)
	}
	singleWant := "note=a+b"
	single, err := media.SerializeNamedEncodingValues(
		mustMediaValue(t, `{"note":{"type":"string"}}`),
		mustMediaValue(t, `{"note":{}}`),
		mustMediaValue(t, `{"note":"a b"}`),
		1,
		len(singleWant),
	)
	if err != nil || single != singleWant {
		t.Fatalf("single encoding = %q, %v", single, err)
	}
	empty, err := media.SerializeNamedEncodingValues(
		mustMediaValue(t, `{}`),
		mustMediaValue(t, `{}`),
		mustMediaValue(t, `{}`),
		1,
		1,
	)
	if err != nil || empty != "" {
		t.Fatalf("empty encoding = %q, %v", empty, err)
	}
}

func TestSerializeNamedEncodingValuesEnforcesBounds(t *testing.T) {
	t.Parallel()

	properties := mustMediaValue(t, `{"value":{"type":"string"}}`)
	encodings := mustMediaValue(t, `{"value":{}}`)
	instance := mustMediaValue(t, `{"value":"long"}`)
	for _, maximum := range []int{0, len("value=long") - 1} {
		_, err := media.SerializeNamedEncodingValues(
			properties,
			encodings,
			instance,
			1,
			maximum,
		)
		if !errors.Is(err, media.ErrEncodingSerializationLimit) &&
			!errors.Is(err, media.ErrInvalidEncodingSerialization) {
			t.Fatalf("maximum %d error = %v", maximum, err)
		}
	}
	_, err := media.SerializeNamedEncodingValues(
		mustMediaValue(t, `{"tags":{"type":"array"}}`),
		mustMediaValue(t, `{"tags":{}}`),
		mustMediaValue(t, `{"tags":["a","b"]}`),
		2,
		len("tags=a&tags=b")-1,
	)
	if !errors.Is(err, media.ErrEncodingSerializationLimit) {
		t.Fatalf("combined output error = %v", err)
	}
	_, err = media.SerializeNamedEncodingValues(
		mustMediaValue(t, `[]`),
		encodings,
		instance,
		1,
		100,
	)
	if !errors.Is(err, media.ErrInvalidNamedEncoding) {
		t.Fatalf("invalid mapping error = %v", err)
	}
}

func TestNamedEncodingValuesValidatesInputsAndBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		properties string
		encodings  string
		instance   string
		maxValues  int
		limit      bool
	}{
		{name: "properties", properties: `[]`, encodings: `{}`,
			instance: `{}`, maxValues: 1},
		{name: "encodings", properties: `{}`, encodings: `[]`,
			instance: `{}`, maxValues: 1},
		{name: "instance", properties: `{}`, encodings: `{}`,
			instance: `1`, maxValues: 1},
		{name: "maximum", properties: `{}`, encodings: `{}`,
			instance: `{}`, maxValues: 0},
		{name: "object encoding", properties: `{"x":{}}`,
			encodings: `{"x":1}`, instance: `{"x":1}`, maxValues: 1},
		{name: "array property value", properties: `{"x":{"type":"array"}}`,
			encodings: `{"x":{}}`, instance: `{"x":1}`, maxValues: 1},
		{name: "object limit", properties: `{"x":{},"y":{}}`,
			encodings: `{"x":{},"y":{}}`, instance: `{"x":1,"y":2}`,
			maxValues: 1, limit: true},
		{name: "array value limit", properties: `{"x":{"type":"array"}}`,
			encodings: `{"x":{}}`, instance: `{"x":[1,2]}`,
			maxValues: 1, limit: true},
		{name: "array item", properties: `{"x":{}}`, encodings: `{"x":{}}`,
			instance: `[1]`, maxValues: 1},
		{name: "array encoding", properties: `{"x":{}}`,
			encodings: `{"x":1}`, instance: `[{"x":1}]`, maxValues: 1},
		{name: "array limit", properties: `{"x":{}}`, encodings: `{"x":{}}`,
			instance: `[{"x":1},{"x":2}]`, maxValues: 1, limit: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := media.NamedEncodingValues(
				mustMediaValue(t, test.properties), mustMediaValue(t, test.encodings),
				mustMediaValue(t, test.instance), test.maxValues,
			)
			want := media.ErrInvalidNamedEncoding
			if test.limit {
				want = media.ErrNamedEncodingLimit
			}
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
}

func TestNamedEncodingValuesHandlesMissingAndUnionProperties(t *testing.T) {
	t.Parallel()

	applied, err := media.NamedEncodingValues(
		mustMediaValue(t, `{
			"union":{"type":["string","array"]},
			"unionFalse":{"type":["string","null"]},
			"untyped":{},"invalidType":{"type":1}
		}`),
		mustMediaValue(t, `{
			"missing":1,"union":{},"unionFalse":{},
			"untyped":{},"invalidType":{}
		}`),
		mustMediaValue(t, `{
			"union":[1,2],"unionFalse":5,"untyped":3,"invalidType":4
		}`),
		10,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 5 {
		t.Fatalf("applied = %#v", applied)
	}

	for _, instance := range []string{`{}`, `[{},{}]`} {
		empty, emptyErr := media.NamedEncodingValues(
			mustMediaValue(t, `{"x":{}}`), mustMediaValue(t, `{"x":{}}`),
			mustMediaValue(t, instance), 1,
		)
		if emptyErr != nil {
			t.Fatal(emptyErr)
		}
		if len(empty) != 0 {
			t.Fatalf("missing values produced encodings: %#v", empty)
		}
	}
	ignored, ignoredErr := media.NamedEncodingValues(
		mustMediaValue(t, `{}`), mustMediaValue(t, `{"x":1}`),
		mustMediaValue(t, `[{"x":1}]`), 1,
	)
	if ignoredErr != nil || len(ignored) != 0 {
		t.Fatalf("unknown array property = %#v, %v", ignored, ignoredErr)
	}
}

func TestMultipartFormDataDispositionMapsTheEncodingName(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		want string
	}{
		{name: "user", want: `form-data; name="user"`},
		{name: `field "quoted"`, want: `form-data; name="field \"quoted\""`},
		{name: "path\\percent%\t", want: `form-data; name="path\\percent%25%09"`},
		{name: "smile 😀", want: `form-data; name="smile %F0%9F%98%80"`},
	} {
		got, err := media.MultipartFormDataDisposition(test.name, 256)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("disposition for %q = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestMultipartFormDataDispositionValidatesNamesAndBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		maxBytes int
		want     error
	}{
		{name: "field", maxBytes: 0, want: media.ErrInvalidMultipartName},
		{name: string([]byte{0xff}), maxBytes: 100,
			want: media.ErrInvalidMultipartName},
		{name: "field\rname", maxBytes: 100,
			want: media.ErrInvalidMultipartName},
		{name: "field", maxBytes: 18,
			want: media.ErrMultipartDispositionLimit},
		{name: "", maxBytes: 1,
			want: media.ErrMultipartDispositionLimit},
	} {
		_, err := media.MultipartFormDataDisposition(test.name, test.maxBytes)
		if !errors.Is(err, test.want) {
			t.Fatalf("name %q error = %v, want %v", test.name, err, test.want)
		}
	}
}

func TestMultipartFormDataDispositionAcceptsExactOutputBound(t *testing.T) {
	t.Parallel()

	const name = "A %\x1f\x7f\"\\😀"
	want, err := media.MultipartFormDataDisposition(name, 256)
	if err != nil {
		t.Fatal(err)
	}
	got, err := media.MultipartFormDataDisposition(name, len(want))
	if err != nil || got != want {
		t.Fatalf("exact disposition = %q, %v; want %q", got, err, want)
	}
	if _, err = media.MultipartFormDataDisposition(name, len(want)-1); !errors.Is(err, media.ErrMultipartDispositionLimit) {
		t.Fatalf("one-byte-short error = %v", err)
	}
}

func TestSelectEncodingContentTypeRequiresAnExplicitMultipleChoice(t *testing.T) {
	t.Parallel()

	encoding := mustMediaValue(t, `{"contentType":"image/png, image/*"}`)
	if _, err := media.SelectEncodingContentType(
		encoding, mustMediaValue(t, `{"type":"string"}`), "",
	); !errors.Is(err, media.ErrEncodingContentTypeSelection) {
		t.Fatalf("missing selection error = %v", err)
	}
	selected, err := media.SelectEncodingContentType(
		encoding, mustMediaValue(t, `{"type":"string"}`), "image/jpeg",
	)
	if err != nil {
		t.Fatal(err)
	}
	if selected != "image/jpeg" {
		t.Fatalf("selected content type = %q", selected)
	}
	if _, err = media.SelectEncodingContentType(
		encoding, mustMediaValue(t, `{"type":"string"}`), "text/plain",
	); !errors.Is(err, media.ErrEncodingContentTypeSelection) {
		t.Fatalf("unlisted selection error = %v", err)
	}
}

func TestApplyEncodingCombinesCommonAndSerializationFields(t *testing.T) {
	t.Parallel()

	applied, err := media.ApplyEncoding(
		"part",
		mustMediaValue(t, `"a b"`),
		mustMediaValue(t, `{"type":"string"}`),
		mustMediaValue(t, `{
			"contentType":"text/plain",
			"headers":{"X-Meta":{"schema":{"type":"string"}}},
			"style":"form","explode":false,
			"encoding":{"nested":{}},
			"prefixEncoding":[{}],"itemEncoding":{}
		}`),
		"multipart/form-data",
		"",
		10,
		100,
	)
	if err != nil {
		t.Fatal(err)
	}
	if applied.ContentType != "text/plain" ||
		applied.Serialized != "part=a b" ||
		!applied.SerializationApplied ||
		len(applied.Headers) != 1 ||
		applied.Headers[0].Name != "X-Meta" {
		t.Fatalf("applied = %#v", applied)
	}
	if applied.Encoding.Kind() != jsonvalue.ObjectKind ||
		applied.NamedEncoding.Kind() != jsonvalue.ObjectKind ||
		applied.PrefixEncoding.Kind() != jsonvalue.ArrayKind ||
		applied.ItemEncoding.Kind() != jsonvalue.ObjectKind {
		t.Fatalf("nested encodings = %#v", applied)
	}
}

func TestApplyEncodingPropagatesFieldErrors(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		encoding   string
		mediaType  string
		maxHeaders int
		maxBytes   int
	}{
		{name: "encoding", encoding: `[]`, mediaType: "multipart/form-data",
			maxHeaders: 1, maxBytes: 1},
		{name: "media type", encoding: `{}`, mediaType: "invalid",
			maxHeaders: 1, maxBytes: 1},
		{name: "content type", encoding: `{"contentType":"image/*, text/*"}`,
			mediaType: "multipart/form-data", maxHeaders: 1, maxBytes: 1},
		{name: "headers", encoding: `{}`, mediaType: "multipart/form-data",
			maxHeaders: 0, maxBytes: 1},
		{name: "serialization", encoding: `{"style":"invalid"}`,
			mediaType: "multipart/form-data", maxHeaders: 1, maxBytes: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := media.ApplyEncoding(
				"part", mustMediaValue(t, `1`), mustMediaValue(t, `{}`),
				mustMediaValue(t, test.encoding), test.mediaType, "",
				test.maxHeaders, test.maxBytes,
			)
			if err == nil {
				t.Fatal("invalid application accepted")
			}
		})
	}
}

func TestSelectEncodingContentTypeUsesSingleAndDefaultValues(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		encoding string
		schema   string
		selected string
		want     string
	}{
		{name: "single", encoding: `{"contentType":"application/json"}`,
			schema: `{}`, want: "application/json"},
		{name: "selected single", encoding: `{"contentType":"image/*"}`,
			schema: `{}`, selected: "image/png", want: "image/png"},
		{name: "binary", encoding: `{}`, schema: `{}`,
			want: "application/octet-stream"},
		{name: "encoded string", encoding: `{}`,
			schema: `{"type":"string","contentEncoding":"base64"}`,
			want:   "application/octet-stream"},
		{name: "string", encoding: `{}`, schema: `{"type":"string"}`,
			want: "text/plain"},
		{name: "integer", encoding: `{}`, schema: `{"type":"integer"}`,
			want: "text/plain"},
		{name: "object", encoding: `{}`, schema: `{"type":"object"}`,
			want: "application/json"},
		{name: "array", encoding: `{}`, schema: `{"type":"array"}`,
			want: "application/json"},
		{name: "schema media type ignored", encoding: `{}`,
			schema: `{"type":"string","contentMediaType":"image/png"}`,
			want:   "text/plain"},
		{name: "explicit overrides schema media type",
			encoding: `{"contentType":"image/jpeg"}`,
			schema:   `{"type":"string","contentMediaType":"image/png"}`,
			want:     "image/jpeg"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := media.SelectEncodingContentType(
				mustMediaValue(t, test.encoding), mustMediaValue(t, test.schema),
				test.selected,
			)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("content type = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSelectEncodingContentTypeRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		encoding string
		schema   string
		selected string
	}{
		{encoding: `[]`, schema: `{}`},
		{encoding: `{}`, schema: `[]`},
		{encoding: `{"contentType":1}`, schema: `{}`},
		{encoding: `{"contentType":""}`, schema: `{}`},
		{encoding: `{"contentType":"not a media type"}`, schema: `{}`},
		{encoding: `{"contentType":"*/json"}`, schema: `{}`},
		{encoding: `{"contentType":","}`, schema: `{}`},
		{encoding: `{"contentType":"image/*"}`, schema: `{}`,
			selected: "image/*"},
		{encoding: `{"contentType":"image/*"}`, schema: `{}`,
			selected: "not a media type"},
	} {
		_, err := media.SelectEncodingContentType(
			mustMediaValue(t, test.encoding), mustMediaValue(t, test.schema),
			test.selected,
		)
		if !errors.Is(err, media.ErrInvalidEncodingContentType) {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestSelectEncodingContentTypeMatchesExactAndUniversalChoices(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		encoding  string
		selected  string
		wantError error
	}{
		{encoding: `{}`, selected: "text/plain",
			wantError: media.ErrEncodingContentTypeSelection},
		{encoding: `{"contentType":"image/*"}`,
			wantError: media.ErrEncodingContentTypeSelection},
		{encoding: `{"contentType":"application/json"}`,
			selected: "application/json"},
		{encoding: `{"contentType":"*/*"}`,
			selected: "application/json"},
	} {
		got, err := media.SelectEncodingContentType(
			mustMediaValue(t, test.encoding), mustMediaValue(t, `{}`),
			test.selected,
		)
		if !errors.Is(err, test.wantError) {
			t.Fatalf("error = %v, want %v", err, test.wantError)
		}
		if test.wantError == nil && got != test.selected {
			t.Fatalf("content type = %q, want %q", got, test.selected)
		}
	}
}

func TestEncodingHeadersFiltersContentTypeForMultipart(t *testing.T) {
	t.Parallel()

	headers, err := media.EncodingHeaders(mustMediaValue(t, `{
		"headers":{
			"X-Trace":{"schema":{"type":"string"}},
			"content-type":{"schema":{"type":"string"}},
			"X-Count":{"$ref":"#/components/headers/Count"}
		}
	}`), true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 2 || headers[0].Name != "X-Trace" ||
		headers[1].Name != "X-Count" {
		t.Fatalf("headers = %#v", headers)
	}
	if headers[0].Value.Kind() != jsonvalue.ObjectKind ||
		headers[1].Value.Kind() != jsonvalue.ObjectKind {
		t.Fatalf("header descriptors were not retained: %#v", headers)
	}
}

func TestEncodingHeadersIgnoresAllHeadersOutsideMultipart(t *testing.T) {
	t.Parallel()

	headers, err := media.EncodingHeaders(
		mustMediaValue(t, `{"headers":{"X-Test":{}}}`), false, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 0 {
		t.Fatalf("headers = %#v", headers)
	}
	headers, err = media.EncodingHeaders(mustMediaValue(t, `{}`), true, 1)
	if err != nil || len(headers) != 0 {
		t.Fatalf("absent headers = %#v, %v", headers, err)
	}
}

func TestEncodingHeadersValidatesInputsAndBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		encoding string
		maximum  int
		want     error
	}{
		{encoding: `[]`, maximum: 1, want: media.ErrInvalidEncodingHeaders},
		{encoding: `{}`, maximum: 0, want: media.ErrInvalidEncodingHeaders},
		{encoding: `{"headers":[]}`, maximum: 1,
			want: media.ErrInvalidEncodingHeaders},
		{encoding: `{"headers":{"X":1}}`, maximum: 1,
			want: media.ErrInvalidEncodingHeaders},
		{encoding: `{"headers":{"X":{},"Y":{}}}`, maximum: 1,
			want: media.ErrEncodingHeaderLimit},
	} {
		_, err := media.EncodingHeaders(
			mustMediaValue(t, test.encoding), true, test.maximum,
		)
		if !errors.Is(err, test.want) {
			t.Fatalf("EncodingHeaders(%s) error = %v, want %v",
				test.encoding, err, test.want)
		}
	}
}

func TestSerializeEncodingAppliesFormURLEncoding(t *testing.T) {
	t.Parallel()

	encoded, applied, err := media.SerializeEncoding(
		"field", mustMediaValue(t, `"a b+c~"`),
		mustMediaValue(t, `{"explode":false,"contentType":"text/plain"}`),
		"application/x-www-form-urlencoded", 100,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !applied || encoded != "field=a+b%2Bc%7E" {
		t.Fatalf("serialized = %q, %t", encoded, applied)
	}
	if strings.HasPrefix(encoded, "?") {
		t.Fatalf("form body has query marker: %q", encoded)
	}
}

func TestSerializeEncodingDoesNotPercentEncodeMultipartFormData(t *testing.T) {
	t.Parallel()

	for _, encoding := range []string{
		`{"style":"form"}`,
		`{"style":"form","allowReserved":false}`,
		`{"style":"form","allowReserved":true}`,
	} {
		encoded, applied, err := media.SerializeEncoding(
			"field", mustMediaValue(t, `"a b+c~/%"`),
			mustMediaValue(t, encoding), "multipart/form-data; boundary=test", 100,
		)
		if err != nil {
			t.Fatal(err)
		}
		if !applied || encoded != "field=a b+c~/%" {
			t.Fatalf("serialized = %q, %t", encoded, applied)
		}
	}
}

func TestSerializeEncodingUsesQueryStyleDefaults(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		encoding string
		value    string
		want     string
	}{
		{encoding: `{"explode":true}`, value: `["a","b"]`,
			want: "field=a&field=b"},
		{encoding: `{"style":"deepObject"}`, value: `{"x":"a/b"}`,
			want: "field%5Bx%5D=a%2Fb"},
		{encoding: `{"style":"deepObject","allowReserved":true}`,
			value: `{"x":"a/b"}`, want: "field%5Bx%5D=a/b"},
	} {
		got, applied, err := media.SerializeEncoding(
			"field", mustMediaValue(t, test.value),
			mustMediaValue(t, test.encoding),
			"application/x-www-form-urlencoded", 100,
		)
		if err != nil {
			t.Fatal(err)
		}
		if !applied || got != test.want {
			t.Fatalf("serialized = %q, %t; want %q", got, applied, test.want)
		}
	}
}

func TestSerializeEncodingIgnoresInapplicableOrAbsentFields(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		mediaType string
		encoding  string
	}{
		{mediaType: "application/json", encoding: `{"style":1}`},
		{mediaType: "application/x-www-form-urlencoded", encoding: `{}`},
		{mediaType: "multipart/form-data", encoding: `{"contentType":"text/plain"}`},
	} {
		got, applied, err := media.SerializeEncoding(
			"field", mustMediaValue(t, `"value"`),
			mustMediaValue(t, test.encoding), test.mediaType, 100,
		)
		if err != nil || applied || got != "" {
			t.Fatalf("SerializeEncoding() = %q, %t, %v", got, applied, err)
		}
	}
}

func TestSerializeEncodingValidatesFieldsAndBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		value     string
		encoding  string
		mediaType string
		maximum   int
		want      error
	}{
		{name: "encoding", value: `1`, encoding: `[]`,
			mediaType: "application/x-www-form-urlencoded", maximum: 10,
			want: media.ErrInvalidEncodingSerialization},
		{name: "maximum", value: `1`, encoding: `{}`,
			mediaType: "application/x-www-form-urlencoded", maximum: 0,
			want: media.ErrInvalidEncodingSerialization},
		{name: "media type", value: `1`, encoding: `{"style":"form"}`,
			mediaType: "not a media type", maximum: 10,
			want: media.ErrInvalidEncodingSerialization},
		{name: "style type", value: `1`, encoding: `{"style":1}`,
			mediaType: "application/x-www-form-urlencoded", maximum: 10,
			want: media.ErrInvalidEncodingSerialization},
		{name: "explode type", value: `1`, encoding: `{"explode":1}`,
			mediaType: "application/x-www-form-urlencoded", maximum: 10,
			want: media.ErrInvalidEncodingSerialization},
		{name: "reserved type", value: `1`, encoding: `{"allowReserved":1}`,
			mediaType: "application/x-www-form-urlencoded", maximum: 10,
			want: media.ErrInvalidEncodingSerialization},
		{name: "unsupported style", value: `1`,
			encoding:  `{"style":"matrix"}`,
			mediaType: "application/x-www-form-urlencoded", maximum: 10,
			want: media.ErrInvalidEncodingSerialization},
		{name: "unsupported value", value: `[[1]]`,
			encoding:  `{"explode":false}`,
			mediaType: "application/x-www-form-urlencoded", maximum: 100,
			want: media.ErrInvalidEncodingSerialization},
		{name: "output", value: `"long"`, encoding: `{"explode":false}`,
			mediaType: "multipart/form-data", maximum: 4,
			want: media.ErrEncodingSerializationLimit},
		{name: "encoded output", value: `"long"`,
			encoding:  `{"explode":false}`,
			mediaType: "application/x-www-form-urlencoded", maximum: 4,
			want: media.ErrEncodingSerializationLimit},
		{name: "parameter output", value: `""`,
			encoding:  `{"explode":false}`,
			mediaType: "application/x-www-form-urlencoded", maximum: 1,
			want: media.ErrEncodingSerializationLimit},
	} {
		_, _, err := media.SerializeEncoding(
			"field", mustMediaValue(t, test.value),
			mustMediaValue(t, test.encoding), test.mediaType, test.maximum,
		)
		if !errors.Is(err, test.want) {
			t.Fatalf("%s error = %v, want %v", test.name, err, test.want)
		}
	}
}

func TestSerializeEncodingHandlesTheLargestMultipartBound(t *testing.T) {
	t.Parallel()

	got, applied, err := media.SerializeEncoding(
		"field", mustMediaValue(t, `1`), mustMediaValue(t, `{"style":"form"}`),
		"multipart/form-data", int(^uint(0)>>1),
	)
	if err != nil || !applied || got != "field=1" {
		t.Fatalf("SerializeEncoding() = %q, %t, %v", got, applied, err)
	}
}

func TestSerializeEncodingAcceptsExactSerializationBounds(t *testing.T) {
	t.Parallel()

	got, applied, err := media.SerializeEncoding(
		"x", mustMediaValue(t, `""`), mustMediaValue(t, `{"style":"form"}`),
		"multipart/form-data", 2,
	)
	if err != nil || !applied || got != "x=" {
		t.Fatalf("exact multipart serialization = %q, %t, %v", got, applied, err)
	}
	got, applied, err = media.SerializeEncoding(
		"x", mustMediaValue(t, `"😀"`), mustMediaValue(t, `{"style":"form"}`),
		"multipart/form-data", len("x=😀"),
	)
	if err != nil || !applied || got != "x=😀" {
		t.Fatalf("expanded multipart serialization = %q, %t, %v", got, applied, err)
	}
	got, applied, err = media.SerializeEncoding(
		"x", mustMediaValue(t, `1`), mustMediaValue(t, `{}`),
		"application/x-www-form-urlencoded", 1,
	)
	if err != nil || applied || got != "" {
		t.Fatalf("minimum serialization bound = %q, %t, %v", got, applied, err)
	}
}

func TestSerializeEncodingHandlesMultipartScalingThreshold(t *testing.T) {
	t.Parallel()

	maximum := int(^uint(0) >> 1)
	for _, limit := range []int{maximum / 3, maximum/3 + 1} {
		got, applied, err := media.SerializeEncoding(
			"x", mustMediaValue(t, `1`), mustMediaValue(t, `{"style":"form"}`),
			"multipart/form-data", limit,
		)
		if err != nil || !applied || got != "x=1" {
			t.Fatalf("limit %d serialization = %q, %t, %v", limit, got, applied, err)
		}
	}
}

func TestSerializeEncodingForVersionAppliesOpenAPI31StyleRules(t *testing.T) {
	t.Parallel()

	version, err := specversion.Parse("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = media.SerializeEncodingForVersion(
		version, "field", mustMediaValue(t, `{"x":"one"}`),
		mustMediaValue(t, `{"style":"deepObject"}`),
		"application/x-www-form-urlencoded", 100,
	)
	if !errors.Is(err, media.ErrInvalidEncodingSerialization) {
		t.Fatalf("implicit OpenAPI 3.1 deepObject explode error = %v", err)
	}
	got, applied, err := media.SerializeEncodingForVersion(
		version, "field", mustMediaValue(t, `{"x":"one"}`),
		mustMediaValue(t, `{"style":"deepObject","explode":true}`),
		"application/x-www-form-urlencoded", 100,
	)
	if err != nil || !applied || got != "field%5Bx%5D=one" {
		t.Fatalf("SerializeEncodingForVersion() = %q, %t, %v", got, applied, err)
	}
}

func TestSerializeEncodingForVersionAppliesOpenAPI30FormRules(t *testing.T) {
	t.Parallel()

	for _, rawVersion := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
	} {
		version, err := specversion.Parse(rawVersion)
		if err != nil {
			t.Fatal(err)
		}
		got, applied, err := media.SerializeEncodingForVersion(
			version, "field", mustMediaValue(t, `"a/b"`),
			mustMediaValue(t, `{"allowReserved":true}`),
			"application/x-www-form-urlencoded", 100,
		)
		if err != nil || !applied || got != "field=a/b" {
			t.Fatalf("version %s form encoding = %q, %t, %v",
				rawVersion, got, applied, err)
		}
		for _, mediaType := range []string{"multipart/form-data", "text/plain"} {
			got, applied, err = media.SerializeEncodingForVersion(
				version, "field", mustMediaValue(t, `"a/b"`),
				mustMediaValue(t, `{"allowReserved":true}`), mediaType, 100,
			)
			if err != nil || applied || got != "" {
				t.Fatalf("version %s ignored %s encoding = %q, %t, %v",
					rawVersion, mediaType, got, applied, err)
			}
		}
	}
}

func TestSerializeEncodingForVersionAppliesOpenAPI304ContentTypeRules(t *testing.T) {
	t.Parallel()

	version, err := specversion.Parse("3.0.4")
	if err != nil {
		t.Fatal(err)
	}
	got, applied, err := media.SerializeEncodingForVersion(
		version, "field", mustMediaValue(t, `"a/b"`),
		mustMediaValue(t, `{
			"contentType":"application/json",
			"allowReserved":true
		}`),
		"application/x-www-form-urlencoded", 100,
	)
	if err != nil || !applied || got != "field=a/b" {
		t.Fatalf("explicit serialization fields = %q, %t, %v", got, applied, err)
	}
	got, applied, err = media.SerializeEncodingForVersion(
		version, "field", mustMediaValue(t, `"value"`),
		mustMediaValue(t, `{"contentType":"text/plain"}`),
		"application/x-www-form-urlencoded", 100,
	)
	if err != nil || applied || got != "" {
		t.Fatalf("contentType-only encoding = %q, %t, %v", got, applied, err)
	}
}

func TestSerializeEncodingForVersionAppliesOpenAPI310AllowReserved(t *testing.T) {
	t.Parallel()

	version, err := specversion.Parse("3.1.0")
	if err != nil {
		t.Fatal(err)
	}
	got, applied, err := media.SerializeEncodingForVersion(
		version, "field", mustMediaValue(t, `"a/b"`),
		mustMediaValue(t, `{
			"contentType":"text/plain",
			"allowReserved":true
		}`),
		"application/x-www-form-urlencoded", 100,
	)
	if err != nil || !applied || got != "field=a/b" {
		t.Fatalf("allowReserved serialization = %q, %t, %v", got, applied, err)
	}
	got, applied, err = media.SerializeEncodingForVersion(
		version, "field", mustMediaValue(t, `"a/b"`),
		mustMediaValue(t, `{"allowReserved":true}`),
		"application/json", 100,
	)
	if err != nil || applied || got != "" {
		t.Fatalf("inapplicable allowReserved = %q, %t, %v", got, applied, err)
	}
}

func TestSerializeEncodingForVersionAllowsContentAndSerializationFields(t *testing.T) {
	t.Parallel()

	for _, rawVersion := range []string{"3.0.4", "3.1.1", "3.1.2"} {
		version, err := specversion.Parse(rawVersion)
		if err != nil {
			t.Fatal(err)
		}
		got, applied, err := media.SerializeEncodingForVersion(
			version, "field", mustMediaValue(t, `"a/b"`),
			mustMediaValue(t, `{
				"contentType":"application/json",
				"allowReserved":true
			}`),
			"application/x-www-form-urlencoded", 100,
		)
		if err != nil || !applied || got != "field=a/b" {
			t.Errorf("OpenAPI %s serialization = %q, %t, %v",
				rawVersion, got, applied, err)
		}
	}
}

func TestSerializeEncodingForVersionIgnoresFieldsByMediaContext(t *testing.T) {
	t.Parallel()

	encoding := mustMediaValue(t, `{
		"contentType":"application/json",
		"style":"form",
		"explode":true,
		"allowReserved":true
	}`)
	for _, rawVersion := range []string{"3.1.0", "3.1.1", "3.1.2"} {
		version, err := specversion.Parse(rawVersion)
		if err != nil {
			t.Fatal(err)
		}
		got, applied, err := media.SerializeEncodingForVersion(
			version, "field", mustMediaValue(t, `"a/b"`),
			encoding, "text/plain", 100,
		)
		if err != nil || applied || got != "" {
			t.Errorf("OpenAPI %s non-form encoding = %q, %t, %v",
				rawVersion, got, applied, err)
		}
		got, applied, err = media.SerializeEncodingForVersion(
			version, "field", mustMediaValue(t, `"a/b"`),
			encoding, "application/x-www-form-urlencoded", 100,
		)
		if err != nil || !applied || got != "field=a/b" {
			t.Errorf("OpenAPI %s form encoding = %q, %t, %v",
				rawVersion, got, applied, err)
		}
	}
}

func TestSerializeEncodingForVersionRejectsUnsupportedDialects(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"2.0"} {
		parsed, err := specversion.Parse(version)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = media.SerializeEncodingForVersion(
			parsed, "field", mustMediaValue(t, `"one"`),
			mustMediaValue(t, `{"style":"form"}`),
			"application/x-www-form-urlencoded", 100,
		)
		if !errors.Is(err, media.ErrInvalidEncodingSerialization) {
			t.Fatalf("version %s error = %v", version, err)
		}
	}
}

func TestEncodingMappersApplyNestedEncodingFields(t *testing.T) {
	t.Parallel()

	parent := mustMediaValue(t, `{
		"encoding":{"child":{"contentType":"application/json"}},
		"prefixEncoding":[{"contentType":"text/plain"}],
		"itemEncoding":{"contentType":"application/octet-stream"}
	}`)
	named, exists := parent.Lookup("encoding")
	if !exists {
		t.Fatal("nested named encodings are absent")
	}
	namedValues, err := media.NamedEncodingValues(
		mustMediaValue(t, `{"child":{"type":"object"}}`), named,
		mustMediaValue(t, `{"child":{"id":1}}`), 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(namedValues) != 1 || namedValues[0].Name != "child" {
		t.Fatalf("nested named values = %#v", namedValues)
	}
	positional, err := media.PositionalEncodings(parent, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(positional) != 2 || positional[0].Index != 0 ||
		positional[1].Index != 1 {
		t.Fatalf("nested positional values = %#v", positional)
	}
}

func TestFormURLEncodeAppliesWHATWGRulesAfterValueSerialization(t *testing.T) {
	t.Parallel()

	encoded, err := media.FormURLEncode([]media.FormField{
		{Name: "id", Value: "f81d4fae-7dec"},
		{Name: "address", Value: `{"street":"123 Example Dr.","zip":"99999+1234"}`},
		{Name: "tag", Value: "one"},
		{Name: "tag", Value: "two~*"},
	}, 10, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	want := "id=f81d4fae-7dec&" +
		"address=%7B%22street%22%3A%22123+Example+Dr.%22%2C" +
		"%22zip%22%3A%2299999%2B1234%22%7D&" +
		"tag=one&tag=two%7E*"
	if encoded != want {
		t.Fatalf("form body = %q, want %q", encoded, want)
	}
}

func TestFormURLEncodeValidatesInputAndOutputBounds(t *testing.T) {
	t.Parallel()

	invalidUTF8 := string([]byte{0xff})
	for _, test := range []struct {
		fields    []media.FormField
		maxFields int
		maxBytes  int
		want      error
	}{
		{maxFields: 0, maxBytes: 1, want: media.ErrInvalidFormURLEncoding},
		{maxFields: 1, maxBytes: 0, want: media.ErrInvalidFormURLEncoding},
		{fields: []media.FormField{{Name: invalidUTF8, Value: "x"}},
			maxFields: 1, maxBytes: 10, want: media.ErrInvalidFormURLEncoding},
		{fields: []media.FormField{{Name: "x", Value: invalidUTF8}},
			maxFields: 1, maxBytes: 10, want: media.ErrInvalidFormURLEncoding},
		{fields: []media.FormField{{Name: "x"}, {Name: "y"}},
			maxFields: 1, maxBytes: 10, want: media.ErrFormURLEncodingLimit},
		{fields: []media.FormField{{Name: "long", Value: "value"}},
			maxFields: 1, maxBytes: 5, want: media.ErrFormURLEncodingLimit},
		{fields: []media.FormField{{Name: "x"}, {Name: "y"}},
			maxFields: 2, maxBytes: 2, want: media.ErrFormURLEncodingLimit},
		{fields: []media.FormField{{Name: "a "}},
			maxFields: 1, maxBytes: 1, want: media.ErrFormURLEncodingLimit},
		{fields: []media.FormField{{Name: "!"}},
			maxFields: 1, maxBytes: 2, want: media.ErrFormURLEncodingLimit},
	} {
		_, err := media.FormURLEncode(test.fields, test.maxFields, test.maxBytes)
		if !errors.Is(err, test.want) {
			t.Fatalf("FormURLEncode() error = %v, want %v", err, test.want)
		}
	}
}

func TestFormURLEncodeAcceptsExactCharacterAndOutputBounds(t *testing.T) {
	t.Parallel()

	fields := []media.FormField{{Name: "AZaz09*-._ !", Value: "x"}}
	want := "AZaz09*-._+%21=x"
	got, err := media.FormURLEncode(fields, 1, len(want))
	if err != nil || got != want {
		t.Fatalf("exact form body = %q, %v; want %q", got, err, want)
	}
	if _, err = media.FormURLEncode(fields, 1, len(want)-1); !errors.Is(err, media.ErrFormURLEncodingLimit) {
		t.Fatalf("one-byte-short error = %v", err)
	}
	if _, err = media.FormURLEncode(
		[]media.FormField{{Name: "x", Value: "!"}}, 1, len("x=%21")-1,
	); !errors.Is(err, media.ErrFormURLEncodingLimit) {
		t.Fatalf("terminal percent triple error = %v", err)
	}
	if got, err = media.FormURLEncode(
		[]media.FormField{{Name: "x", Value: "!"}}, 1, len("x=%21"),
	); err != nil || got != "x=%21" {
		t.Fatalf("exact terminal percent triple = %q, %v", got, err)
	}
}

func TestExternalExampleTextDefaultsUnknownCharsetsToUTF8(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		contentType string
		raw         string
	}{
		{contentType: "", raw: "plain €"},
		{contentType: "application/octet-stream", raw: "plain €"},
		{contentType: "text/plain; charset=utf-8", raw: "plain €"},
		{contentType: "text/plain; charset=US-ASCII", raw: "plain"},
	} {
		got, err := media.ExternalExampleText(
			[]byte(test.raw), test.contentType, 100,
		)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.raw {
			t.Fatalf("text = %q, want %q", got, test.raw)
		}
	}
}

func TestExternalExampleTextRejectsInvalidOrUnboundedText(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw         []byte
		contentType string
		maximum     int
		want        error
	}{
		{raw: []byte("x"), maximum: 0, want: media.ErrInvalidExternalExample},
		{raw: []byte("long"), maximum: 3, want: media.ErrExternalExampleLimit},
		{raw: []byte{0xff}, maximum: 1, want: media.ErrInvalidExternalExample},
		{raw: []byte("€"), contentType: "text/plain; charset=us-ascii",
			maximum: 10, want: media.ErrInvalidExternalExample},
		{raw: []byte("x"), contentType: "not a media type", maximum: 10,
			want: media.ErrInvalidExternalExample},
		{raw: []byte("x"), contentType: "text/plain; charset=iso-8859-1",
			maximum: 10, want: media.ErrUnsupportedExternalExampleCharset},
	} {
		_, err := media.ExternalExampleText(test.raw, test.contentType, test.maximum)
		if !errors.Is(err, test.want) {
			t.Fatalf("ExternalExampleText() error = %v, want %v", err, test.want)
		}
	}
}

func TestExternalExampleTextAcceptsExactBoundsAndASCIIEndpoint(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw         []byte
		contentType string
	}{
		{raw: []byte("x")},
		{raw: []byte{0x7f}, contentType: "text/plain; charset=us-ascii"},
	} {
		got, err := media.ExternalExampleText(test.raw, test.contentType, 1)
		if err != nil || got != string(test.raw) {
			t.Fatalf("ExternalExampleText(%x) = %q, %v", test.raw, got, err)
		}
	}
}

func mustMediaValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := parse.JSON(
		context.Background(), strings.NewReader(raw), parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
