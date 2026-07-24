package acl

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
	ErrInvalidDocument            = errors.New("invalid ACL document")
	ErrUnsupportedDocumentVersion = errors.New("unsupported ACL document version")
	ErrDocumentLimitExceeded      = errors.New("ACL document size limit exceeded")
)

type EntryDocument struct {
	ID           EntryID                    `json:"id"`
	SubjectKind  authorization.SubjectKind  `json:"subject_kind"`
	SubjectID    authorization.SubjectID    `json:"subject_id"`
	Action       authorization.Action       `json:"action"`
	ResourceType authorization.ResourceType `json:"resource_type"`
	ResourceID   authorization.ResourceID   `json:"resource_id,omitempty"`
	Tenant       authorization.TenantID     `json:"tenant,omitempty"`
	Effect       string                     `json:"effect"`
}

type Document struct {
	Version           uint64          `json:"version"`
	GlobalInheritance bool            `json:"global_inheritance,omitempty"`
	Limits            Limits          `json:"limits,omitempty"`
	Entries           []EntryDocument `json:"entries"`
}

func (document Document) Build() (*Evaluator, error) {
	if document.Version != DocumentVersion {
		return nil, ErrUnsupportedDocumentVersion
	}
	entries := make([]Entry, len(document.Entries))
	for index, entry := range document.Entries {
		effect, err := parseEffect(entry.Effect)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", index, err)
		}
		entries[index] = Entry{
			ID: entry.ID,
			Subject: authorization.Subject{
				Kind: entry.SubjectKind,
				ID:   entry.SubjectID,
			},
			Action:       entry.Action,
			ResourceType: entry.ResourceType,
			ResourceID:   entry.ResourceID,
			Tenant:       entry.Tenant,
			Effect:       effect,
		}
	}
	options := []Option{WithLimits(document.Limits)}
	if document.GlobalInheritance {
		options = append(options, WithGlobalInheritance())
	}
	return New(entries, options...)
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

func parseEffect(effect string) (authorization.Outcome, error) {
	switch effect {
	case EffectAllow:
		return authorization.Allow, nil
	case EffectDeny:
		return authorization.Deny, nil
	default:
		return authorization.NotApplicable, ErrInvalidDocument
	}
}
