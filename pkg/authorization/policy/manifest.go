package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

var (
	ErrInvalidManifest       = errors.New("invalid policy manifest")
	ErrDuplicateRecord       = errors.New("duplicate policy record")
	ErrManifestLimitExceeded = errors.New("policy manifest size limit exceeded")
)

const maxEncodedManifestBytes = 16 << 20

type Format string

const FormatV1 Format = "authorization.policy/v1"

type Algorithm string

const (
	AlgorithmDenyOverrides   Algorithm = "deny-overrides"
	AlgorithmAllowOverrides  Algorithm = "allow-overrides"
	AlgorithmFirstApplicable Algorithm = "first-applicable"
	AlgorithmPriorityOrder   Algorithm = "priority-order"
)

type Model string

const (
	ModelACL       Model = "acl"
	ModelRBAC      Model = "rbac"
	ModelABAC      Model = "abac"
	ModelComposite Model = "composite"
)

type Record struct {
	ID          authorization.PolicyID `json:"id"`
	Revision    authorization.Revision `json:"revision"`
	Model       Model                  `json:"model"`
	Priority    int                    `json:"priority,omitempty"`
	ActiveFrom  *time.Time             `json:"active_from,omitempty"`
	ActiveUntil *time.Time             `json:"active_until,omitempty"`
	Metadata    map[string]string      `json:"metadata,omitempty"`
	Document    json.RawMessage        `json:"document"`
}

type Manifest struct {
	Format    Format                 `json:"format"`
	Revision  authorization.Revision `json:"revision"`
	Algorithm Algorithm              `json:"algorithm"`
	Policies  []Record               `json:"policies"`
}

func (manifest Manifest) Validate() error {
	if manifest.Format != FormatV1 || manifest.Revision == 0 ||
		!manifest.Algorithm.valid() {
		return ErrInvalidManifest
	}

	policyIDs := make(map[authorization.PolicyID]struct{}, len(manifest.Policies))
	for index, record := range manifest.Policies {
		if record.ID == "" || record.Revision == 0 || !record.Model.valid() ||
			!validDocument(record.Document) {
			return fmt.Errorf("policy %d: %w", index, ErrInvalidManifest)
		}
		if _, exists := policyIDs[record.ID]; exists {
			return fmt.Errorf("policy %q: %w", record.ID, ErrDuplicateRecord)
		}
		if record.ActiveFrom != nil && record.ActiveUntil != nil &&
			!record.ActiveUntil.After(*record.ActiveFrom) {
			return fmt.Errorf("policy %q activation: %w", record.ID, ErrInvalidManifest)
		}
		policyIDs[record.ID] = struct{}{}
	}

	return nil
}

func Encode(manifest Manifest) ([]byte, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}

	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	if len(encoded)+1 > maxEncodedManifestBytes {
		return nil, ErrManifestLimitExceeded
	}
	return append(encoded, '\n'), nil
}

func Decode(encoded []byte) (Manifest, error) {
	if len(encoded) > maxEncodedManifestBytes {
		return Manifest{}, ErrManifestLimitExceeded
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w: %v", ErrInvalidManifest, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Manifest{}, fmt.Errorf("decode manifest trailing data: %w", ErrInvalidManifest)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (algorithm Algorithm) valid() bool {
	return algorithm == AlgorithmDenyOverrides ||
		algorithm == AlgorithmAllowOverrides ||
		algorithm == AlgorithmFirstApplicable ||
		algorithm == AlgorithmPriorityOrder
}

func (model Model) valid() bool {
	return model == ModelACL || model == ModelRBAC ||
		model == ModelABAC || model == ModelComposite
}

func validDocument(document json.RawMessage) bool {
	trimmed := bytes.TrimSpace(document)
	return len(trimmed) >= 2 && trimmed[0] == '{' && json.Valid(trimmed)
}

// Repository is the storage-neutral optimistic-concurrency contract for
// portable policy manifests.
type Repository interface {
	Load(context.Context) (Manifest, error)
	Update(context.Context, authorization.Revision, Manifest) (Manifest, error)
}
