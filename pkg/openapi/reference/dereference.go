package reference

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

var (
	// ErrDereferenceSibling reports ambiguous Path Item siblings or malformed
	// Reference Object overlay fields.
	ErrDereferenceSibling = errors.New("reference object has meaningful siblings")
	// ErrDereferenceCycle reports a recursive chain of Reference Objects.
	ErrDereferenceCycle = errors.New("reference object cycle")
	// ErrDereferenceTarget reports a Reference Object whose target is not an
	// object and therefore cannot replace the referencing object.
	ErrDereferenceTarget = errors.New("reference object target is not an object")
)

// DereferenceOptions bounds one explicit Reference Object expansion.
type DereferenceOptions struct {
	ReferenceLimits Limits
	MaxReferences   int
	MaxNodes        int
	MaxDepth        int
}

// DefaultDereferenceOptions returns conservative bounds for untrusted input.
func DefaultDereferenceOptions() DereferenceOptions {
	return DereferenceOptions{
		ReferenceLimits: DefaultLimits(),
		MaxReferences:   100_000,
		MaxNodes:        1_000_000,
		MaxDepth:        256,
	}
}

// DereferenceEntry records one Reference Object replacement.
type DereferenceEntry struct {
	sourceResource string
	sourcePointer  string
	rawReference   string
	targetResource string
	targetPointer  string
}

// SourceResource returns the resource containing the replaced reference.
func (entry DereferenceEntry) SourceResource() string { return entry.sourceResource }

// SourcePointer returns the escaped pointer to the replaced $ref member.
func (entry DereferenceEntry) SourcePointer() string { return entry.sourcePointer }

// RawReference returns the original URI-reference spelling.
func (entry DereferenceEntry) RawReference() string { return entry.rawReference }

// TargetResource returns the resolved target resource identity.
func (entry DereferenceEntry) TargetResource() string { return entry.targetResource }

// TargetPointer returns the target pointer, or anchor prefixed with #.
func (entry DereferenceEntry) TargetPointer() string { return entry.targetPointer }

// DereferenceResult owns a dereferenced document and ordered provenance.
type DereferenceResult struct {
	document openapi.Document
	entries  []DereferenceEntry
}

// Document returns the immutable dereferenced document.
func (result DereferenceResult) Document() openapi.Document { return result.document }

// Entries returns caller-owned dereference provenance.
func (result DereferenceResult) Entries() []DereferenceEntry {
	return append([]DereferenceEntry(nil), result.entries...)
}

// DereferenceObjects replaces OpenAPI Reference Objects with their resolved
// object targets. JSON Schema $ref keywords are deliberately retained because
// inlining them can change base URI and dynamic-scope semantics. Resolver is
// the only external I/O boundary.
func DereferenceObjects(
	ctx context.Context,
	base Resource,
	resolver Resolver,
	options DereferenceOptions,
) (DereferenceResult, error) {
	return dereferenceObjects(ctx, base, resolver, options, openapi.Decode)
}

type documentDecoder func(jsonvalue.Value) (openapi.Document, error)

func dereferenceObjects(
	ctx context.Context,
	base Resource,
	resolver Resolver,
	options DereferenceOptions,
	decode documentDecoder,
) (DereferenceResult, error) {
	if ctx == nil {
		return DereferenceResult{}, errors.New("dereference objects: nil context")
	}
	if err := ctx.Err(); err != nil {
		return DereferenceResult{}, err
	}
	if err := options.validate(); err != nil {
		return DereferenceResult{}, err
	}
	document, err := decode(base.Root)
	if err != nil {
		return DereferenceResult{}, fmt.Errorf("dereference objects: %w", err)
	}
	base = withOpenAPI32Self(base)
	dereferencer := objectDereferencer{
		ctx:      ctx,
		dialect:  document.SpecificationVersion().Dialect(),
		resolver: resolver,
		options:  options,
	}
	if !resolverIsNil(resolver) {
		dereferencer.resolver = &cachedResolver{
			resolver:  resolver,
			base:      base,
			resources: make(map[string]Resource),
		}
	}
	root, err := dereferencer.rewrite(
		base, base.Root, nil, "", 1, make(map[string]bool),
	)
	if err != nil {
		return DereferenceResult{}, err
	}
	resultDocument, err := decode(root)
	if err != nil {
		return DereferenceResult{}, fmt.Errorf("dereference objects: %w", err)
	}
	return DereferenceResult{
		document: resultDocument,
		entries:  dereferencer.entries,
	}, nil
}

