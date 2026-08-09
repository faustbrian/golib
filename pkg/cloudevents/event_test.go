package cloudevents

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestNewEventOwnsInputsAndPreservesContext(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"order":"A-123"}`)
	occurredAt := time.Date(2026, time.August, 9, 1, 2, 3, 456000000, time.FixedZone("EEST", 3*60*60))
	tenant, err := NewStringAttribute("tenant-a")
	if err != nil {
		t.Fatalf("create tenant attribute: %v", err)
	}
	extensions := map[string]Attribute{"tenantid": tenant}
	data, err := NewJSONData(payload)
	if err != nil {
		t.Fatalf("create JSON data: %v", err)
	}

	event, err := NewEvent(Attributes{
		ID:              "evt-123",
		Source:          "https://orders.example/tenant-a",
		Type:            "com.example.order.created.v1",
		DataContentType: "application/json",
		DataSchema:      "https://schemas.example/order-created-v1.json",
		Subject:         "orders/A-123",
		Time:            &occurredAt,
		Extensions:      extensions,
	}, data)
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	payload[2] = 'X'
	delete(extensions, "tenantid")
	occurredAt = time.Time{}

	if got := event.SpecVersion(); got != "1.0" {
		t.Fatalf("spec version = %q, want 1.0", got)
	}
	if got := event.ID(); got != "evt-123" {
		t.Fatalf("id = %q, want evt-123", got)
	}
	if got := event.Source(); got != "https://orders.example/tenant-a" {
		t.Fatalf("source = %q", got)
	}
	if got := event.Type(); got != "com.example.order.created.v1" {
		t.Fatalf("type = %q", got)
	}
	if got, ok := event.DataContentType(); !ok || got != "application/json" {
		t.Fatalf("datacontenttype = %q, %v", got, ok)
	}
	if got, ok := event.DataSchema(); !ok || got != "https://schemas.example/order-created-v1.json" {
		t.Fatalf("dataschema = %q, %v", got, ok)
	}
	if got, ok := event.Subject(); !ok || got != "orders/A-123" {
		t.Fatalf("subject = %q, %v", got, ok)
	}
	if got, ok := event.Time(); !ok || !got.Equal(time.Date(2026, time.August, 8, 22, 2, 3, 456000000, time.UTC)) {
		t.Fatalf("time = %v, %v", got, ok)
	}
	if got, ok := event.Extension("tenantid"); !ok || got.String() != "tenant-a" {
		t.Fatalf("tenantid = %v, %v", got, ok)
	}
	ownedExtensions := event.Extensions()
	delete(ownedExtensions, "tenantid")
	if got, ok := event.Extension("tenantid"); !ok || got.String() != "tenant-a" {
		t.Fatalf("extension map mutation changed event = %v, %v", got, ok)
	}

	gotData := event.Data().Bytes()
	if !bytes.Equal(gotData, []byte(`{"order":"A-123"}`)) {
		t.Fatalf("data = %q", gotData)
	}
	gotData[0] = 'X'
	if bytes.Equal(event.Data().Bytes(), gotData) {
		t.Fatal("event data aliases returned bytes")
	}
}

func TestNewEventReportsCanonicalValidationIssues(t *testing.T) {
	t.Parallel()

	_, err := NewEvent(Attributes{
		Source:          "https://example.com/%zz",
		Type:            "com.example.invalid\ntype",
		DataContentType: "text/plain; charset",
		DataSchema:      "/schemas/event.json",
		Extensions: map[string]Attribute{
			"SpecVersion": {},
			"data":        {},
			"id":          {},
		},
	}, Data{})
	if err == nil {
		t.Fatal("NewEvent() error = nil")
	}
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("error %v does not match ErrInvalidEvent", err)
	}

	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error type = %T, want *ValidationError", err)
	}
	want := []Issue{
		{Field: "datacontenttype", Code: IssueInvalidMediaType},
		{Field: "dataschema", Code: IssueAbsoluteURIRequired},
		{Field: "extensions.SpecVersion", Code: IssueInvalidName},
		{Field: "extensions.data", Code: IssueReservedName},
		{Field: "extensions.id", Code: IssueReservedName},
		{Field: "id", Code: IssueRequired},
		{Field: "source", Code: IssueInvalidURIReference},
		{Field: "type", Code: IssueInvalidString},
	}
	if got := validation.Issues(); !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %#v, want %#v", got, want)
	}

	got := validation.Issues()
	got[0] = Issue{}
	if reflect.DeepEqual(validation.Issues(), got) {
		t.Fatal("Issues() aliases validation storage")
	}
	if wantMessage := "cloudevents: invalid event: datacontenttype invalid_media_type; dataschema absolute_uri_required; extensions.SpecVersion invalid_name; extensions.data reserved_name; extensions.id reserved_name; id required; source invalid_uri_reference; type invalid_string"; err.Error() != wantMessage {
		t.Fatalf("error = %q, want %q", err, wantMessage)
	}
}

func TestDataKindsPreserveAbsentNullEmptyAndBinaryValues(t *testing.T) {
	t.Parallel()

	absent := Data{}
	if absent.Present() || absent.Kind() != DataAbsent || absent.Bytes() != nil {
		t.Fatalf("absent data = present %v, kind %v, bytes %v", absent.Present(), absent.Kind(), absent.Bytes())
	}

	nullData, err := NewJSONData([]byte("null"))
	if err != nil {
		t.Fatalf("create null JSON data: %v", err)
	}
	if !nullData.Present() || nullData.Kind() != DataJSON || !bytes.Equal(nullData.Bytes(), []byte("null")) {
		t.Fatalf("null JSON data = present %v, kind %v, bytes %q", nullData.Present(), nullData.Kind(), nullData.Bytes())
	}

	emptyText, err := NewTextData("")
	if err != nil {
		t.Fatalf("create empty text data: %v", err)
	}
	if !emptyText.Present() || emptyText.Kind() != DataText || len(emptyText.Bytes()) != 0 {
		t.Fatalf("empty text data = present %v, kind %v, bytes %q", emptyText.Present(), emptyText.Kind(), emptyText.Bytes())
	}

	emptyBinary := NewBinaryData(nil)
	if !emptyBinary.Present() || emptyBinary.Kind() != DataBinary || len(emptyBinary.Bytes()) != 0 {
		t.Fatalf("empty binary data = present %v, kind %v, bytes %q", emptyBinary.Present(), emptyBinary.Kind(), emptyBinary.Bytes())
	}

	if _, err := NewJSONData([]byte(`{"unterminated":`)); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("invalid JSON error = %v, want ErrInvalidData", err)
	}
	if _, err := NewTextData(string([]byte{0xff})); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("invalid UTF-8 text error = %v, want ErrInvalidData", err)
	}
}

func TestAttributeTypesEnforceCloudEventsTypeSystem(t *testing.T) {
	t.Parallel()

	boolean := NewBooleanAttribute(true)
	if boolean.Kind() != AttributeBoolean || boolean.String() != "true" {
		t.Fatalf("boolean attribute = kind %v, string %q", boolean.Kind(), boolean.String())
	}

	integer, err := NewIntegerAttribute(-2147483648)
	if err != nil || integer.Kind() != AttributeInteger || integer.String() != "-2147483648" {
		t.Fatalf("integer attribute = %v, %v", integer, err)
	}
	if _, err := NewIntegerAttribute(2147483648); !errors.Is(err, ErrInvalidAttribute) {
		t.Fatalf("out-of-range integer error = %v", err)
	}

	stringValue, err := NewStringAttribute("Helsinki €")
	if err != nil || stringValue.Kind() != AttributeString || stringValue.String() != "Helsinki €" {
		t.Fatalf("string attribute = %v, %v", stringValue, err)
	}
	for _, invalid := range []string{"line\nbreak", string([]byte{0xff}), "noncharacter\ufdd0"} {
		if _, err := NewStringAttribute(invalid); !errors.Is(err, ErrInvalidAttribute) {
			t.Fatalf("invalid string %q error = %v", invalid, err)
		}
	}

	binary := NewBinaryAttribute([]byte{0x00, 0xff})
	if binary.Kind() != AttributeBinary || binary.String() != "AP8=" || !bytes.Equal(binary.Bytes(), []byte{0x00, 0xff}) {
		t.Fatalf("binary attribute = kind %v, string %q, bytes %v", binary.Kind(), binary.String(), binary.Bytes())
	}
	returned := binary.Bytes()
	returned[0] = 0xff
	if bytes.Equal(returned, binary.Bytes()) {
		t.Fatal("binary attribute aliases returned bytes")
	}

	absoluteURI, err := NewURIAttribute("https://example.com/schemas/event.json")
	if err != nil || absoluteURI.Kind() != AttributeURI {
		t.Fatalf("URI attribute = %v, %v", absoluteURI, err)
	}
	if _, err := NewURIAttribute("/relative"); !errors.Is(err, ErrInvalidAttribute) {
		t.Fatalf("relative absolute-URI error = %v", err)
	}
	for _, invalid := range []string{"https://example.com/has space", "https://example.com/ümlaut"} {
		if _, err := NewURIAttribute(invalid); !errors.Is(err, ErrInvalidAttribute) {
			t.Fatalf("non-RFC3986 URI %q error = %v", invalid, err)
		}
	}
	reference, err := NewURIReferenceAttribute("/relative/path")
	if err != nil || reference.Kind() != AttributeURIReference {
		t.Fatalf("URI-reference attribute = %v, %v", reference, err)
	}
	if _, err := NewURIReferenceAttribute("/has space"); !errors.Is(err, ErrInvalidAttribute) {
		t.Fatalf("non-RFC3986 URI-reference error = %v", err)
	}

	timestamp := NewTimestampAttribute(time.Date(2026, time.August, 9, 4, 5, 6, 120000000, time.FixedZone("EEST", 3*60*60)))
	if timestamp.Kind() != AttributeTimestamp || timestamp.String() != "2026-08-09T01:05:06.12Z" {
		t.Fatalf("timestamp attribute = kind %v, string %q", timestamp.Kind(), timestamp.String())
	}
}

func TestEventsRejectUnassignedExtensionValuesAndZeroValueEncoding(t *testing.T) {
	t.Parallel()

	_, err := NewEvent(Attributes{
		ID:         "1",
		Source:     "/source",
		Type:       "example",
		Extensions: map[string]Attribute{"custom": {}},
	}, Data{})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("unassigned extension error = %v", err)
	}

	if _, err := EncodeJSON(Event{}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("zero Event EncodeJSON() error = %v", err)
	}
	if _, _, err := EncodeHTTP([]Event{{}}, StructuredMode); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("zero Event EncodeHTTP() error = %v", err)
	}
	if _, err := EncodeKafka(Event{}, BinaryMode, nil); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("zero Event EncodeKafka() error = %v", err)
	}
}
