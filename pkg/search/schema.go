package search

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
)

var (
	ErrInvalidIndexDefinition = errors.New("search: invalid index definition")
	ErrSchemaLimit            = errors.New("search: index definition exceeds size limit")
)

// IndexDefinition is an immutable backend-neutral settings and mapping
// definition. Backend-specific settings remain explicit JSON owned by the
// adapter rather than being silently normalized into shared semantics.
type IndexDefinition struct {
	name        string
	settings    json.RawMessage
	mappings    json.RawMessage
	fingerprint string
}

// NewIndexDefinition validates and canonicalizes bounded JSON object settings
// and mappings. The fingerprint excludes the physical index name so equivalent
// generations compare equal.
func NewIndexDefinition(name string, settings, mappings json.RawMessage, limits Limits) (IndexDefinition, error) {
	if !validIndexName(name) {
		return IndexDefinition{}, ErrInvalidIndexDefinition
	}
	if limits.MaxJSONDepth <= 0 || limits.MaxJSONNodes <= 0 {
		return IndexDefinition{}, ErrInvalidLimits
	}
	if len(settings)+len(mappings) > limits.MaxSourceBytes {
		return IndexDefinition{}, ErrSchemaLimit
	}
	remainingNodes := limits.MaxJSONNodes
	canonicalSettings, err := canonicalJSONObject(settings, limits.MaxJSONDepth, &remainingNodes)
	if err != nil {
		return IndexDefinition{}, errors.Join(ErrInvalidIndexDefinition, err)
	}
	canonicalMappings, err := canonicalJSONObject(mappings, limits.MaxJSONDepth, &remainingNodes)
	if err != nil {
		return IndexDefinition{}, errors.Join(ErrInvalidIndexDefinition, err)
	}
	hash := sha256.New()
	_, _ = hash.Write(canonicalSettings)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(canonicalMappings)
	return IndexDefinition{
		name: name, settings: canonicalSettings, mappings: canonicalMappings,
		fingerprint: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func (d IndexDefinition) Name() string { return d.name }
func (d IndexDefinition) Settings() json.RawMessage {
	return append(json.RawMessage(nil), d.settings...)
}
func (d IndexDefinition) Mappings() json.RawMessage {
	return append(json.RawMessage(nil), d.mappings...)
}
func (d IndexDefinition) Fingerprint() string { return d.fingerprint }

type CompatibilityKind string

const (
	Compatible      CompatibilityKind = "compatible"
	ReindexRequired CompatibilityKind = "reindex_required"
)

type Compatibility struct {
	Kind    CompatibilityKind
	Reasons []string
}

// CompareDefinitions reports whether two physical generations have identical
// shared schema semantics. Any difference requires an explicit reindex plan;
// adapters may offer a more detailed backend-native compatibility analysis.
func CompareDefinitions(current, target IndexDefinition) Compatibility {
	if current.fingerprint == target.fingerprint {
		return Compatibility{Kind: Compatible}
	}
	reasons := make([]string, 0, 2)
	if !bytes.Equal(current.settings, target.settings) {
		reasons = append(reasons, "settings changed")
	}
	if !bytes.Equal(current.mappings, target.mappings) {
		reasons = append(reasons, "mappings changed")
	}
	return Compatibility{Kind: ReindexRequired, Reasons: reasons}
}

func canonicalJSONObject(value json.RawMessage, maximumDepth int, remainingNodes *int) (json.RawMessage, error) {
	if err := validateBoundedJSONObject(value, maximumDepth, remainingNodes); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	if _, ok := decoded.(map[string]any); !ok {
		return nil, ErrInvalidIndexDefinition
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	return json.Marshal(decoded)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalidIndexDefinition
	}
	return nil
}

func validIndexName(name string) bool {
	if name == "" || len(name) > 255 || name == "." || name == ".." || strings.HasPrefix(name, "_") || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "+") {
		return false
	}
	for _, character := range name {
		if unicode.IsUpper(character) || unicode.IsSpace(character) || unicode.IsControl(character) || strings.ContainsRune(`\/*?"<>|,#:`, character) {
			return false
		}
	}
	return true
}
