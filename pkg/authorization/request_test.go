package authorization

import (
	"errors"
	"testing"
)

func TestRequestValidate(t *testing.T) {
	t.Parallel()

	valid := Request{
		Subject:  Subject{Kind: SubjectUser, ID: "user-123"},
		Action:   "document.read",
		Resource: Resource{Type: "document", ID: "document-456"},
		Tenant:   "tenant-789",
	}

	tests := map[string]struct {
		mutate func(*Request)
		field  string
	}{
		"subject kind": {
			mutate: func(request *Request) { request.Subject.Kind = "" },
			field:  "subject.kind",
		},
		"subject id": {
			mutate: func(request *Request) { request.Subject.ID = "" },
			field:  "subject.id",
		},
		"action": {
			mutate: func(request *Request) { request.Action = "" },
			field:  "action",
		},
		"resource type": {
			mutate: func(request *Request) { request.Resource.Type = "" },
			field:  "resource.type",
		},
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Request.Validate() error = %v", err)
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			request := valid
			tt.mutate(&request)

			err := request.Validate()
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Request.Validate() error = %v, want ErrInvalidRequest", err)
			}

			var validationError *ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Request.Validate() error type = %T, want *ValidationError", err)
			}

			if validationError.Field != tt.field {
				t.Errorf("ValidationError.Field = %q, want %q", validationError.Field, tt.field)
			}
			if validationError.Error() == "" {
				t.Error("ValidationError.Error() is empty")
			}
		})
	}
}

func TestRequestValidateAllowsResourceTypeAndGlobalScope(t *testing.T) {
	t.Parallel()

	request := Request{
		Subject:  Subject{Kind: SubjectServiceAccount, ID: "billing-worker"},
		Action:   "invoice.list",
		Resource: Resource{Type: "invoice"},
	}

	if err := request.Validate(); err != nil {
		t.Fatalf("Request.Validate() error = %v", err)
	}
}

func TestRequestValidateRejectsEmptyGroupID(t *testing.T) {
	t.Parallel()

	request := Request{
		Subject: Subject{
			Kind:   SubjectUser,
			ID:     "user-1",
			Groups: []SubjectID{"editors", ""},
		},
		Action:   "document.read",
		Resource: Resource{Type: "document"},
	}

	err := request.Validate()
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Request.Validate() error = %v, want *ValidationError", err)
	}
	if validationError.Field != "subject.groups[1]" {
		t.Errorf("ValidationError.Field = %q, want subject.groups[1]", validationError.Field)
	}
}
