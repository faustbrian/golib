package validate

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

var errInvalidOADResourceRoot = errors.New("invalid OpenAPI description document root")

type validationReferenceResolver struct {
	resolver  reference.Resolver
	resources map[string]reference.Resource
	dialect   specversion.Dialect
}

func (resolver *validationReferenceResolver) Resolve(
	ctx context.Context,
	identifier string,
) (reference.Resource, error) {
	if resource, exists := resolver.resources[identifier]; exists {
		return resource, nil
	}
	resource, err := resolver.resolver.Resolve(ctx, identifier)
	if err != nil {
		return reference.Resource{}, err
	}
	switch resolver.dialect {
	case specversion.DialectOAS32:
		switch resource.Root.Kind() {
		case jsonvalue.ObjectKind, jsonvalue.BooleanKind:
		default:
			return reference.Resource{}, errInvalidOADResourceRoot
		}
	}
	resolver.resources[identifier] = resource
	return resource, nil
}

func validationResolver(
	resolver reference.Resolver,
	dialect specversion.Dialect,
) reference.Resolver {
	if resolver == nil {
		return nil
	}
	value := reflect.ValueOf(resolver)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil
		}
	}
	if cached, ok := resolver.(*validationReferenceResolver); ok &&
		cached.dialect == dialect {
		return cached
	}
	return &validationReferenceResolver{
		resolver: resolver, resources: make(map[string]reference.Resource),
		dialect: dialect,
	}
}

func validReferenceResourceURI(raw string) bool {
	if raw == "" {
		return true
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Fragment == ""
}

func validateReferenceTargets(
	ctx context.Context,
	document openapi.Document,
	options Options,
) ([]Diagnostic, error) {
	limits := normalizedReferenceLimits(options.ReferenceLimits)
	resource := validationResource(document, options.ReferenceResourceURI)
	dialect := document.SpecificationVersion().Dialect()
	resolver := validationResolver(options.ReferenceResolver, dialect)
	occurrences, err := reference.ScanFiltered(
		ctx,
		resource.Root,
		limits,
		func(pointer reference.Pointer, value jsonvalue.Value) bool {
			_, stringValue := value.Text()
			return stringValue &&
				referenceSourceKind(pointer.Tokens(), dialect) != ""
		},
	)
	if err != nil {
		return nil, fmt.Errorf("validate OpenAPI references: %w", err)
	}
	if len(occurrences) > options.MaxReferences {
		return nil, fmt.Errorf(
			"validate OpenAPI references: %w",
			reference.ErrLimitExceeded,
		)
	}
	diagnostics := make([]Diagnostic, 0)
	for _, occurrence := range occurrences {
		expectedKind := referenceSourceKind(
			occurrence.Pointer().Tokens(), dialect,
		)
		chain, err := reference.ResolveChain(
			ctx, resource, occurrence.Raw(), resolver, limits,
		)
		if err == nil {
			if invalidReferenceCycle(expectedKind, chain) {
				diagnostics = append(diagnostics, Diagnostic{
					Code:     "openapi.reference.cycle.invalid",
					Message:  "the Reference Object cycle has no concrete target",
					Severity: SeverityError, Source: SourceReference,
					InstanceLocation:     occurrence.Pointer().String(),
					SpecificationVersion: document.SpecificationVersion().String(),
					SpecificationSection: "reference-object",
				})
			} else if incompatibleReferenceTarget(expectedKind, chain) {
				diagnostics = append(diagnostics, Diagnostic{
					Code:     "openapi.reference.target.type",
					Message:  "the reference target has an incompatible OpenAPI object type",
					Severity: SeverityError, Source: SourceReference,
					InstanceLocation:     occurrence.Pointer().String(),
					SpecificationVersion: document.SpecificationVersion().String(),
					SpecificationSection: "reference-object",
				})
			}
			continue
		}
		if errors.Is(err, reference.ErrExternalResolutionDisabled) {
			continue
		}
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, reference.ErrLimitExceeded) {
			return nil, fmt.Errorf("validate OpenAPI references: %w", err)
		}
		code := "openapi.reference.unresolved"
		message := "the reference target could not be resolved"
		if errors.Is(err, reference.ErrTargetNotFound) {
			code = "openapi.reference.target.missing"
			message = "the reference target does not exist"
		} else if errors.Is(err, reference.ErrInvalidReference) ||
			errors.Is(err, reference.ErrInvalidFragment) {
			code = "openapi.reference.invalid"
			message = "the reference is not a valid URI-reference target"
		} else if errors.Is(err, errInvalidOADResourceRoot) {
			code = "openapi.reference.document-root.invalid"
			message = "referenced OpenAPI 3.2 documents must contain an OpenAPI or Schema Object"
		}
		diagnostics = append(diagnostics, Diagnostic{
			Code: code, Message: message,
			Severity: SeverityError, Source: SourceReference,
			InstanceLocation:     occurrence.Pointer().String(),
			SpecificationVersion: document.SpecificationVersion().String(),
			SpecificationSection: "reference-object",
		})
	}
	return diagnostics, nil
}

