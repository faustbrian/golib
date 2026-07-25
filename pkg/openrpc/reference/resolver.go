package reference

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

var (
	// ErrResolvePolicy reports invalid resolver resource or access policy.
	ErrResolvePolicy = errors.New("reference: invalid resolver policy")
	// ErrResolveLimit reports a depth, document, reference, or fetched-byte
	// violation.
	ErrResolveLimit = errors.New("reference: resolver limit exceeded")
	// ErrReferenceCycle reports a repeated target in one resolution chain.
	ErrReferenceCycle = errors.New("reference: resolution cycle")
	// ErrExternalDisabled reports external resolution denied by policy.
	ErrExternalDisabled = errors.New("reference: external resolution disabled")
	// ErrSchemeNotAllowed reports an external URI scheme denied by policy.
	ErrSchemeNotAllowed = errors.New("reference: external scheme not allowed")
	// ErrHostNotAllowed reports an external URI host denied by policy.
	ErrHostNotAllowed = errors.New("reference: external host not allowed")
	// ErrLoadFailed reports that a caller-provided store could not load a
	// document. Store errors are deliberately not included to avoid leaking
	// fetched content or credentials.
	ErrLoadFailed = errors.New("reference: document load failed")
	// ErrInvalidDocument reports invalid JSON returned by a store.
	ErrInvalidDocument = errors.New("reference: invalid loaded document")
)

// Store explicitly supplies external documents. The maximum byte argument is
// the remaining resolver allowance; implementations should stop reading when
// it is exceeded. Resolver rechecks the returned size before parsing.
type Store interface {
	Load(ctx context.Context, documentURI string, maxBytes int) ([]byte, error)
}

// StoreFunc adapts a function to Store.
type StoreFunc func(context.Context, string, int) ([]byte, error)

// Load implements Store.
func (function StoreFunc) Load(ctx context.Context, documentURI string, maxBytes int) ([]byte, error) {
	return function(ctx, documentURI, maxBytes)
}

// ResolvePolicy bounds resolution and explicitly authorizes external targets.
// Allowed schemes and hosts use case-insensitive exact matching.
type ResolvePolicy struct {
	MaxDepth        int
	MaxDocuments    int
	MaxFetchedBytes int
	MaxReferences   int
	AllowExternal   bool
	AllowedSchemes  []string
	AllowedHosts    []string
	Reference       Policy
	Pointer         PointerPolicy
	JSON            jsonvalue.Policy
}

// DefaultResolvePolicy disables external access while applying finite bounds.
func DefaultResolvePolicy() ResolvePolicy {
	return ResolvePolicy{
		MaxDepth:        64,
		MaxDocuments:    32,
		MaxFetchedBytes: 16 << 20,
		MaxReferences:   100_000,
		Reference:       DefaultPolicy(),
		Pointer:         DefaultPointerPolicy(),
		JSON:            jsonvalue.DefaultPolicy(),
	}
}

// Resolver resolves JSON Pointer targets and reference aliases. It performs no
// I/O unless external access is enabled and a Store is supplied.
type Resolver struct {
	store          Store
	policy         ResolvePolicy
	allowedSchemes map[string]struct{}
	allowedHosts   map[string]struct{}
}

// NewResolver validates and takes an ownership-safe copy of policy.
func NewResolver(store Store, policy ResolvePolicy) (*Resolver, error) {
	if !validResolvePolicy(policy) {
		return nil, ErrResolvePolicy
	}
	policy.AllowedSchemes = append([]string(nil), policy.AllowedSchemes...)
	policy.AllowedHosts = append([]string(nil), policy.AllowedHosts...)
	return &Resolver{
		store:          store,
		policy:         policy,
		allowedSchemes: normalizedSet(policy.AllowedSchemes),
		allowedHosts:   normalizedSet(policy.AllowedHosts),
	}, nil
}

// Target is one resolved immutable JSON value and its source document URI.
type Target struct {
	value       jsonvalue.Value
	documentURI string
}

// Value returns the resolved value.
func (target Target) Value() jsonvalue.Value { return target.value }

// DocumentURI returns the absolute URI of the containing document without its
// fragment.
func (target Target) DocumentURI() string { return target.documentURI }

// Resolve follows input from root, including reference aliases, under policy.
// Per-call document caching makes repeated aliases deterministic without
// retaining mutable process-global or cross-request state.
func (resolver *Resolver) Resolve(
	ctx context.Context,
	root jsonvalue.Value,
	base string,
	input string,
) (Target, error) {
	targets, err := resolver.ResolveMany(ctx, root, base, []string{input})
	if err != nil {
		return Target{}, err
	}
	return targets[0], nil
}

