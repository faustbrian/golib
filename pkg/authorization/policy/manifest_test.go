package policy_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/authorization/policy"
)

func TestManifestRoundTripsStrictHumanReadableJSON(t *testing.T) {
	t.Parallel()

	activeFrom := time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC)
	activeUntil := activeFrom.Add(24 * time.Hour)
	manifest := policy.Manifest{
		Format:    policy.FormatV1,
		Revision:  7,
		Algorithm: policy.AlgorithmDenyOverrides,
		Policies: []policy.Record{
			{
				ID:          "documents",
				Revision:    3,
				Model:       policy.ModelACL,
				Priority:    10,
				ActiveFrom:  &activeFrom,
				ActiveUntil: &activeUntil,
				Metadata:    map[string]string{"owner": "security"},
				Document:    json.RawMessage(`{"entries":[]}`),
			},
		},
	}

	encoded, err := policy.Encode(manifest)
	if err != nil {
		t.Fatalf("policy.Encode() error = %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"format": "authorization.policy/v1"`)) {
		t.Errorf("policy.Encode() output is not readable v1 JSON: %s", encoded)
	}
	if encoded[len(encoded)-1] != '\n' {
		t.Error("policy.Encode() output does not end in a newline")
	}

	decoded, err := policy.Decode(encoded)
	if err != nil {
		t.Fatalf("policy.Decode() error = %v", err)
	}
	reencoded, err := policy.Encode(decoded)
	if err != nil {
		t.Fatalf("policy.Encode(decoded) error = %v", err)
	}
	if !bytes.Equal(reencoded, encoded) {
		t.Errorf("re-encoded manifest changed:\n%s\nwant:\n%s", reencoded, encoded)
	}
}