func normalizedReferenceLimits(limits reference.Limits) reference.Limits {
	defaults := reference.DefaultLimits()
	if limits.MaxTraversalDepth == 0 {
		limits.MaxTraversalDepth = defaults.MaxTraversalDepth
	}
	if limits.MaxTraversalNodes == 0 {
		limits.MaxTraversalNodes = defaults.MaxTraversalNodes
	}
	if limits.MaxReferenceDepth == 0 {
		limits.MaxReferenceDepth = defaults.MaxReferenceDepth
	}
	return limits
}

func validationResource(
	document openapi.Document,
	retrievalURI string,
) reference.Resource {
	resource := reference.Resource{
		RetrievalURI: retrievalURI,
		Root:         document.Raw(),
	}
	switch document.SpecificationVersion().Dialect() {
	case openapi.DialectOAS32:
		if self, exists := document.Raw().Lookup("$self"); exists {
			if rawSelf, valid := self.Text(); valid {
				base, _ := url.Parse(retrievalURI)
				selfReference, err := url.Parse(rawSelf)
				if err == nil {
					resource.CanonicalURI = base.ResolveReference(selfReference).String()
				}
			}
		}
	}
	return resource
}

func invalidReferenceCycle(
	expectedKind string,
	chain reference.Chain,
) bool {
	return chain.Circular() && expectedKind != "schemas"
}

func incompatibleReferenceTarget(
	expected string,
	chain reference.Chain,
) bool {
	for _, target := range chain.Targets() {
		actual := referenceTargetKind(target.Fragment)
		if actual != "" && actual != expected {
			return true
		}
	}
	return false
}

func referenceSourceKind(tokens []string, dialect openapi.Dialect) string {
	switch len(tokens) {
	case 0, 1:
		return ""
	}
	if tokens[len(tokens)-1] != "$ref" {
		return ""
	}
	if referenceDataPointer(tokens, dialect) {
		return ""
	}
	switch len(tokens) {
	case 4:
		if tokens[0] == "components" {
			return tokens[1]
		}
	}
	if schemaPointerContext(tokens) {
		return "schemas"
	}
	switch len(tokens) {
	case 3:
		switch tokens[0] {
		case "parameters":
			return "parameters"
		case "responses":
			return "responses"
		case "securityDefinitions":
			return "securitySchemes"
		case "paths", "webhooks":
			return "pathItems"
		}
	}
	for index, token := range tokens {
		if token == "callbacks" && len(tokens) == index+4 {
			return "pathItems"
		}
	}
	parent := tokens[len(tokens)-2]
	if parent == "requestBody" {
		return "requestBodies"
	}
	if len(tokens) == 2 {
		return ""
	}
	switch tokens[len(tokens)-3] {
	case "responses", "parameters", "headers", "callbacks", "links", "examples":
		return tokens[len(tokens)-3]
	case "content":
		switch dialect {
		case openapi.DialectOAS32:
			return "mediaTypes"
		default:
			return ""
		}
	case "properties", "allOf", "oneOf", "anyOf", "prefixItems":
		return "schemas"
	default:
		return ""
	}
}

func referenceDataPointer(tokens []string, dialect openapi.Dialect) bool {
	schemaStart := schemaPointerStart(tokens)
	insideSchema := false
	for index, token := range tokens[:len(tokens)-1] {
		if index == schemaStart {
			insideSchema = true
			continue
		}
		mapEntry := referenceMapNamesMayStartWithX(tokens[:index])
		if strings.HasPrefix(strings.ToLower(token), "x-") && !mapEntry {
			return true
		}
		if token == "example" && !mapEntry {
			return true
		}
		switch index {
		case 0, 1:
		default:
			ancestor := tokens[index-2]
			switch token {
			case "value", "dataValue":
				if ancestor == "examples" {
					return true
				}
			case "requestBody":
				if ancestor == "links" {
					return true
				}
			case "examples":
				if dialect == openapi.DialectSwagger20 && ancestor == "responses" {
					return true
				}
			}
		}
		if insideSchema && !mapEntry {
			switch token {
			case "const", "default", "enum", "examples":
				return true
			}
		}
	}
	return false
}

func referenceMapNamesMayStartWithX(tokens []string) bool {
	switch len(tokens) {
	case 0:
		return false
	case 1:
		switch tokens[0] {
		case "definitions", "parameters", "responses", "securityDefinitions":
			return true
		}
	case 2:
		if tokens[0] == "components" {
			return true
		}
	}
	switch tokens[len(tokens)-1] {
	case "$defs", "properties", "patternProperties", "dependentSchemas",
		"examples", "headers", "links", "callbacks", "content", "encoding":
		return true
	default:
		return false
	}
}

func schemaPointerContext(tokens []string) bool {
	return schemaPointerStart(tokens) != -1
}