// ResolveMany follows references under one shared, per-call document cache and
// resource budget. References are resolved in input order and each chain has
// independent cycle detection.
func (resolver *Resolver) ResolveMany(
	ctx context.Context,
	root jsonvalue.Value,
	base string,
	inputs []string,
) ([]Target, error) {
	if resolver == nil {
		return nil, ErrResolvePolicy
	}
	if ctx == nil {
		return nil, ErrResolvePolicy
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(inputs) > resolver.policy.MaxReferences {
		return nil, ErrResolveLimit
	}
	rootURI, err := absoluteDocumentURI(base)
	if err != nil {
		return nil, err
	}

	state := resolveState{
		documents: map[string]jsonvalue.Value{rootURI: root},
	}
	targets := make([]Target, len(inputs))
	for index, input := range inputs {
		reference, parseErr := Parse(input, resolver.policy.Reference)
		if parseErr != nil {
			return nil, parseErr
		}
		reference, parseErr = reference.ResolveAgainst(rootURI)
		if parseErr != nil {
			return nil, parseErr
		}
		state.visited = make(map[string]struct{})
		targets[index], parseErr = resolver.resolve(ctx, reference, &state)
		if parseErr != nil {
			return nil, parseErr
		}
	}
	return targets, nil
}

type resolveState struct {
	documents    map[string]jsonvalue.Value
	visited      map[string]struct{}
	fetchedBytes int
	fetchedDocs  int
	references   int
}

func (resolver *Resolver) resolve(ctx context.Context, current Reference, state *resolveState) (Target, error) {
	for depth := 0; ; depth++ {
		if err := ctx.Err(); err != nil {
			return Target{}, err
		}
		if depth >= resolver.policy.MaxDepth {
			return Target{}, ErrResolveLimit
		}
		state.references++
		if state.references > resolver.policy.MaxReferences {
			return Target{}, ErrResolveLimit
		}
		documentURI := uriWithoutFragment(current.uri)
		pointer, err := current.TargetPointer(resolver.policy.Pointer)
		if err != nil {
			return Target{}, err
		}
		identity := documentURI + "#" + pointer.String()
		if _, duplicate := state.visited[identity]; duplicate {
			return Target{}, ErrReferenceCycle
		}
		state.visited[identity] = struct{}{}

		document, ok := state.documents[documentURI]
		if !ok {
			document, err = resolver.load(ctx, documentURI, state)
			if err != nil {
				return Target{}, err
			}
			state.documents[documentURI] = document
		}
		value, err := pointer.Evaluate(document, resolver.policy.JSON)
		if err != nil {
			return Target{}, err
		}
		alias, ok, err := referenceAlias(value)
		if err != nil {
			return Target{}, err
		}
		if !ok {
			return Target{value: value, documentURI: documentURI}, nil
		}
		current, err = Parse(alias, resolver.policy.Reference)
		if err != nil {
			return Target{}, err
		}
		current, err = current.ResolveAgainst(documentURI)
		if err != nil {
			return Target{}, err
		}
	}
}

func (resolver *Resolver) load(ctx context.Context, documentURI string, state *resolveState) (jsonvalue.Value, error) {
	if !resolver.policy.AllowExternal {
		return jsonvalue.Value{}, ErrExternalDisabled
	}
	parsed, err := url.Parse(documentURI)
	if err != nil {
		return jsonvalue.Value{}, ErrInvalidReference
	}
	if _, allowed := resolver.allowedSchemes[strings.ToLower(parsed.Scheme)]; !allowed {
		return jsonvalue.Value{}, ErrSchemeNotAllowed
	}
	if _, allowed := resolver.allowedHosts[strings.ToLower(parsed.Hostname())]; !allowed {
		return jsonvalue.Value{}, ErrHostNotAllowed
	}
	if state.fetchedDocs >= resolver.policy.MaxDocuments || resolver.store == nil {
		if resolver.store == nil {
			return jsonvalue.Value{}, ErrLoadFailed
		}
		return jsonvalue.Value{}, ErrResolveLimit
	}
	remaining := resolver.policy.MaxFetchedBytes - state.fetchedBytes
	if remaining <= 0 {
		return jsonvalue.Value{}, ErrResolveLimit
	}
	data, err := resolver.store.Load(ctx, documentURI, remaining)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return jsonvalue.Value{}, contextErr
		}
		if errors.Is(err, ErrStoreLimit) {
			return jsonvalue.Value{}, ErrResolveLimit
		}
		return jsonvalue.Value{}, ErrLoadFailed
	}
	if len(data) > remaining {
		return jsonvalue.Value{}, ErrResolveLimit
	}
	state.fetchedBytes += len(data)
	state.fetchedDocs++
	document, err := jsonvalue.Parse(data, resolver.policy.JSON)
	if err != nil {
		return jsonvalue.Value{}, ErrInvalidDocument
	}
	return document, nil
}

func referenceAlias(value jsonvalue.Value) (string, bool, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value.Bytes(), &object); err != nil {
		// A non-object value cannot be a Reference Object. Callers validate the
		// target's expected object shape independently.
		return "", false, nil //nolint:nilerr
	}
	raw, exists := object["$ref"]
	if !exists {
		return "", false, nil
	}
	var alias string
	if err := json.Unmarshal(raw, &alias); err != nil || alias == "" {
		return "", false, ErrInvalidReference
	}
	return alias, true, nil
}

func absoluteDocumentURI(input string) (string, error) {
	if input == "" || !utf8ValidURI(input) {
		return "", ErrInvalidBase
	}
	parsed, err := url.Parse(input)
	if err != nil || !parsed.IsAbs() {
		return "", ErrInvalidBase
	}
	return uriWithoutFragment(parsed), nil
}

func uriWithoutFragment(uri *url.URL) string {
	copy := *uri
	copy.Fragment = ""
	copy.RawFragment = ""
	return copy.String()
}

func utf8ValidURI(input string) bool {
	return utf8.ValidString(input) && !containsURIControl(input)
}

func validResolvePolicy(policy ResolvePolicy) bool {
	return policy.MaxDepth > 0 &&
		policy.MaxDocuments > 0 &&
		policy.MaxFetchedBytes > 0 &&
		policy.MaxReferences > 0 &&
		policy.Reference.MaxLength > 0 &&
		policy.Pointer.MaxLength > 0 &&
		policy.Pointer.MaxTokens > 0 &&
		policy.Pointer.MaxIndexDigits > 0 &&
		policy.JSON.MaxBytes > 0 &&
		policy.JSON.MaxDepth > 0 &&
		policy.JSON.MaxTokens > 0
}

func normalizedSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[strings.ToLower(value)] = struct{}{}
	}
	return set
}
