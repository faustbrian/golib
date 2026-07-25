package rbac

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

func TestDocumentDecodeEncodeAndEvaluate(t *testing.T) {
	t.Parallel()

	document := Document{
		Version:           DocumentVersion,
		GlobalInheritance: true,
		Roles:             []RoleDocument{{ID: "reader"}},
		Permissions: []PermissionDocument{{
			ID: "read", RoleID: "reader", Action: "read",
			ResourceType: "document", Effect: EffectAllow,
		}, {
			ID: "deny-other", RoleID: "reader", Action: "read",
			ResourceType: "document", ResourceID: "other", Effect: EffectDeny,
		}},
		Assignments: []AssignmentDocument{{
			ID: "assigned", SubjectKind: authorization.SubjectUser,
			SubjectID: "user-1", RoleID: "reader",
		}},
	}
	encoded, err := EncodeDocument(document)
	if err != nil {
		t.Fatalf("EncodeDocument() error = %v", err)
	}
	if !json.Valid(encoded) {
		t.Fatalf("EncodeDocument() = %s", encoded)
	}
	evaluator, err := DecodeDocument(encoded)
	if err != nil {
		t.Fatalf("DecodeDocument() error = %v", err)
	}
	decision, err := evaluator.Evaluate(context.Background(), authorization.Request{
		Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
		Action:  "read", Resource: authorization.Resource{Type: "document"},
		Tenant: "tenant-1",
	})
	if err != nil || decision.Outcome != authorization.Allow {
		t.Fatalf("Evaluate() = (%+v, %v), want allow", decision, err)
	}
	compiled, err := (Decoder{}).Decode(encoded)
	if err != nil || compiled == nil {
		t.Fatalf("Decoder.Decode() = (%v, %v), want evaluator", compiled, err)
	}
}

func TestDocumentRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		encoded string
		want    error
	}{
		"malformed":     {encoded: `{`, want: ErrInvalidDocument},
		"unknown field": {encoded: `{"version":1,"roles":[],"permissions":[],"assignments":[],"extra":true}`, want: ErrInvalidDocument},
		"trailing":      {encoded: `{"version":1,"roles":[],"permissions":[],"assignments":[]} {}`, want: ErrInvalidDocument},
		"version":       {encoded: `{"version":2,"roles":[],"permissions":[],"assignments":[]}`, want: ErrUnsupportedDocumentVersion},
		"effect": {encoded: `{"version":1,"roles":[{"id":"r"}],"permissions":[{"id":"p","role_id":"r","action":"read","resource_type":"doc","effect":"maybe"}],"assignments":[]}`,
			want: ErrInvalidDocument},
		"role": {encoded: `{"version":1,"roles":[{"id":""}],"permissions":[],"assignments":[]}`,
			want: ErrInvalidRole},
		"limit": {encoded: `{"version":1,"limits":{"max_roles":1},"roles":[{"id":"a"},{"id":"b"}],"permissions":[],"assignments":[]}`,
			want: ErrRoleLimitExceeded},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeDocument([]byte(tt.encoded)); !errors.Is(err, tt.want) {
				t.Errorf("DecodeDocument() error = %v, want %v", err, tt.want)
			}
		})
	}
	if _, err := EncodeDocument(Document{Version: 2}); !errors.Is(err, ErrUnsupportedDocumentVersion) {
		t.Errorf("EncodeDocument(invalid) error = %v, want version error", err)
	}
	if _, err := DecodeDocument(bytes.Repeat([]byte{'x'}, maxEncodedDocumentBytes+1)); !errors.Is(err, ErrDocumentLimitExceeded) {
		t.Errorf("DecodeDocument(oversized) error = %v, want ErrDocumentLimitExceeded", err)
	}
	if _, err := DecodeDocument(bytes.Repeat([]byte{'x'}, maxEncodedDocumentBytes)); errors.Is(err, ErrDocumentLimitExceeded) {
		t.Errorf("DecodeDocument(at limit) error = %v, do not want ErrDocumentLimitExceeded", err)
	}
	atLimit := Document{Version: DocumentVersion, Roles: []RoleDocument{{ID: "x"}}}
	baseline, err := json.MarshalIndent(atLimit, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent(at limit baseline) error = %v", err)
	}
	atLimit.Roles[0].ID = RoleID(strings.Repeat("x", maxEncodedDocumentBytes-len(baseline)+1))
	encoded, err := EncodeDocument(atLimit)
	if err != nil {
		t.Fatalf("EncodeDocument(at limit) error = %v", err)
	}
	if len(encoded) != maxEncodedDocumentBytes {
		t.Errorf("len(EncodeDocument(at limit)) = %d, want %d", len(encoded), maxEncodedDocumentBytes)
	}
	oversized := Document{
		Version: DocumentVersion,
		Roles:   []RoleDocument{{ID: RoleID(strings.Repeat("x", maxEncodedDocumentBytes))}},
	}
	if _, err := EncodeDocument(oversized); !errors.Is(err, ErrDocumentLimitExceeded) {
		t.Errorf("EncodeDocument(oversized) error = %v, want ErrDocumentLimitExceeded", err)
	}
}
