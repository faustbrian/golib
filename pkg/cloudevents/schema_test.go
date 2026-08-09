package cloudevents

import (
	"context"
	"errors"
	"testing"
)

type recordingSchemaValidator struct {
	called      bool
	uri         string
	contentType string
	data        []byte
	err         error
}

func (validator *recordingSchemaValidator) Validate(
	_ context.Context,
	uri string,
	contentType string,
	data []byte,
) error {
	validator.called = true
	validator.uri = uri
	validator.contentType = contentType
	validator.data = data
	return validator.err
}

func TestValidateSchemaIsExplicitAndOwnsValidatorInput(t *testing.T) {
	t.Parallel()

	data, err := NewJSONData([]byte(`{"valid":true}`))
	if err != nil {
		t.Fatalf("create data: %v", err)
	}
	event, err := NewEvent(Attributes{
		ID:              "1",
		Source:          "/source",
		Type:            "example",
		DataContentType: "application/json",
		DataSchema:      "https://schemas.example/event.json",
	}, data)
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	validator := &recordingSchemaValidator{}
	if err := ValidateSchema(context.Background(), event, validator); err != nil {
		t.Fatalf("ValidateSchema() error = %v", err)
	}
	if !validator.called || validator.uri != "https://schemas.example/event.json" ||
		validator.contentType != "application/json" || string(validator.data) != `{"valid":true}` {
		t.Fatalf("validator call = %#v", validator)
	}
	validator.data[0] = 'X'
	if event.Data().Bytes()[0] == 'X' {
		t.Fatal("validator data aliases event data")
	}

	withoutSchema, err := NewEvent(Attributes{ID: "2", Source: "/source", Type: "example"}, data)
	if err != nil {
		t.Fatalf("create schemaless event: %v", err)
	}
	unused := &recordingSchemaValidator{}
	if err := ValidateSchema(context.Background(), withoutSchema, unused); !errors.Is(err, ErrSchemaRequired) {
		t.Fatalf("schemaless validation error = %v", err)
	}
	if unused.called {
		t.Fatal("validator was called without an explicit schema URI")
	}
}

func TestValidateSchemaRejectsNilContextPrecisely(t *testing.T) {
	t.Parallel()

	validator := &recordingSchemaValidator{}
	//lint:ignore SA1012 A nil context is the public contract under test.
	//nolint:staticcheck // A nil context is the public contract under test.
	if err := ValidateSchema(nil, Event{}, validator); err == nil || errors.Is(err, context.Canceled) ||
		err.Error() != "cloudevents: context required" {
		t.Fatalf("nil context error = %v, want context-required diagnostic", err)
	}
	if validator.called {
		t.Fatal("validator called")
	}
}

func TestValidateSchemaRejectsTypedNilValidatorWithoutPanicking(t *testing.T) {
	t.Parallel()

	data, err := NewJSONData([]byte(`null`))
	if err != nil {
		t.Fatalf("create data: %v", err)
	}
	event, err := NewEvent(Attributes{
		ID: "1", Source: "/source", Type: "example",
		DataSchema: "https://schemas.example/event.json",
	}, data)
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	var validator *recordingSchemaValidator
	if err := ValidateSchema(context.Background(), event, validator); !errors.Is(err, ErrSchemaValidatorRequired) {
		t.Fatalf("typed-nil validator error = %v, want ErrSchemaValidatorRequired", err)
	}
}