func TestManifestValidationAndStrictDecode(t *testing.T) {
	t.Parallel()

	valid := policy.Manifest{
		Format:    policy.FormatV1,
		Revision:  1,
		Algorithm: policy.AlgorithmDenyOverrides,
		Policies: []policy.Record{
			{
				ID:       "policy",
				Revision: 1,
				Model:    policy.ModelACL,
				Document: json.RawMessage(`{"entries":[]}`),
			},
		},
	}

	tests := map[string]func(*policy.Manifest){
		"format":    func(manifest *policy.Manifest) { manifest.Format = "unknown" },
		"revision":  func(manifest *policy.Manifest) { manifest.Revision = 0 },
		"algorithm": func(manifest *policy.Manifest) { manifest.Algorithm = "unknown" },
		"policy id": func(manifest *policy.Manifest) { manifest.Policies[0].ID = "" },
		"policy revision": func(manifest *policy.Manifest) {
			manifest.Policies[0].Revision = 0
		},
		"model":    func(manifest *policy.Manifest) { manifest.Policies[0].Model = "unknown" },
		"document": func(manifest *policy.Manifest) { manifest.Policies[0].Document = nil },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			manifest := valid
			manifest.Policies = append([]policy.Record(nil), valid.Policies...)
			mutate(&manifest)
			if err := manifest.Validate(); !errors.Is(err, policy.ErrInvalidManifest) {
				t.Errorf("Manifest.Validate() error = %v, want ErrInvalidManifest", err)
			}
		})
	}

	duplicate := valid
	duplicate.Policies = append(duplicate.Policies, duplicate.Policies[0])
	if err := duplicate.Validate(); !errors.Is(err, policy.ErrDuplicateRecord) {
		t.Errorf("duplicate Manifest.Validate() error = %v, want ErrDuplicateRecord", err)
	}

	_, err := policy.Decode([]byte(`{"format":"authorization.policy/v1","revision":1,"algorithm":"deny-overrides","policies":[],"unknown":true}`))
	if !errors.Is(err, policy.ErrInvalidManifest) {
		t.Errorf("unknown-field policy.Decode() error = %v, want ErrInvalidManifest", err)
	}
	_, err = policy.Decode([]byte(`{} {}`))
	if !errors.Is(err, policy.ErrInvalidManifest) {
		t.Errorf("trailing policy.Decode() error = %v, want ErrInvalidManifest", err)
	}
	_, err = policy.Decode([]byte(`{}`))
	if !errors.Is(err, policy.ErrInvalidManifest) {
		t.Errorf("invalid semantic policy.Decode() error = %v, want ErrInvalidManifest", err)
	}

	if _, err := policy.Encode(policy.Manifest{}); !errors.Is(err, policy.ErrInvalidManifest) {
		t.Errorf("invalid policy.Encode() error = %v, want ErrInvalidManifest", err)
	}

	invalidWindow := valid
	start := time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(-time.Hour)
	invalidWindow.Policies = append([]policy.Record(nil), valid.Policies...)
	invalidWindow.Policies[0].ActiveFrom = &start
	invalidWindow.Policies[0].ActiveUntil = &end
	if err := invalidWindow.Validate(); !errors.Is(err, policy.ErrInvalidManifest) {
		t.Errorf("invalid activation Manifest.Validate() error = %v, want ErrInvalidManifest", err)
	}

	unencodable := valid
	outOfRange := time.Date(10_000, time.January, 1, 0, 0, 0, 0, time.UTC)
	unencodable.Policies = append([]policy.Record(nil), valid.Policies...)
	unencodable.Policies[0].ActiveFrom = &outOfRange
	if _, err := policy.Encode(unencodable); err == nil {
		t.Error("unencodable policy.Encode() error = nil")
	}

	const maxManifestBytes = 16 << 20
	if _, err := policy.Decode(bytes.Repeat([]byte{'x'}, maxManifestBytes)); errors.Is(err, policy.ErrManifestLimitExceeded) {
		t.Errorf("policy.Decode(at limit) error = %v, do not want ErrManifestLimitExceeded", err)
	}
	atLimit := valid
	atLimit.Policies = append([]policy.Record(nil), valid.Policies...)
	atLimit.Policies[0].Document = json.RawMessage(`{"value":""}`)
	baseline, err := policy.Encode(atLimit)
	if err != nil {
		t.Fatalf("policy.Encode(at limit baseline) error = %v", err)
	}
	atLimit.Policies[0].Document = json.RawMessage(
		`{"value":"` + strings.Repeat("x", maxManifestBytes-len(baseline)) + `"}`,
	)
	encoded, err := policy.Encode(atLimit)
	if err != nil {
		t.Fatalf("policy.Encode(at limit) error = %v", err)
	}
	if len(encoded) != maxManifestBytes {
		t.Errorf("len(policy.Encode(at limit)) = %d, want %d", len(encoded), maxManifestBytes)
	}
	atLimit.Policies[0].Document = json.RawMessage(
		`{"value":"` + strings.Repeat("x", maxManifestBytes-len(baseline)+1) + `"}`,
	)
	if _, err := policy.Encode(atLimit); !errors.Is(err, policy.ErrManifestLimitExceeded) {
		t.Errorf("policy.Encode(one byte over limit) error = %v, want ErrManifestLimitExceeded", err)
	}

	if _, err := policy.Decode(bytes.Repeat([]byte{'x'}, maxManifestBytes+1)); !errors.Is(err, policy.ErrManifestLimitExceeded) {
		t.Errorf("policy.Decode(oversized) error = %v, want ErrManifestLimitExceeded", err)
	}
	oversized := valid
	oversized.Policies = append([]policy.Record(nil), valid.Policies...)
	oversized.Policies[0].Document = json.RawMessage(
		`{"value":"` + strings.Repeat("x", maxManifestBytes) + `"}`,
	)
	if _, err := policy.Encode(oversized); !errors.Is(err, policy.ErrManifestLimitExceeded) {
		t.Errorf("policy.Encode(oversized) error = %v, want ErrManifestLimitExceeded", err)
	}
}
