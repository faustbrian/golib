package acl

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
		Limits:            Limits{MaxEntries: 10, MaxMatches: 5},
		Entries: []EntryDocument{{
			ID: "reader", SubjectKind: authorization.SubjectUser,
			SubjectID: "user-1", Action: "read", ResourceType: "document",
			Effect: EffectAllow,
		}, {
			ID: "other-deny", SubjectKind: authorization.SubjectUser,
			SubjectID: "user-2", Action: "read", ResourceType: "document",
			Effect: EffectDeny,
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
		Action:  "read", Resource: authorization.Resource{Type: "document", ID: "doc-1"},
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
		"unknown field": {encoded: `{"version":1,"entries":[],"extra":true}`, want: ErrInvalidDocument},
		"trailing":      {encoded: `{"version":1,"entries":[]} {}`, want: ErrInvalidDocument},
		"version":       {encoded: `{"version":2,"entries":[]}`, want: ErrUnsupportedDocumentVersion},
		"effect": {encoded: `{"version":1,"entries":[{"id":"x","subject_kind":"user","subject_id":"u","action":"read","resource_type":"doc","effect":"maybe"}]}`,
			want: ErrInvalidDocument},
		"entry": {encoded: `{"version":1,"entries":[{"id":"x","subject_kind":"user","subject_id":"u","action":"","resource_type":"doc","effect":"allow"}]}`,
			want: ErrInvalidEntry},
		"limit": {encoded: `{"version":1,"limits":{"max_entries":1},"entries":[{"id":"x","subject_kind":"user","subject_id":"u","action":"read","resource_type":"doc","effect":"allow"},{"id":"y","subject_kind":"user","subject_id":"u","action":"write","resource_type":"doc","effect":"deny"}]}`,
			want: ErrEntryLimitExceeded},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeDocument([]byte(tt.encoded)); !errors.Is(err, tt.want) {
				t.Errorf("DecodeDocument() error = %v, want %v", err, tt.want)
			}
		})
	}

	invalid := Document{Version: 2, Entries: []EntryDocument{}}
	if _, err := EncodeDocument(invalid); !errors.Is(err, ErrUnsupportedDocumentVersion) {
		t.Errorf("EncodeDocument(invalid) error = %v, want version error", err)
	}
	if _, err := DecodeDocument(bytes.Repeat([]byte{'x'}, maxEncodedDocumentBytes+1)); !errors.Is(err, ErrDocumentLimitExceeded) {
		t.Errorf("DecodeDocument(oversized) error = %v, want ErrDocumentLimitExceeded", err)
	}
	if _, err := DecodeDocument(bytes.Repeat([]byte{'x'}, maxEncodedDocumentBytes)); errors.Is(err, ErrDocumentLimitExceeded) {
		t.Errorf("DecodeDocument(at limit) error = %v, do not want ErrDocumentLimitExceeded", err)
	}
	atLimit := Document{Version: DocumentVersion, Entries: []EntryDocument{{
		ID: "entry", SubjectKind: authorization.SubjectUser, SubjectID: "user",
		Action: "x", ResourceType: "document", Effect: EffectAllow,
	}}}
	baseline, err := json.MarshalIndent(atLimit, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent(at limit baseline) error = %v", err)
	}
	atLimit.Entries[0].Action = authorization.Action(strings.Repeat("x", maxEncodedDocumentBytes-len(baseline)+1))
	encoded, err := EncodeDocument(atLimit)
	if err != nil {
		t.Fatalf("EncodeDocument(at limit) error = %v", err)
	}
	if len(encoded) != maxEncodedDocumentBytes {
		t.Errorf("len(EncodeDocument(at limit)) = %d, want %d", len(encoded), maxEncodedDocumentBytes)
	}
	oversized := Document{Version: DocumentVersion, Entries: []EntryDocument{{
		ID: "entry", SubjectKind: authorization.SubjectUser, SubjectID: "user",
		Action:       authorization.Action(strings.Repeat("x", maxEncodedDocumentBytes)),
		ResourceType: "document", Effect: EffectAllow,
	}}}
	if _, err := EncodeDocument(oversized); !errors.Is(err, ErrDocumentLimitExceeded) {
		t.Errorf("EncodeDocument(oversized) error = %v, want ErrDocumentLimitExceeded", err)
	}
}
