package cloudevents

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEventValidationRejectsCorruptedInternalDataStates(t *testing.T) {
	t.Parallel()

	valid := Event{id: "1", source: "/source", eventType: "example"}
	tests := []struct {
		name string
		data Data
	}{
		{name: "absent nonzero kind", data: Data{kind: DataText}},
		{name: "invalid JSON", data: Data{kind: DataJSON, present: true, bytes: []byte("{")}},
		{name: "invalid text", data: Data{kind: DataText, present: true, bytes: []byte{0xff}}},
		{name: "unknown kind", data: Data{kind: 255, present: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := valid
			event.data = test.data
			if err := event.Validate(); !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("Validate() error = %v, want ErrInvalidEvent", err)
			}
		})
	}

	valid.data = Data{kind: DataBinary, present: true, bytes: []byte{0xff}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("binary Validate() error = %v", err)
	}
	if got := (Attribute{kind: AttributeString, text: "value"}).Bytes(); got != nil {
		t.Fatalf("non-binary Attribute.Bytes() = %v, want nil", got)
	}
}

func TestAttributeValidationCoversEveryAbstractTypeAndInvariant(t *testing.T) {
	t.Parallel()

	timestamp := NewTimestampAttribute(time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC))
	uri, err := NewURIAttribute("https://example.com/schema")
	if err != nil {
		t.Fatal(err)
	}
	reference, err := NewURIReferenceAttribute("/source")
	if err != nil {
		t.Fatal(err)
	}
	integer, err := NewIntegerAttribute(42)
	if err != nil {
		t.Fatal(err)
	}
	valid := []Attribute{
		NewBooleanAttribute(false), integer, NewBinaryAttribute([]byte("x")), uri, reference, timestamp,
	}
	maximum, err := NewIntegerAttribute(2147483647)
	if err != nil || maximum.String() != "2147483647" {
		t.Fatalf("maximum integer = %#v, %v", maximum, err)
	}
	for _, attribute := range valid {
		if !validAttribute(attribute) {
			t.Fatalf("validAttribute(%#v) = false", attribute)
		}
	}
	invalid := []Attribute{
		{kind: AttributeBoolean, text: "TRUE"},
		{kind: AttributeInteger, text: "01"},
		{kind: AttributeBinary, text: "eA==", bytes: []byte("different")},
		{kind: AttributeURI, text: "/relative"},
		{kind: AttributeURIReference, text: "/has space"},
		{kind: AttributeTimestamp, text: "2026-08-09T01:02:03+01:00"},
		{},
	}
	for _, attribute := range invalid {
		if validAttribute(attribute) {
			t.Fatalf("validAttribute(%#v) = true", attribute)
		}
	}
}

func TestAttributeNamesAndCloudStringsEnforceExactCharacterBoundaries(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"a", "z", "0", "9", "a0z9"} {
		if !validAttributeName(name) {
			t.Fatalf("validAttributeName(%q) = false", name)
		}
	}
	for _, name := range []string{"`", "{", "/", ":"} {
		if validAttributeName(name) {
			t.Fatalf("validAttributeName(%q) = true", name)
		}
	}

	for _, value := range []string{"\u0020", "\u00a0", "\ufdcf", "\ufdf0"} {
		if !validCloudString(value) {
			t.Fatalf("validCloudString(%q) = false", value)
		}
	}
	for _, value := range []string{"\u001f", "\u007f", "\u009f", "\ufdd0", "\ufdef", "\U0001fffe", "\U0010ffff"} {
		if validCloudString(value) {
			t.Fatalf("validCloudString(%q) = true", value)
		}
	}
}

func TestValidationReportsSelectedExtensionAndOptionalAttributeFailures(t *testing.T) {
	t.Parallel()

	stringValue, err := NewStringAttribute("value")
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewEvent(Attributes{
		ID:      "1",
		Source:  "/source",
		Type:    "example",
		Subject: "invalid\nsubject",
		Extensions: map[string]Attribute{
			"":             stringValue,
			"traceparent":  stringValue,
			"tracestate":   NewBooleanAttribute(true),
			"partitionkey": NewBooleanAttribute(true),
		},
	}, Data{})
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("NewEvent() error = %v", err)
	}
	want := []Issue{
		{Field: "extensions.", Code: IssueInvalidName},
		{Field: "extensions.partitionkey", Code: IssueInvalidAttribute},
		{Field: "extensions.traceparent", Code: IssueInvalidAttribute},
		{Field: "extensions.tracestate", Code: IssueInvalidAttribute},
		{Field: "subject", Code: IssueInvalidString},
	}
	if got := validation.Issues(); !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %#v, want %#v", got, want)
	}

	_, err = NewEvent(Attributes{ID: "1"}, Data{})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("missing source/type error = %v", err)
	}
	_, err = NewEvent(Attributes{ID: "bad\nid", Source: "/source", Type: "example"}, Data{})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("invalid ID error = %v", err)
	}
	traceParent, traceErr := NewTraceParentAttribute("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if traceErr != nil {
		t.Fatal(traceErr)
	}
	_, err = NewEvent(Attributes{
		ID: "2", Source: "/source", Type: "example",
		Extensions: map[string]Attribute{"traceparent": traceParent, "tracestate": {}},
	}, Data{})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("isolated tracestate error = %v", err)
	}
}

