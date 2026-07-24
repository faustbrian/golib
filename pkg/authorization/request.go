package authorization

import (
	"errors"
	"fmt"
	"time"
)

var ErrInvalidRequest = errors.New("invalid authorization request")

// ValidationError identifies an invalid public input without including its
// potentially sensitive value.
type ValidationError struct {
	Field string
}

func (validationError *ValidationError) Error() string {
	return fmt.Sprintf("%s: %v", validationError.Field, ErrInvalidRequest)
}

func (validationError *ValidationError) Unwrap() error {
	return ErrInvalidRequest
}

// SubjectKind identifies the application-defined kind of principal.
type SubjectKind string

const (
	SubjectUser           SubjectKind = "user"
	SubjectServiceAccount SubjectKind = "service-account"
	SubjectAPIKey         SubjectKind = "api-key"
	SubjectGroup          SubjectKind = "group"
)

type SubjectID string
type Action string
type ResourceType string
type ResourceID string
type TenantID string
type AttributeName string
type Attributes map[AttributeName]Value

// Subject identifies the principal making an authorization request.
type Subject struct {
	Kind       SubjectKind
	ID         SubjectID
	Groups     []SubjectID
	Attributes Attributes
}

// Resource identifies either a resource type or a concrete resource instance.
// An empty ID intentionally represents the entire resource type.
type Resource struct {
	Type       ResourceType
	ID         ResourceID
	Attributes Attributes
}

// Environment contains deterministic request-scoped evaluation inputs.
type Environment struct {
	Time       time.Time
	Attributes Attributes
}

// Request contains the stable, typed inputs shared by every policy model. An
// empty tenant denotes an explicitly global request scope.
type Request struct {
	Subject     Subject
	Action      Action
	Resource    Resource
	Tenant      TenantID
	Environment Environment
	Attributes  Attributes
}

// Validate rejects incomplete requests before any policy is evaluated.
func (request Request) Validate() error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "subject.kind", value: string(request.Subject.Kind)},
		{name: "subject.id", value: string(request.Subject.ID)},
		{name: "action", value: string(request.Action)},
		{name: "resource.type", value: string(request.Resource.Type)},
	} {
		if field.value == "" {
			return &ValidationError{Field: field.name}
		}
	}

	for index, groupID := range request.Subject.Groups {
		if groupID == "" {
			return &ValidationError{Field: fmt.Sprintf("subject.groups[%d]", index)}
		}
	}

	return nil
}
