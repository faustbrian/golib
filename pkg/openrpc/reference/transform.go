package reference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

var (
	// ErrTransformPolicy reports non-positive dereferencing limits.
	ErrTransformPolicy = errors.New("reference: invalid transform policy")
	// ErrTransformLimit reports a dereferencing resource-bound violation.
	ErrTransformLimit = errors.New("reference: transform limit exceeded")
	// ErrDereferenceCycle reports recursive structural reference expansion.
	ErrDereferenceCycle = errors.New("reference: dereference cycle")
	// ErrTransformInput reports an invalid zero JSON value.
	ErrTransformInput = errors.New("reference: invalid transform input")
)

// TransformError identifies the logical JSON Pointer where dereferencing
// failed while retaining a stable error category through Unwrap.
type TransformError struct {
	Pointer string
	Err     error
}

// Error implements error without including referenced values or URI text.
func (err *TransformError) Error() string {
	return fmt.Sprintf("reference transform at %s: %v", err.Pointer, err.Err)
}

// Unwrap supports errors.Is and errors.As for the underlying category.
func (err *TransformError) Unwrap() error { return err.Err }

// TransformPolicy bounds structural reference transformations independently
// from fetching and URI resolution limits.
type TransformPolicy struct {
	MaxDepth        int
	MaxReferences   int
	MaxOutputBytes  int
	MaxOutputTokens int
}

// Selector decides whether the $ref object at a logical JSON path is a
// transformable reference. The supplied path is owned by the caller.
type Selector interface {
	Dereference(path []string) bool
}

// SelectorFunc adapts a function to Selector.
type SelectorFunc func([]string) bool

// Dereference implements Selector.
func (function SelectorFunc) Dereference(path []string) bool { return function(path) }

// DefaultTransformPolicy returns finite limits for untrusted documents.
func DefaultTransformPolicy() TransformPolicy {
	return TransformPolicy{
		MaxDepth:        256,
		MaxReferences:   100_000,
		MaxOutputBytes:  64 << 20,
		MaxOutputTokens: 4_000_000,
	}
}

// Dereference replaces every object containing $ref with its resolved target.
// Draft 7 treats $ref siblings as ignored, so they are intentionally not
// merged into the target. The operation performs I/O only through resolver's
// explicitly configured Store and returns an immutable JSON value.
func Dereference(
	ctx context.Context,
	resolver *Resolver,
	root jsonvalue.Value,
	base string,
	policy TransformPolicy,
) (jsonvalue.Value, error) {
	return DereferenceSelected(ctx, resolver, root, base, policy, nil)
}

// DereferenceSelected replaces only $ref objects accepted by selector. A nil
// selector expands every $ref object. This distinguishes OpenRPC Reference
// Objects from recursive JSON Schema references when required by a caller.
func DereferenceSelected(
	ctx context.Context,
	resolver *Resolver,
	root jsonvalue.Value,
	base string,
	policy TransformPolicy,
	selector Selector,
) (jsonvalue.Value, error) {
	if resolver == nil || ctx == nil || !validTransformPolicy(policy) {
		return jsonvalue.Value{}, ErrTransformPolicy
	}
	if err := ctx.Err(); err != nil {
		return jsonvalue.Value{}, err
	}
	rootURI, err := absoluteDocumentURI(base)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	decoded, err := decodeTransformValue(root.Bytes())
	if err != nil {
		return jsonvalue.Value{}, ErrTransformInput
	}
	state := resolveState{documents: map[string]jsonvalue.Value{rootURI: root}}
	walk := transformWalk{
		ctx:      ctx,
		resolver: resolver,
		policy:   policy,
		state:    &state,
		active:   make(map[string]struct{}),
		selector: selector,
	}
	result, err := walk.value(decoded, rootURI, 0, nil)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	encoded, _ := json.Marshal(result)
	if len(encoded) > policy.MaxOutputBytes {
		return jsonvalue.Value{}, transformError(nil, ErrTransformLimit)
	}
	jsonPolicy := resolver.policy.JSON
	jsonPolicy.MaxBytes = policy.MaxOutputBytes
	jsonPolicy.MaxDepth = policy.MaxDepth
	jsonPolicy.MaxTokens = policy.MaxOutputTokens
	transformed, err := jsonvalue.Parse(encoded, jsonPolicy)
	if err != nil {
		return jsonvalue.Value{}, transformError(nil, ErrTransformLimit)
	}
	return transformed, nil
}

