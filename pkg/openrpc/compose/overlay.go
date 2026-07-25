package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

var (
	// ErrInvalidOverlay reports malformed patches or invalid options.
	ErrInvalidOverlay = errors.New("compose: invalid overlay")
	// ErrOverlayLimit reports an action or output resource-bound violation.
	ErrOverlayLimit = errors.New("compose: overlay limit exceeded")
	// ErrOverlayResult reports a patch result that is not a valid OpenRPC
	// document under the configured parser and semantic validator.
	ErrOverlayResult = errors.New("compose: invalid overlay result")
)

// Overlay is one immutable RFC 7396 JSON Merge Patch object. Actions are
// applied in slice order by ApplyOverlays.
type Overlay struct {
	patch jsonvalue.Value
}

// NewOverlay validates and owns one object-valued merge patch.
func NewOverlay(input []byte, policy jsonvalue.Policy) (Overlay, error) {
	patch, err := jsonvalue.Parse(input, policy)
	if err != nil {
		return Overlay{}, ErrInvalidOverlay
	}
	decoded, _ := decodeOverlayJSON(patch.Bytes())
	if _, object := decoded.(map[string]any); !object {
		return Overlay{}, ErrInvalidOverlay
	}
	return Overlay{patch: patch}, nil
}

// Bytes returns an owned copy of the merge patch.
func (overlay Overlay) Bytes() []byte { return overlay.patch.Bytes() }

// OverlayOptions bounds ordered patch application and configures the strict
// parser used for the resulting document.
type OverlayOptions struct {
	MaxActions     int
	MaxOutputBytes int
	Parse          openrpcparse.Options
	Validation     validate.Options
}

// DefaultOverlayOptions returns strict finite composition limits.
func DefaultOverlayOptions() OverlayOptions {
	return OverlayOptions{
		MaxActions:     1_000,
		MaxOutputBytes: 64 << 20,
		Parse:          openrpcparse.DefaultOptions(),
		Validation:     validate.DefaultOptions(),
	}
}

// ApplyOverlays applies object-valued RFC 7396 patches atomically in the given
// order. Null removes a field, objects merge recursively, and every other JSON
// value replaces its target. The base document is never mutated.
func ApplyOverlays(
	ctx context.Context,
	document openrpc.Document,
	overlays []Overlay,
	options OverlayOptions,
) (openrpc.Document, error) {
	if ctx == nil || options.MaxActions <= 0 || options.MaxOutputBytes <= 0 ||
		len(overlays) > options.MaxActions {
		return openrpc.Document{}, ErrInvalidOverlay
	}
	if err := ctx.Err(); err != nil {
		return openrpc.Document{}, err
	}
	encoded, err := openrpc.MarshalCanonical(document)
	if err != nil {
		return openrpc.Document{}, ErrOverlayResult
	}
	if len(encoded) > options.MaxOutputBytes {
		return openrpc.Document{}, ErrOverlayLimit
	}
	current, _ := decodeOverlayJSON(encoded)
	for _, overlay := range overlays {
		if err := ctx.Err(); err != nil {
			return openrpc.Document{}, err
		}
		patch, err := decodeOverlayJSON(overlay.patch.Bytes())
		if err != nil {
			return openrpc.Document{}, ErrInvalidOverlay
		}
		patchObject, object := patch.(map[string]any)
		if !object {
			return openrpc.Document{}, ErrInvalidOverlay
		}
		current = mergePatch(current, patchObject)
		encoded, _ = json.Marshal(current)
		if len(encoded) > options.MaxOutputBytes {
			return openrpc.Document{}, ErrOverlayLimit
		}
	}
	parsed, err := openrpcparse.Decode(encoded, options.Parse)
	if err != nil {
		return openrpc.Document{}, ErrOverlayResult
	}
	if report := validate.Document(ctx, parsed.Document(), options.Validation); !report.Valid() {
		return openrpc.Document{}, ErrOverlayResult
	}
	return parsed.Document(), nil
}

func mergePatch(target any, patch map[string]any) any {
	targetObject, object := target.(map[string]any)
	if !object {
		targetObject = make(map[string]any)
	}
	for name, patchValue := range patch {
		if patchValue == nil {
			delete(targetObject, name)
			continue
		}
		if patchObject, object := patchValue.(map[string]any); object {
			targetObject[name] = mergePatch(targetObject[name], patchObject)
			continue
		}
		targetObject[name] = patchValue
	}
	return targetObject
}

func decodeOverlayJSON(input []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidOverlay
	}
	return value, nil
}