func TestTraceStateRejectsWrongAbstractTypeWithValidSyntax(t *testing.T) {
	t.Parallel()

	traceParent, err := NewTraceParentAttribute("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if err != nil {
		t.Fatal(err)
	}
	wrongType, err := NewURIReferenceAttribute("vendor=value")
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewEvent(Attributes{
		ID:     "1",
		Source: "/source",
		Type:   "example",
		Extensions: map[string]Attribute{
			"traceparent": traceParent,
			"tracestate":  wrongType,
		},
	}, Data{})
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("NewEvent() error = %v", err)
	}
	want := []Issue{{Field: "extensions.tracestate", Code: IssueInvalidAttribute}}
	if got := validation.Issues(); !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %#v, want %#v", got, want)
	}
}

func TestTracingExtensionsRejectEveryGrammarBoundary(t *testing.T) {
	t.Parallel()

	invalidParents := []string{
		"0-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"000-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-short-00f067aa0ba902b7-01",
		"00-04bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-000f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-0",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-001",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-gg",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-extra",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-ab",
	}
	for _, value := range invalidParents {
		if _, err := NewTraceParentAttribute(value); !errors.Is(err, ErrInvalidAttribute) {
			t.Fatalf("traceparent %q error = %v", value, err)
		}
	}
	if _, err := NewTraceParentAttribute("01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-ab"); err != nil {
		t.Fatalf("future traceparent error = %v", err)
	}

	invalidStates := []string{
		string(make([]byte, 513)),
		strings.Repeat("a=v,", 32) + "a=v",
		"a=",
		"=value",
		"1vendor=value",
		"a!=value",
		"tenant@System=value",
		"tenant@@system=value",
		"a=value ",
		"a=has=equals",
		"a=has\x7fcontrol",
		"a=unicode-é",
	}
	for _, value := range invalidStates {
		if _, err := NewTraceStateAttribute(value); !errors.Is(err, ErrInvalidAttribute) {
			t.Fatalf("tracestate %q error = %v", value, err)
		}
	}
}

func TestTraceStateAcceptsExactNormativeBoundaries(t *testing.T) {
	t.Parallel()

	members := make([]string, 32)
	for index := range members {
		members[index] = string(rune('a'+index%26)) + string(rune('0'+index/26)) + "=v"
	}
	valid := []string{
		strings.Join(members, ","),
		strings.Repeat("a", 256) + "=" + strings.Repeat("v", 255),
		strings.Repeat("a", 256) + "=v",
		strings.Repeat("a", 241) + "@" + strings.Repeat("b", 14) + "=v",
		"a@a=v", "z@z=v", "0@a=v", "9@a=v",
		"a= !~", "a=" + strings.Repeat("v", 256),
		"a_z-*/09=value",
	}
	for _, value := range valid {
		if _, err := NewTraceStateAttribute(value); err != nil {
			t.Fatalf("boundary tracestate %q error = %v", value, err)
		}
	}

	invalid := []string{
		strings.Repeat("a", 257) + "=v",
		strings.Repeat("a", 242) + "@b=v",
		"a@" + strings.Repeat("b", 15) + "=v",
		"/tenant@a=v", ":tenant@a=v", "`tenant@a=v", "{tenant@a=v",
		"tenant@`system=v", "tenant@{system=v", "tenant@0system=v",
		"a,=v", "a.=v", "a:=v", "a@=v", "a`=v", "a{=v",
		"tenant!@system=v", "tenant@system!=v",
		"a=" + strings.Repeat("v", 257),
		"a=\x1f", "a=\x7f", "a=comma,value", "a=equals=value",
	}
	for _, value := range invalid {
		if _, err := NewTraceStateAttribute(value); !errors.Is(err, ErrInvalidAttribute) {
			t.Fatalf("boundary tracestate %q error = %v", value, err)
		}
	}
}

type valueSchemaValidator struct{ err error }

func (validator valueSchemaValidator) Validate(context.Context, string, string, []byte) error {
	return validator.err
}

func TestValidateSchemaRejectsEveryPreconditionAndReturnsValidatorError(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateSchema(cancelled, Event{}, valueSchemaValidator{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
	if err := ValidateSchema(context.Background(), Event{}, nil); !errors.Is(err, ErrSchemaValidatorRequired) {
		t.Fatalf("nil validator error = %v", err)
	}
	if err := ValidateSchema(context.Background(), Event{}, valueSchemaValidator{}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("invalid event error = %v", err)
	}

	event, err := NewEvent(Attributes{
		ID: "1", Source: "/source", Type: "example",
		DataSchema: "https://schemas.example/event.json",
	}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSchema(context.Background(), event, valueSchemaValidator{}); !errors.Is(err, ErrDataRequired) {
		t.Fatalf("missing data error = %v", err)
	}

	data, err := NewJSONData([]byte("null"))
	if err != nil {
		t.Fatal(err)
	}
	event, err = NewEvent(Attributes{
		ID: "2", Source: "/source", Type: "example",
		DataSchema: "https://schemas.example/event.json",
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("validator failed")
	if err := ValidateSchema(context.Background(), event, valueSchemaValidator{err: want}); !errors.Is(err, want) {
		t.Fatalf("validator error = %v", err)
	}
}