type transformWalk struct {
	ctx        context.Context
	resolver   *Resolver
	policy     TransformPolicy
	state      *resolveState
	active     map[string]struct{}
	selector   Selector
	references int
}

func (walk *transformWalk) value(value any, documentURI string, depth int, path []string) (any, error) {
	if err := walk.ctx.Err(); err != nil {
		return nil, transformError(path, err)
	}
	if depth > walk.policy.MaxDepth {
		return nil, transformError(path, ErrTransformLimit)
	}
	switch typed := value.(type) {
	case map[string]any:
		if raw, exists := typed["$ref"]; exists {
			selected := walk.selector == nil || walk.selector.Dereference(append([]string(nil), path...))
			if selected {
				input, ok := raw.(string)
				if !ok || input == "" {
					return nil, transformError(path, ErrInvalidReference)
				}
				resolved, err := walk.reference(input, documentURI, depth, path)
				if err != nil {
					return nil, transformError(path, err)
				}
				return resolved, nil
			}
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child, err := walk.value(typed[key], documentURI, depth+1, append(path, key))
			if err != nil {
				return nil, err
			}
			typed[key] = child
		}
		return typed, nil
	case []any:
		for index := range typed {
			child, err := walk.value(typed[index], documentURI, depth+1, append(path, strconv.Itoa(index)))
			if err != nil {
				return nil, err
			}
			typed[index] = child
		}
		return typed, nil
	default:
		return value, nil
	}
}

func (walk *transformWalk) reference(input string, documentURI string, depth int, path []string) (any, error) {
	walk.references++
	if walk.references > walk.policy.MaxReferences {
		return nil, ErrTransformLimit
	}
	parsed, err := Parse(input, walk.resolver.policy.Reference)
	if err != nil {
		return nil, err
	}
	parsed, err = parsed.ResolveAgainst(documentURI)
	if err != nil {
		return nil, err
	}
	identity := parsed.String()
	if _, cycle := walk.active[identity]; cycle {
		return nil, ErrDereferenceCycle
	}
	walk.active[identity] = struct{}{}
	defer delete(walk.active, identity)

	walk.state.visited = make(map[string]struct{})
	target, err := walk.resolver.resolve(walk.ctx, parsed, walk.state)
	if err != nil {
		if errors.Is(err, ErrReferenceCycle) {
			return nil, ErrDereferenceCycle
		}
		return nil, err
	}
	decoded, _ := decodeTransformValue(target.value.Bytes())
	return walk.value(decoded, target.documentURI, depth+1, path)
}

func validTransformPolicy(policy TransformPolicy) bool {
	return policy.MaxDepth > 0 && policy.MaxReferences > 0 &&
		policy.MaxOutputBytes > 0 && policy.MaxOutputTokens > 0
}

func decodeTransformValue(input []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func transformError(path []string, err error) error {
	var existing *TransformError
	if errors.As(err, &existing) {
		return err
	}
	return &TransformError{Pointer: transformPointer(path), Err: err}
}

func transformPointer(path []string) string {
	if len(path) == 0 {
		return "#"
	}
	escaped := make([]string, len(path))
	for index, token := range path {
		token = strings.ReplaceAll(token, "~", "~0")
		escaped[index] = strings.ReplaceAll(token, "/", "~1")
	}
	return "#/" + strings.Join(escaped, "/")
}
