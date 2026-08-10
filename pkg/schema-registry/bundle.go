package schemaregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

var (
	// ErrReferenceMissing marks a bundle whose transitive graph is incomplete.
	ErrReferenceMissing = errors.New("schema registry: missing reference")
	// ErrReferenceCycle marks a cycle in a provider-coordinate reference graph.
	ErrReferenceCycle = errors.New("schema registry: reference cycle")
	// ErrReferenceLimit marks a schema graph that exceeds caller-selected bounds.
	ErrReferenceLimit = errors.New("schema registry: reference limit exceeded")
	// ErrFingerprintCollision marks unequal canonical schemas with one claimed
	// portable fingerprint.
	ErrFingerprintCollision = errors.New("schema registry: fingerprint collision")
)

// GraphLimits bound local and provider reference traversal.
type GraphLimits struct {
	MaxSchemas    int
	MaxDepth      int
	MaxReferences int
}

// Provenance records the immutable source and revision of an offline bundle.
type Provenance struct {
	Source   string
	Revision string
}

// Bundle is an immutable, content-addressed graph for startup and offline use.
// Resolution performs no network access.
type Bundle struct {
	root       Fingerprint
	schemas    map[Fingerprint]Schema
	provenance Provenance
}

type bundleWire struct {
	Version    uint64             `json:"version"`
	Root       string             `json:"root"`
	Provenance Provenance         `json:"provenance"`
	Schemas    []bundleSchemaWire `json:"schemas"`
}

type bundleSchemaWire struct {
	Fingerprint string                `json:"fingerprint"`
	Format      Format                `json:"format"`
	Content     []byte                `json:"content"`
	References  []bundleReferenceWire `json:"references,omitempty"`
	Metadata    map[string]string     `json:"metadata,omitempty"`
}

type bundleReferenceWire struct {
	Name        string `json:"name"`
	Subject     string `json:"subject,omitempty"`
	Version     uint64 `json:"version,omitempty"`
	Fingerprint string `json:"fingerprint"`
}

// NewBundle validates a complete bounded graph rooted at root.
func NewBundle(
	root Schema,
	dependencies []Schema,
	limits GraphLimits,
	provenance Provenance,
) (Bundle, error) {
	if limits.MaxSchemas <= 0 || limits.MaxDepth <= 0 || limits.MaxReferences <= 0 {
		return Bundle{}, fmt.Errorf("%w: invalid graph limits", ErrInvalidRequest)
	}
	if provenance.Source == "" || provenance.Revision == "" {
		return Bundle{}, fmt.Errorf("%w: incomplete provenance", ErrInvalidRequest)
	}
	if 1+len(dependencies) > limits.MaxSchemas {
		return Bundle{}, fmt.Errorf("%w: schemas", ErrReferenceLimit)
	}

	schemas := make(map[Fingerprint]Schema)
	if err := insertBundleSchema(schemas, root); err != nil {
		return Bundle{}, err
	}
	for _, dependency := range dependencies {
		if err := insertBundleSchema(schemas, dependency); err != nil {
			return Bundle{}, err
		}
	}

	state := make(map[Fingerprint]uint8, len(schemas))
	references := 0
	var visit func(Fingerprint, int) error
	visit = func(fingerprint Fingerprint, depth int) error {
		if depth > limits.MaxDepth {
			return fmt.Errorf("%w: depth", ErrReferenceLimit)
		}
		switch state[fingerprint] {
		case 1:
			return fmt.Errorf("%w: %s", ErrReferenceCycle, fingerprint)
		case 2:
			return nil
		}
		schema, exists := schemas[fingerprint]
		if !exists {
			return fmt.Errorf("%w: %s", ErrReferenceMissing, fingerprint)
		}
		state[fingerprint] = 1
		for _, reference := range schema.definition.References {
			references++
			if references > limits.MaxReferences {
				return fmt.Errorf("%w: references", ErrReferenceLimit)
			}
			if err := visit(reference.Fingerprint, depth+1); err != nil {
				return err
			}
		}
		state[fingerprint] = 2
		return nil
	}
	if err := visit(root.Fingerprint(), 1); err != nil {
		return Bundle{}, err
	}

	return Bundle{root: root.Fingerprint(), schemas: schemas, provenance: provenance}, nil
}

// Root returns the bundle root schema.
func (bundle Bundle) Root() Schema { return bundle.schemas[bundle.root] }

// Resolve returns a schema solely from the immutable local bundle.
func (bundle Bundle) Resolve(fingerprint Fingerprint) (Schema, bool) {
	schema, found := bundle.schemas[fingerprint]
	return schema, found
}

// Provenance returns the immutable bundle source metadata.
func (bundle Bundle) Provenance() Provenance { return bundle.provenance }