func (options DereferenceOptions) validate() error {
	if err := options.ReferenceLimits.validate(); err != nil {
		return err
	}
	if options.MaxReferences < 1 || options.MaxNodes < 1 || options.MaxDepth < 1 {
		return ErrLimitExceeded
	}
	return nil
}

type objectDereferencer struct {
	ctx        context.Context
	dialect    specversion.Dialect
	resolver   Resolver
	options    DereferenceOptions
	nodes      int
	references int
	entries    []DereferenceEntry
}

func (dereferencer *objectDereferencer) rewrite(
	resource Resource,
	value jsonvalue.Value,
	tokens []string,
	sourcePointer string,
	depth int,
	chain map[string]bool,
) (jsonvalue.Value, error) {
	if err := dereferencer.ctx.Err(); err != nil {
		return jsonvalue.Value{}, err
	}
	if depth > dereferencer.options.MaxDepth {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	dereferencer.nodes++
	if dereferencer.nodes > dereferencer.options.MaxNodes {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	if value.Kind() == jsonvalue.ObjectKind &&
		dereferencer.isReferenceObject(tokens) {
		if referenceValue, exists := value.Lookup("$ref"); exists {
			return dereferencer.replaceReference(
				resource, value, referenceValue, tokens, sourcePointer,
				depth, chain,
			)
		}
	}
	childCount, _ := value.Length()
	if !childrenFitBudget(
		childCount, dereferencer.nodes, 0, depth,
		dereferencer.options.MaxNodes, dereferencer.options.MaxDepth,
	) {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	switch value.Kind() {
	case jsonvalue.ArrayKind:
		elements, _ := value.Elements()
		for index := range elements {
			childToken := strconv.Itoa(index)
			transformed, err := dereferencer.rewrite(
				resource, elements[index], appendToken(tokens, childToken),
				appendPointer(sourcePointer, childToken), depth+1, chain,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			elements[index] = transformed
		}
		result, _ := jsonvalue.Array(elements)
		return result, nil
	case jsonvalue.ObjectKind:
		members, _ := value.Members()
		for index := range members {
			transformed, err := dereferencer.rewrite(
				resource, members[index].Value,
				appendToken(tokens, members[index].Name),
				appendPointer(sourcePointer, members[index].Name),
				depth+1, chain,
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			members[index].Value = transformed
		}
		result, _ := jsonvalue.Object(members)
		return result, nil
	default:
		return value, nil
	}
}

func (dereferencer *objectDereferencer) replaceReference(
	resource Resource,
	object jsonvalue.Value,
	referenceValue jsonvalue.Value,
	tokens []string,
	sourcePointer string,
	depth int,
	chain map[string]bool,
) (jsonvalue.Value, error) {
	raw, ok := referenceValue.Text()
	if !ok {
		return jsonvalue.Value{}, fmt.Errorf(
			"%w at %s", ErrInvalidReference,
			appendPointer(sourcePointer, "$ref"),
		)
	}
	if dereferencer.pathItemReferenceHasMeaningfulSiblings(tokens, object) {
		return jsonvalue.Value{}, fmt.Errorf(
			"%w at %s", ErrDereferenceSibling, sourcePointer,
		)
	}
	dereferencer.references++
	if dereferencer.references > dereferencer.options.MaxReferences {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	target, err := Resolve(
		dereferencer.ctx, resource, raw, dereferencer.resolver,
		dereferencer.options.ReferenceLimits,
	)
	if err != nil {
		return jsonvalue.Value{}, fmt.Errorf(
			"dereference object at %s: %w", sourcePointer, err,
		)
	}
	if target.Value.Kind() != jsonvalue.ObjectKind {
		return jsonvalue.Value{}, fmt.Errorf(
			"%w at %s", ErrDereferenceTarget, sourcePointer,
		)
	}
	registry := dereferencer.referenceObjectRegistry(tokens)
	if registry != "" {
		location, known := knownBundleTargetLocation(
			dereferencer.dialect, target.Fragment,
		)
		if known && location.registry != registry {
			return jsonvalue.Value{}, fmt.Errorf(
				"%w at %s: expected %s, got %s",
				ErrDereferenceTarget, sourcePointer, registry, location.registry,
			)
		}
	}
	identity := targetIdentity(target)
	if chain[identity] {
		return jsonvalue.Value{}, fmt.Errorf(
			"%w at %s", ErrDereferenceCycle, sourcePointer,
		)
	}
	dereferencer.entries = append(dereferencer.entries, DereferenceEntry{
		sourceResource: resourceIdentifier(resource),
		sourcePointer:  appendPointer(sourcePointer, "$ref"),
		rawReference:   raw,
		targetResource: resourceIdentifier(target.Resource),
		targetPointer:  dereferenceTargetPointer(target.Fragment),
	})
	chain[identity] = true
	targetTokens := tokens
	if _, known := knownBundleTargetLocation(
		dereferencer.dialect, target.Fragment,
	); known && target.Fragment.Kind() == FragmentPointer {
		targetTokens = target.Fragment.Pointer().Tokens()
	}
	transformed, err := dereferencer.rewrite(
		target.Resource, target.Value, targetTokens,
		dereferenceTargetPointer(target.Fragment), depth+1, chain,
	)
	delete(chain, identity)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	return dereferencer.applyReferenceOverlays(
		transformed, object, registry, sourcePointer,
	)
}

func (dereferencer *objectDereferencer) applyReferenceOverlays(
	target jsonvalue.Value,
	referenceObject jsonvalue.Value,
	registry string,
	pointer string,
) (jsonvalue.Value, error) {
	if dereferencer.dialect != specversion.DialectOAS31 &&
		dereferencer.dialect != specversion.DialectOAS32 {
		return target, nil
	}
	allowed := map[string]bool{}
	switch registry {
	case "examples":
		allowed["summary"] = true
		allowed["description"] = true
	case "pathItems":
		allowed["summary"] = true
		allowed["description"] = true
	case "responses":
		allowed["description"] = true
		if dereferencer.dialect == specversion.DialectOAS32 {
			allowed["summary"] = true
		}
	case "parameters", "requestBodies", "headers",
		"securitySchemes", "links":
		allowed["description"] = true
	}
	targetMembers, _ := target.Members()
	referenceMembers, _ := referenceObject.Members()
	for _, sibling := range referenceMembers {
		if !allowed[sibling.Name] {
			continue
		}
		if _, valid := sibling.Value.Text(); !valid {
			return jsonvalue.Value{}, fmt.Errorf(
				"%w at %s/%s", ErrDereferenceSibling, pointer,
				escapeBundlePointer(sibling.Name),
			)
		}
		replaced := false
		for index := range targetMembers {
			if targetMembers[index].Name == sibling.Name {
				targetMembers[index].Value = sibling.Value
				replaced = true
				break
			}
		}
		if !replaced {
			targetMembers = append(targetMembers, sibling)
		}
	}
	result, _ := jsonvalue.Object(targetMembers)
	return result, nil
}

func (dereferencer *objectDereferencer) pathItemReferenceHasMeaningfulSiblings(
	tokens []string,
	object jsonvalue.Value,
) bool {
	pathItem := dereferencer.isPathItem(tokens) || callbackPathItem(tokens)
	if !pathItem {
		return false
	}
	members, _ := object.Members()
	if len(members) < 2 {
		return false
	}
	componentReference := len(tokens) == 3 && tokens[0] == "components" &&
		tokens[1] == "pathItems" &&
		(dereferencer.dialect == specversion.DialectOAS31 ||
			dereferencer.dialect == specversion.DialectOAS32)
	if !componentReference {
		return true
	}
	for _, member := range members {
		switch member.Name {
		case "$ref", "summary", "description":
			continue
		case "get", "put", "post", "delete", "options", "head", "patch",
			"trace", "query", "servers", "parameters", "additionalOperations":
			return true
		}
	}
	return false
}

func (dereferencer *objectDereferencer) isReferenceObject(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	if dereferencer.dialect == specversion.DialectSwagger20 {
		return lastParentIs(tokens, "parameters") ||
			lastParentIs(tokens, "responses")
	}
	if len(tokens) == 3 && tokens[0] == "components" &&
		tokens[1] != "schemas" {
		return oasBundleRegistries[dereferencer.dialect][tokens[1]]
	}
	if dereferencer.isPathItem(tokens) || lastParentIs(tokens, "parameters") ||
		lastParentIs(tokens, "responses") || lastTokenIs(tokens, "requestBody") {
		return true
	}
	if dereferencer.dialect == specversion.DialectOAS32 &&
		lastParentIs(tokens, "content") {
		return true
	}
	for _, registry := range []string{"headers", "links", "examples", "callbacks"} {
		if lastParentIs(tokens, registry) {
			return true
		}
	}
	return callbackPathItem(tokens)
}

func (dereferencer *objectDereferencer) referenceObjectRegistry(
	tokens []string,
) string {
	if dereferencer.dialect == specversion.DialectSwagger20 {
		if lastParentIs(tokens, "parameters") {
			return "parameters"
		}
		if lastParentIs(tokens, "responses") {
			return "responses"
		}
		return ""
	}
	if len(tokens) == 3 && tokens[0] == "components" &&
		oasBundleRegistries[dereferencer.dialect][tokens[1]] {
		return tokens[1]
	}
	if dereferencer.isPathItem(tokens) || callbackPathItem(tokens) {
		return "pathItems"
	}
	if lastParentIs(tokens, "parameters") {
		return "parameters"
	}
	if lastParentIs(tokens, "responses") {
		return "responses"
	}
	if lastTokenIs(tokens, "requestBody") {
		return "requestBodies"
	}
	if dereferencer.dialect == specversion.DialectOAS32 &&
		lastParentIs(tokens, "content") {
		return "mediaTypes"
	}
	for _, registry := range []string{"headers", "links", "examples", "callbacks"} {
		if lastParentIs(tokens, registry) {
			return registry
		}
	}
	return ""
}

func (dereferencer *objectDereferencer) isPathItem(tokens []string) bool {
	if len(tokens) == 2 && (tokens[0] == "paths" || tokens[0] == "webhooks") {
		return true
	}
	return len(tokens) == 3 && tokens[0] == "components" &&
		tokens[1] == "pathItems"
}

func callbackPathItem(tokens []string) bool {
	for index, token := range tokens {
		if token == "callbacks" && len(tokens) == index+3 {
			return true
		}
	}
	return false
}

func lastParentIs(tokens []string, name string) bool {
	return len(tokens) >= 2 && tokens[len(tokens)-2] == name
}

func lastTokenIs(tokens []string, name string) bool {
	return len(tokens) >= 1 && tokens[len(tokens)-1] == name
}

func appendPointer(pointer string, token string) string {
	return pointer + "/" + escapeBundlePointer(token)
}

func dereferenceTargetPointer(fragment Fragment) string {
	if fragment.Kind() == FragmentAnchor {
		return "#" + fragment.Anchor()
	}
	return fragment.Pointer().String()
}