func schemaPointerStart(tokens []string) int {
	switch len(tokens) {
	case 0, 1:
		return -1
	case 2:
		if tokens[0] == "definitions" {
			return 1
		}
	default:
		switch tokens[0] {
		case "components":
			if tokens[1] == "schemas" {
				return 2
			}
		case "definitions":
			return 1
		}
	}
	for index, token := range tokens[:len(tokens)-1] {
		if token == "schema" {
			return index
		}
	}
	return -1
}

func referenceTargetKind(fragment reference.Fragment) string {
	if fragment.Kind() != reference.FragmentPointer {
		return ""
	}
	tokens := fragment.Pointer().Tokens()
	if len(tokens) >= 3 && tokens[0] == "components" {
		if tokens[1] == "schemas" || referenceSchemaContext(tokens[3:]) {
			return "schemas"
		}
		if len(tokens) == 3 {
			return tokens[1]
		}
		return ""
	}
	if len(tokens) == 2 &&
		(tokens[0] == "paths" || tokens[0] == "webhooks") {
		return "pathItems"
	}
	if len(tokens) < 2 {
		return ""
	}
	switch tokens[0] {
	case "definitions":
		return "schemas"
	case "parameters", "responses":
		if len(tokens) == 2 {
			return tokens[0]
		}
		if referenceSchemaContext(tokens[2:]) {
			return "schemas"
		}
		return ""
	case "securityDefinitions":
		return "securitySchemes"
	default:
		return ""
	}
}

func referenceSchemaContext(tokens []string) bool {
	for _, token := range tokens {
		switch token {
		case "schema", "properties", "items", "allOf", "oneOf", "anyOf",
			"not", "additionalProperties", "prefixItems", "$defs", "definitions":
			return true
		}
	}
	return false
}

func resolveReferencedObject(
	ctx context.Context,
	root jsonvalue.Value,
	value jsonvalue.Value,
) (jsonvalue.Value, bool) {
	return resolveReferencedObjectWithPolicy(
		ctx,
		reference.Resource{Root: root},
		value,
		nil,
		reference.DefaultLimits(),
	)
}

func resolveReferencedSchema(
	ctx context.Context,
	root jsonvalue.Value,
	value jsonvalue.Value,
) (jsonvalue.Value, bool) {
	resolved, _, ok := resolveReferencedSchemaResourceWithPolicy(
		ctx,
		reference.Resource{Root: root},
		value,
		nil,
		reference.DefaultLimits(),
	)
	return resolved, ok
}

func resolveReferencedSchemaResourceWithPolicy(
	ctx context.Context,
	resource reference.Resource,
	value jsonvalue.Value,
	resolver reference.Resolver,
	limits reference.Limits,
) (jsonvalue.Value, reference.Resource, bool) {
	referenceValue, exists := value.Lookup("$ref")
	if !exists {
		kind := value.Kind()
		switch kind {
		case jsonvalue.ObjectKind, jsonvalue.BooleanKind:
			return value, resource, true
		default:
			return value, reference.Resource{}, false
		}
	}
	rawReference, ok := referenceValue.Text()
	if !ok {
		return jsonvalue.Value{}, reference.Resource{}, false
	}
	chain, err := reference.ResolveChain(
		ctx,
		resource,
		rawReference,
		resolver,
		limits,
	)
	if err != nil || chain.Circular() {
		return jsonvalue.Value{}, reference.Resource{}, false
	}
	targets := chain.Targets()
	target := targets[len(targets)-1]
	kind := target.Value.Kind()
	if kind != jsonvalue.ObjectKind && kind != jsonvalue.BooleanKind {
		return jsonvalue.Value{}, reference.Resource{}, false
	}
	return target.Value, target.Resource, true
}

func resolveReferencedObjectWithPolicy(
	ctx context.Context,
	resource reference.Resource,
	value jsonvalue.Value,
	resolver reference.Resolver,
	limits reference.Limits,
) (jsonvalue.Value, bool) {
	resolved, _, ok := resolveReferencedObjectResourceWithPolicy(
		ctx, resource, value, resolver, limits,
	)
	return resolved, ok
}

func resolveReferencedObjectResourceWithPolicy(
	ctx context.Context,
	resource reference.Resource,
	value jsonvalue.Value,
	resolver reference.Resolver,
	limits reference.Limits,
) (jsonvalue.Value, reference.Resource, bool) {
	referenceValue, exists := value.Lookup("$ref")
	if !exists {
		return value, resource, value.Kind() == jsonvalue.ObjectKind
	}
	rawReference, ok := referenceValue.Text()
	if !ok {
		return jsonvalue.Value{}, reference.Resource{}, false
	}
	chain, err := reference.ResolveChain(
		ctx,
		resource,
		rawReference,
		resolver,
		limits,
	)
	if err != nil || chain.Circular() {
		return jsonvalue.Value{}, reference.Resource{}, false
	}
	targets := chain.Targets()
	if targets[len(targets)-1].Value.Kind() != jsonvalue.ObjectKind {
		return jsonvalue.Value{}, reference.Resource{}, false
	}
	target := targets[len(targets)-1]
	return target.Value, target.Resource, true
}
