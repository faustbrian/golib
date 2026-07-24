package compose

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

var (
	// ErrInvalidRename reports unknown component kinds, missing names, or
	// invalid resource options.
	ErrInvalidRename = errors.New("compose: invalid component rename")
	// ErrRenameConflict reports an existing or repeated target name.
	ErrRenameConflict = errors.New("compose: component rename conflict")
	// ErrRenameLimit reports excessive rename count or output size.
	ErrRenameLimit = errors.New("compose: component rename limit exceeded")
)

// ComponentKind identifies one registry in the OpenRPC Components Object.
type ComponentKind string

const (
	SchemaComponents            ComponentKind = "schemas"
	LinkComponents              ComponentKind = "links"
	ErrorComponents             ComponentKind = "errors"
	ExampleComponents           ComponentKind = "examples"
	ExamplePairingComponents    ComponentKind = "examplePairings"
	ContentDescriptorComponents ComponentKind = "contentDescriptors"
	TagComponents               ComponentKind = "tags"
)

// RenameOptions bounds deterministic component rewriting.
type RenameOptions struct {
	MaxRenames     int
	MaxOutputBytes int
	Parse          openrpcparse.Options
	Validation     validate.Options
}

// DefaultRenameOptions preserves already accepted future fields and applies
// finite rewrite limits.
func DefaultRenameOptions() RenameOptions {
	parseOptions := openrpcparse.DefaultOptions()
	parseOptions.UnknownFields = openrpcparse.PreserveUnknownFields
	return RenameOptions{
		MaxRenames:     100_000,
		MaxOutputBytes: 64 << 20,
		Parse:          parseOptions,
		Validation:     validate.DefaultOptions(),
	}
}

// RenameComponents atomically renames component keys and rewrites matching
// internal JSON Pointer references throughout the document. Unrelated and
// external references retain their exact spelling.
func RenameComponents(
	ctx context.Context,
	document openrpc.Document,
	renames map[ComponentKind]map[string]string,
	options RenameOptions,
) (openrpc.Document, error) {
	if ctx == nil || options.MaxRenames <= 0 || options.MaxOutputBytes <= 0 {
		return openrpc.Document{}, ErrInvalidRename
	}
	if err := ctx.Err(); err != nil {
		return openrpc.Document{}, err
	}
	count := 0
	for kind, names := range renames {
		if !validComponentKind(kind) {
			return openrpc.Document{}, ErrInvalidRename
		}
		count += len(names)
	}
	if count > options.MaxRenames {
		return openrpc.Document{}, ErrRenameLimit
	}
	encoded, err := openrpc.MarshalCanonical(document)
	if err != nil {
		return openrpc.Document{}, ErrInvalidRename
	}
	decoded, _ := decodeOverlayJSON(encoded)
	// Canonical documents always encode as JSON objects.
	root, _ := decoded.(map[string]any)
	components, _ := root["components"].(map[string]any)
	for _, kind := range sortedRenameKinds(renames) {
		registry, _ := components[string(kind)].(map[string]any)
		for _, oldName := range sortedRenameNames(renames[kind]) {
			newName := renames[kind][oldName]
			if oldName == "" || newName == "" {
				return openrpc.Document{}, ErrInvalidRename
			}
			value, exists := registry[oldName]
			if !exists {
				return openrpc.Document{}, ErrInvalidRename
			}
			if _, conflict := registry[newName]; conflict && newName != oldName {
				return openrpc.Document{}, ErrRenameConflict
			}
			if newName != oldName {
				delete(registry, oldName)
				registry[newName] = value
			}
		}
	}
	rewriteComponentReferences(root, renames)
	encoded, _ = json.Marshal(root)
	if len(encoded) > options.MaxOutputBytes {
		return openrpc.Document{}, ErrRenameLimit
	}
	parsed, err := openrpcparse.Decode(encoded, options.Parse)
	if err != nil {
		return openrpc.Document{}, ErrInvalidRename
	}
	if report := validate.Document(ctx, parsed.Document(), options.Validation); !report.Valid() {
		return openrpc.Document{}, ErrInvalidRename
	}
	return parsed.Document(), nil
}

func rewriteComponentReferences(value any, renames map[ComponentKind]map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		if reference, ok := typed["$ref"].(string); ok {
			for _, kind := range sortedRenameKinds(renames) {
				for _, oldName := range sortedRenameNames(renames[kind]) {
					prefix := "#/components/" + string(kind) + "/" + escape(oldName)
					if reference == prefix || strings.HasPrefix(reference, prefix+"/") {
						reference = "#/components/" + string(kind) + "/" +
							escape(renames[kind][oldName]) + strings.TrimPrefix(reference, prefix)
					}
				}
			}
			typed["$ref"] = reference
		}
		for _, child := range typed {
			rewriteComponentReferences(child, renames)
		}
	case []any:
		for _, child := range typed {
			rewriteComponentReferences(child, renames)
		}
	}
}

func validComponentKind(kind ComponentKind) bool {
	switch kind {
	case SchemaComponents, LinkComponents, ErrorComponents, ExampleComponents,
		ExamplePairingComponents, ContentDescriptorComponents, TagComponents:
		return true
	default:
		return false
	}
}

func sortedRenameKinds(renames map[ComponentKind]map[string]string) []ComponentKind {
	names := make([]string, 0, len(renames))
	for kind := range renames {
		names = append(names, string(kind))
	}
	sort.Strings(names)
	kinds := make([]ComponentKind, len(names))
	for index, name := range names {
		kinds[index] = ComponentKind(name)
	}
	return kinds
}

func sortedRenameNames(renames map[string]string) []string {
	names := make([]string, 0, len(renames))
	for name := range renames {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func escape(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}
