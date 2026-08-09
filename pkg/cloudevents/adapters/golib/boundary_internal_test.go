package golib

import (
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/cloudevents"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func TestAdapterHelpersCoverOwnedBoundarySemantics(t *testing.T) {
	t.Parallel()

	if data, err := dataFromPayload("", nil); err != nil || !data.Present() || data.Kind() != cloudevents.DataBinary {
		t.Fatalf("empty content type data = %#v, %v", data, err)
	}
	if _, err := dataFromPayload(";", nil); !errors.Is(err, ErrInvalidAdapterInput) {
		t.Fatalf("invalid media type error = %v", err)
	}
	if data, err := dataFromPayload("application/problem+json", []byte(`{}`)); err != nil || data.Kind() != cloudevents.DataJSON {
		t.Fatalf("suffix JSON data = %#v, %v", data, err)
	}
	if _, err := dataFromPayload("application/json", []byte("{")); err == nil {
		t.Fatal("invalid JSON data error = nil")
	}
	if data, err := dataFromPayload("text/plain", []byte("text")); err != nil || data.Kind() != cloudevents.DataText {
		t.Fatalf("text data = %#v, %v", data, err)
	}
	if _, err := dataFromPayload("text/plain", []byte{0xff}); err == nil {
		t.Fatal("invalid text data error = nil")
	}
	if data, err := dataFromPayload("application/octet-stream", []byte{0xff}); err != nil || data.Kind() != cloudevents.DataBinary {
		t.Fatalf("binary data = %#v, %v", data, err)
	}

	extensions := map[string]cloudevents.Attribute{}
	if err := putStringExtension(extensions, "value", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := putStringExtension(extensions, "bad", "\n"); !errors.Is(err, ErrInvalidAdapterInput) {
		t.Fatalf("invalid extension error = %v", err)
	}
	event := helperEvent(t, extensions, false)
	if value, err := mappedString(event, "missing", "retained", false); err != nil || value != "retained" {
		t.Fatalf("absent mapped value = %q, %v", value, err)
	}
	if value, err := mappedString(event, "value", "ok", false); err != nil || value != "ok" {
		t.Fatalf("matching retained value = %q, %v", value, err)
	}
	if _, err := mappedString(event, "value", "other", true); !errors.Is(err, ErrMetadataCollision) {
		t.Fatalf("mapped collision error = %v", err)
	}
	invalidMapped := helperEvent(t, map[string]cloudevents.Attribute{"value": cloudevents.NewBooleanAttribute(true)}, false)
	if _, err := mappedString(invalidMapped, "value", "", true); !errors.Is(err, ErrInvalidAdapterInput) {
		t.Fatalf("invalid mapped value error = %v", err)
	}
	if _, err := mappedString(event, "value", "", false); !errors.Is(err, ErrUntrustedMetadata) {
		t.Fatalf("mapped trust error = %v", err)
	}
	if value, err := mappedString(event, "value", "", true); err != nil || value != "ok" {
		t.Fatalf("trusted mapped value = %q, %v", value, err)
	}

	if cloneStrings(nil) != nil || cloneTimePointer(nil) != nil || cloneBytes(nil) != nil {
		t.Fatal("nil clone changed presence")
	}
	values := map[string]string{"a": "b"}
	if cloned := cloneStrings(values); cloned["a"] != "b" {
		t.Fatalf("string clone = %#v", cloned)
	}
	now := time.Now()
	if cloned := cloneTimePointer(&now); cloned == &now || !cloned.Equal(now) {
		t.Fatalf("time clone = %v", cloned)
	}

	message := job.Message{Body: []byte("x")}
	if cloned := cloneJob(message); string(cloned.Body) != "x" || cloned.Metadata != nil {
		t.Fatalf("job clone without metadata = %#v", cloned)
	}
	enqueuedAt := now
	message.Metadata = &job.Metadata{
		EnqueuedAt: &enqueuedAt, Tags: map[string]string{"a": "b"},
		Correlation:  map[string]string{"correlationid": "c"},
		TraceContext: map[string]string{"traceparent": "p"},
	}
	clonedMessage := cloneJob(message)
	message.Metadata.Tags["a"] = "changed"
	if clonedMessage.Metadata.Tags["a"] != "b" || clonedMessage.Metadata.EnqueuedAt == message.Metadata.EnqueuedAt {
		t.Fatalf("job metadata clone = %#v", clonedMessage.Metadata)
	}

	if got, err := queueExtensions(nil); err != nil || len(got) != 0 {
		t.Fatalf("nil queue extensions = %#v, %v", got, err)
	}
	metadata := &job.Metadata{
		TenantID:     "tenant-a",
		Correlation:  map[string]string{"correlationid": "c", "requestid": "r", "causationid": "p"},
		TraceContext: map[string]string{"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "tracestate": "vendor=value"},
	}
	queueValues, err := queueExtensions(metadata)
	if err != nil || len(queueValues) != 6 {
		t.Fatalf("queue extensions = %#v, %v", queueValues, err)
	}
	metadata.TenantID = "bad\nvalue"
	if _, err := queueExtensions(metadata); !errors.Is(err, ErrInvalidAdapterInput) {
		t.Fatalf("invalid queue extension error = %v", err)
	}

	carrier := metadataCarrier{"z": "1", "a": "2"}
	if keys := carrier.Keys(); len(keys) != 2 || keys[0] != "a" || keys[1] != "z" {
		t.Fatalf("carrier keys = %v", keys)
	}
	var pointer *int
	if !interfaceIsNil(nil) || !interfaceIsNil(pointer) || interfaceIsNil(1) {
		t.Fatal("interface nil classification failed")
	}
	if !isJSONContentType("application/problem+json") || isJSONContentType(";") || isJSONContentType("text/plain") {
		t.Fatal("JSON content type classification failed")
	}
}

func TestAdapterHelpersReportOptionalAndExtensionLoss(t *testing.T) {
	t.Parallel()

	now := time.Now()
	attribute, _ := cloudevents.NewStringAttribute("value")
	data, _ := cloudevents.NewJSONData([]byte(`{}`))
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "1", Source: "/", Type: "type", DataContentType: "application/json",
		DataSchema: "https://schemas.example/schema", Subject: "subject", Time: &now,
		Extensions: map[string]cloudevents.Attribute{"owned": attribute, "lost": attribute},
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	report := Report{}
	appendOptionalContextLosses(event, &report, "target")
	appendExtensionLosses(event, &report, "target", map[string]struct{}{"owned": {}})
	if len(report.Losses) != 4 {
		t.Fatalf("losses = %#v", report.Losses)
	}
	if rebuilt, err := rebuildEvent(event, event.Extensions()); err != nil {
		t.Fatal(err)
	} else if occurredAt, present := rebuilt.Time(); !present || !occurredAt.Equal(now) {
		t.Fatalf("rebuilt time = %v, %v", occurredAt, present)
	}
	if got := timePointer(now); got == nil || !got.Equal(now) {
		t.Fatalf("time pointer = %v", got)
	}
	if kind, err := parseWorkflowEventType(workflowEventType(1)); err != nil || kind != 1 {
		t.Fatalf("workflow type = %v, %v", kind, err)
	}
	for _, value := range []string{"other", "golib.workflow.history.bad"} {
		if _, err := parseWorkflowEventType(value); !errors.Is(err, ErrInvalidAdapterInput) {
			t.Fatalf("workflow type %q error = %v", value, err)
		}
	}
}

func helperEvent(t *testing.T, extensions map[string]cloudevents.Attribute, withTime bool) cloudevents.Event {
	t.Helper()
	var occurredAt *time.Time
	if withTime {
		now := time.Now()
		occurredAt = &now
	}
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "1", Source: "/", Type: "type", Time: occurredAt, Extensions: extensions,
	}, cloudevents.Data{})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestStringExtensionAndAttributeEqualityBoundaries(t *testing.T) {
	t.Parallel()

	stringValue, _ := cloudevents.NewStringAttribute("value")
	emptyValue, _ := cloudevents.NewStringAttribute("")
	event := helperEvent(t, map[string]cloudevents.Attribute{
		"string": stringValue, "empty": emptyValue, "boolean": cloudevents.NewBooleanAttribute(true),
	}, false)
	if value, present, err := stringExtension(event, "string"); err != nil || !present || value != "value" {
		t.Fatalf("string extension = %q, %v, %v", value, present, err)
	}
	if _, present, err := stringExtension(event, "missing"); err != nil || present {
		t.Fatalf("missing extension = %v, %v", present, err)
	}
	for _, name := range []string{"empty", "boolean"} {
		if _, _, err := stringExtension(event, name); !errors.Is(err, ErrInvalidAdapterInput) {
			t.Fatalf("extension %s error = %v", name, err)
		}
	}
	if !attributesEqual(stringValue, stringValue) || attributesEqual(stringValue, cloudevents.NewBooleanAttribute(true)) ||
		attributesEqual(cloudevents.NewBinaryAttribute([]byte("a")), cloudevents.NewBinaryAttribute([]byte("b"))) {
		t.Fatal("attribute equality classification failed")
	}
	if _, err := rebuildEvent(cloudevents.Event{}, nil); err == nil {
		t.Fatal("invalid rebuilt event error = nil")
	}
}

func TestVerifyQueueExtensionsDetectsMalformedAndCollidingValues(t *testing.T) {
	t.Parallel()

	metadata := &job.Metadata{TenantID: "tenant-a"}
	tenant, _ := cloudevents.NewStringAttribute("tenant-a")
	if err := verifyQueueExtensions(helperEvent(t, map[string]cloudevents.Attribute{"tenantid": tenant}, false), metadata); err != nil {
		t.Fatal(err)
	}
	different, _ := cloudevents.NewStringAttribute("tenant-b")
	if err := verifyQueueExtensions(helperEvent(t, map[string]cloudevents.Attribute{"tenantid": different}, false), metadata); !errors.Is(err, ErrMetadataCollision) {
		t.Fatalf("queue collision error = %v", err)
	}
	if err := verifyQueueExtensions(helperEvent(t, map[string]cloudevents.Attribute{"tenantid": cloudevents.NewBooleanAttribute(true)}, false), metadata); !errors.Is(err, ErrInvalidAdapterInput) {
		t.Fatalf("queue invalid extension error = %v", err)
	}
}
