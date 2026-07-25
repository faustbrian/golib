package rbac

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

const DocumentVersion uint64 = 1
const maxEncodedDocumentBytes = 1 << 20

const (
	EffectAllow = "allow"
	EffectDeny  = "deny"
)

var (
	ErrInvalidDocument            = errors.New("invalid RBAC document")
	ErrUnsupportedDocumentVersion = errors.New("unsupported RBAC document version")
	ErrDocumentLimitExceeded      = errors.New("RBAC document size limit exceeded")
)

type RoleDocument struct {
	ID      RoleID                 `json:"id"`
	Tenant  authorization.TenantID `json:"tenant,omitempty"`
	Parents []RoleID               `json:"parents,omitempty"`
}

type PermissionDocument struct {
	ID           authorization.PolicyID     `json:"id"`
	RoleID       RoleID                     `json:"role_id"`
	Tenant       authorization.TenantID     `json:"tenant,omitempty"`
	Action       authorization.Action       `json:"action"`
	ResourceType authorization.ResourceType `json:"resource_type"`
	ResourceID   authorization.ResourceID   `json:"resource_id,omitempty"`
	Effect       string                     `json:"effect"`
	Priority     int                        `json:"priority,omitempty"`
}

type AssignmentDocument struct {
	ID          authorization.PolicyID    `json:"id"`
	SubjectKind authorization.SubjectKind `json:"subject_kind"`
	SubjectID   authorization.SubjectID   `json:"subject_id"`
	RoleID      RoleID                    `json:"role_id"`
	Tenant      authorization.TenantID    `json:"tenant,omitempty"`
}

type Document struct {
	Version           uint64               `json:"version"`
	GlobalInheritance bool                 `json:"global_inheritance,omitempty"`
	Limits            Limits               `json:"limits,omitempty"`
	Roles             []RoleDocument       `json:"roles"`
	Permissions       []PermissionDocument `json:"permissions"`
	Assignments       []AssignmentDocument `json:"assignments"`
}

func (document Document) Build() (*Evaluator, error) {
	if document.Version != DocumentVersion {
		return nil, ErrUnsupportedDocumentVersion
	}
	roles := make([]Role, len(document.Roles))
	for index, role := range document.Roles {
		roles[index] = Role(role)
	}
	permissions := make([]Permission, len(document.Permissions))
	for index, permission := range document.Permissions {
		effect, err := parseDocumentEffect(permission.Effect)
		if err != nil {
			return nil, fmt.Errorf("permission %d: %w", index, err)
		}
		permissions[index] = Permission{
			ID: permission.ID, RoleID: permission.RoleID, Tenant: permission.Tenant,
			Action: permission.Action, ResourceType: permission.ResourceType,
			ResourceID: permission.ResourceID, Effect: effect,
			Priority: permission.Priority,
		}
	}
	assignments := make([]Assignment, len(document.Assignments))
	for index, assignment := range document.Assignments {
		assignments[index] = Assignment{
			ID: assignment.ID,
			Subject: authorization.Subject{
				Kind: assignment.SubjectKind,
				ID:   assignment.SubjectID,
			},
			RoleID: assignment.RoleID,
			Tenant: assignment.Tenant,
		}
	}
	options := []Option{WithLimits(document.Limits)}
	if document.GlobalInheritance {
		options = append(options, WithGlobalInheritance())
	}
	return New(roles, permissions, assignments, options...)
}

func DecodeDocument(encoded []byte) (*Evaluator, error) {
	if len(encoded) > maxEncodedDocumentBytes {
		return nil, ErrDocumentLimitExceeded
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDocument, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidDocument
	}
	return document.Build()
}

func EncodeDocument(document Document) ([]byte, error) {
	if _, err := document.Build(); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if len(encoded) > maxEncodedDocumentBytes {
		return nil, ErrDocumentLimitExceeded
	}
	return encoded, err
}

type Decoder struct{}

func (Decoder) Decode(document json.RawMessage) (authorization.Evaluator, error) {
	return DecodeDocument(document)
}

func parseDocumentEffect(effect string) (authorization.Outcome, error) {
	switch effect {
	case EffectAllow:
		return authorization.Allow, nil
	case EffectDeny:
		return authorization.Deny, nil
	default:
		return authorization.NotApplicable, ErrInvalidDocument
	}
}