// MarshalBinary returns a deterministic, versioned offline artifact. Loading
// recompiles every definition; the artifact never authorizes network access.
func (bundle Bundle) MarshalBinary() ([]byte, error) {
	if bundle.root == (Fingerprint{}) || len(bundle.schemas) == 0 {
		return nil, fmt.Errorf("%w: empty bundle", ErrInvalidSchema)
	}
	wire := bundleWire{Version: 1, Root: bundle.root.String(), Provenance: bundle.provenance}
	wire.Schemas = make([]bundleSchemaWire, 0, len(bundle.schemas))
	for fingerprint, schema := range bundle.schemas {
		definition := schema.Definition()
		entry := bundleSchemaWire{
			Fingerprint: fingerprint.String(), Format: definition.Format,
			Content: definition.Content, Metadata: definition.Metadata,
			References: make([]bundleReferenceWire, 0, len(definition.References)),
		}
		for _, reference := range definition.References {
			entry.References = append(entry.References, bundleReferenceWire{
				Name: reference.Name, Subject: reference.Subject, Version: reference.Version,
				Fingerprint: reference.Fingerprint.String(),
			})
		}
		wire.Schemas = append(wire.Schemas, entry)
	}
	slices.SortFunc(wire.Schemas, compareBundleSchemaFingerprints)
	// bundleWire contains only JSON-safe concrete fields.
	encoded, _ := json.Marshal(wire)
	return encoded, nil
}

func compareBundleSchemaFingerprints(left, right bundleSchemaWire) int {
	if left.Fingerprint < right.Fingerprint {
		return -1
	}
	if left.Fingerprint > right.Fingerprint {
		return 1
	}
	return 0
}

// LoadBundle validates and recompiles a bounded artifact with caller-supplied
// local canonicalizers. Canonicalizers must not perform network access.
func LoadBundle(
	ctx context.Context,
	encoded []byte,
	canonicalizers map[Format]Canonicalizer,
	limits GraphLimits,
	maxBundleBytes int,
) (Bundle, error) {
	if err := ctx.Err(); err != nil {
		return Bundle{}, err
	}
	if maxBundleBytes <= 0 || len(encoded) == 0 {
		return Bundle{}, fmt.Errorf("%w: bundle bytes", ErrInvalidRequest)
	}
	if len(encoded) > maxBundleBytes {
		return Bundle{}, fmt.Errorf("%w: bundle bytes", ErrLimitExceeded)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var wire bundleWire
	if err := decoder.Decode(&wire); err != nil {
		return Bundle{}, fmt.Errorf("%w: decode bundle", ErrInvalidSchema)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Bundle{}, err
	}
	if wire.Version != 1 || len(wire.Schemas) == 0 || len(wire.Schemas) > limits.MaxSchemas {
		return Bundle{}, fmt.Errorf("%w: bundle version or schemas", ErrInvalidSchema)
	}
	rootFingerprint, err := ParseFingerprint(wire.Root)
	if err != nil {
		return Bundle{}, err
	}
	compiled := make([]Schema, 0, len(wire.Schemas))
	var root Schema
	for _, entry := range wire.Schemas {
		claimed, err := ParseFingerprint(entry.Fingerprint)
		if err != nil {
			return Bundle{}, err
		}
		references := make([]Reference, 0, len(entry.References))
		for _, reference := range entry.References {
			fingerprint, err := ParseFingerprint(reference.Fingerprint)
			if err != nil {
				return Bundle{}, err
			}
			references = append(references, Reference{
				Name: reference.Name, Subject: reference.Subject, Version: reference.Version, Fingerprint: fingerprint,
			})
		}
		canonicalizer := canonicalizers[entry.Format]
		if interfaceIsNil(canonicalizer) {
			return Bundle{}, fmt.Errorf("%w: %s", ErrUnsupportedFormat, entry.Format)
		}
		compileLimits := DefaultCompileLimits()
		compileLimits.MaxSchemaBytes = maxBundleBytes
		compileLimits.MaxCanonicalBytes = maxBundleBytes
		compileLimits.MaxReferences = limits.MaxReferences
		schema, err := CompileWithLimits(ctx, Definition{
			Format: entry.Format, Content: entry.Content, References: references, Metadata: entry.Metadata,
		}, canonicalizer, compileLimits)
		if err != nil {
			return Bundle{}, err
		}
		if schema.Fingerprint() != claimed {
			return Bundle{}, fmt.Errorf("%w: claimed %s", ErrFingerprintCollision, claimed)
		}
		if claimed == rootFingerprint {
			root = schema
		} else {
			compiled = append(compiled, schema)
		}
	}
	if root.Fingerprint() == (Fingerprint{}) {
		return Bundle{}, fmt.Errorf("%w: root", ErrReferenceMissing)
	}
	return NewBundle(root, compiled, limits, wire.Provenance)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing bundle data", ErrInvalidSchema)
	}
	return nil
}

func insertBundleSchema(schemas map[Fingerprint]Schema, schema Schema) error {
	fingerprint := schema.Fingerprint()
	if fingerprint == (Fingerprint{}) {
		return fmt.Errorf("%w: zero schema", ErrInvalidSchema)
	}
	existing, found := schemas[fingerprint]
	if found && !sameSchema(existing, schema) {
		return fmt.Errorf("%w: %s", ErrFingerprintCollision, fingerprint)
	}
	schemas[fingerprint] = schema
	return nil
}

func sameSchema(left, right Schema) bool {
	if left.definition.Format != right.definition.Format ||
		!bytes.Equal(left.canonical, right.canonical) ||
		len(left.definition.References) != len(right.definition.References) {
		return false
	}
	for index := range left.definition.References {
		leftReference := left.definition.References[index]
		rightReference := right.definition.References[index]
		if leftReference.Name != rightReference.Name ||
			leftReference.Fingerprint != rightReference.Fingerprint {
			return false
		}
	}
	return true
}
