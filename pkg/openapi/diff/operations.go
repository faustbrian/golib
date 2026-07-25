// Package diff provides bounded semantic compatibility comparisons for
// OpenAPI descriptions without treating source-text changes as API changes.
package diff

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"sort"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

// ErrInvalidInput reports nil comparison context or documents.
var ErrInvalidInput = errors.New("invalid OpenAPI diff input")

// ErrInvalidOptions reports negative comparison bounds.
var ErrInvalidOptions = errors.New("invalid OpenAPI diff options")

// ErrUnsupportedComparison reports documents from different specification
// dialects. Cross-dialect compatibility requires explicit conversion policy.
var ErrUnsupportedComparison = errors.New("unsupported OpenAPI comparison")

// ErrLimitExceeded reports more semantic changes than caller policy permits.
var ErrLimitExceeded = errors.New("OpenAPI diff limit exceeded")

// Classification describes compatibility impact.
type Classification string

const (
	// Additive adds an independently addressable API surface.
	Additive Classification = "additive"
	// Breaking removes an independently addressable API surface.
	Breaking Classification = "breaking"
	// Compatible relaxes an existing contract without requiring consumers to
	// change.
	Compatible Classification = "compatible"
	// Conditional requires consumer or policy context for a final decision.
	Conditional Classification = "conditionally-compatible"
	// Unknown reports unresolved or invalid semantics that cannot be inferred.
	Unknown Classification = "unknown"
)

// Kind identifies one operation-surface change.
type Kind string

const (
	PathAdded                     Kind = "path-added"
	PathRemoved                   Kind = "path-removed"
	OperationAdded                Kind = "operation-added"
	OperationRemoved              Kind = "operation-removed"
	WebhookAdded                  Kind = "webhook-added"
	WebhookRemoved                Kind = "webhook-removed"
	OperationIDChanged            Kind = "operation-id-changed"
	RequestBodyAdded              Kind = "request-body-added"
	RequestBodyRemoved            Kind = "request-body-removed"
	RequestBodyRequired           Kind = "request-body-required"
	RequestBodyOptional           Kind = "request-body-optional"
	RequestBodyChanged            Kind = "request-body-changed"
	RequestMediaTypeAdded         Kind = "request-media-type-added"
	RequestMediaTypeRemoved       Kind = "request-media-type-removed"
	ResponseAdded                 Kind = "response-added"
	ResponseRemoved               Kind = "response-removed"
	ResponseChanged               Kind = "response-changed"
	ResponseMediaTypeAdded        Kind = "response-media-type-added"
	ResponseMediaTypeRemoved      Kind = "response-media-type-removed"
	ParameterAdded                Kind = "parameter-added"
	ParameterRemoved              Kind = "parameter-removed"
	ParameterRequired             Kind = "parameter-required"
	ParameterOptional             Kind = "parameter-optional"
	ParameterChanged              Kind = "parameter-changed"
	ParameterSerializationChanged Kind = "parameter-serialization-changed"
	ParameterSchemaChanged        Kind = "parameter-schema-changed"
	ParameterContentChanged       Kind = "parameter-content-changed"
	ParameterDefaultChanged       Kind = "parameter-default-changed"
	ParameterExampleChanged       Kind = "parameter-example-changed"
	RequestSchemaAdded            Kind = "request-schema-added"
	RequestSchemaRemoved          Kind = "request-schema-removed"
	RequestSchemaChanged          Kind = "request-schema-changed"
	ResponseSchemaAdded           Kind = "response-schema-added"
	ResponseSchemaRemoved         Kind = "response-schema-removed"
	ResponseSchemaChanged         Kind = "response-schema-changed"
	ResponseHeaderAdded           Kind = "response-header-added"
	ResponseHeaderRemoved         Kind = "response-header-removed"
	ResponseHeaderChanged         Kind = "response-header-changed"
	SecurityRequired              Kind = "security-required"
	SecurityOptional              Kind = "security-optional"
	SecurityChanged               Kind = "security-changed"
	SecuritySchemeAdded           Kind = "security-scheme-added"
	SecuritySchemeRemoved         Kind = "security-scheme-removed"
	SecuritySchemeChanged         Kind = "security-scheme-changed"
	ServerAdded                   Kind = "server-added"
	ServerRemoved                 Kind = "server-removed"
	ServerChanged                 Kind = "server-changed"
	ServerOrderChanged            Kind = "server-order-changed"
	CallbackAdded                 Kind = "callback-added"
	CallbackRemoved               Kind = "callback-removed"
	CallbackChanged               Kind = "callback-changed"
	CallbackExpressionAdded       Kind = "callback-expression-added"
	CallbackExpressionRemoved     Kind = "callback-expression-removed"
	CallbackExpressionChanged     Kind = "callback-expression-changed"
	CallbackOperationAdded        Kind = "callback-operation-added"
	CallbackOperationRemoved      Kind = "callback-operation-removed"
	LinkAdded                     Kind = "link-added"
	LinkRemoved                   Kind = "link-removed"
	LinkChanged                   Kind = "link-changed"
	RequestEncodingChanged        Kind = "request-encoding-changed"
	ResponseEncodingChanged       Kind = "response-encoding-changed"
	RequestExampleChanged         Kind = "request-example-changed"
	ResponseExampleChanged        Kind = "response-example-changed"
	SwaggerSchemeAdded            Kind = "swagger-scheme-added"
	SwaggerSchemeRemoved          Kind = "swagger-scheme-removed"
	SwaggerSchemesChanged         Kind = "swagger-schemes-changed"
	SwaggerHostChanged            Kind = "swagger-host-changed"
	SwaggerBasePathChanged        Kind = "swagger-base-path-changed"
	RequestMediaTypesChanged      Kind = "request-media-types-changed"
	ResponseMediaTypesChanged     Kind = "response-media-types-changed"
	TagAdded                      Kind = "tag-added"
	TagRemoved                    Kind = "tag-removed"
	TagChanged                    Kind = "tag-changed"
	ExtensionChanged              Kind = "extension-changed"
)

// Change is one immutable semantic surface change.
type Change struct {
	kind           Kind
	classification Classification
	pointer        string
}

// Kind returns the semantic change kind.
func (change Change) Kind() Kind {
	return change.kind
}

// Classification returns the compatibility impact.
func (change Change) Classification() Classification {
	return change.classification
}

// Pointer returns the changed location in the owning description.
func (change Change) Pointer() string {
	return change.pointer
}

// Report is an immutable source-ordered operation-surface comparison.
type Report struct {
	changes []Change
}

// Changes returns a caller-owned copy of the semantic changes.
func (report Report) Changes() []Change {
	return append([]Change(nil), report.changes...)
}

// Options bounds semantic comparison work and output.
type Options struct {
	// MaxChanges limits reported semantic changes.
	MaxChanges int
	// MaxNodes limits semantic values visited in each input document.
	MaxNodes int
	// MaxDepth limits input document nesting.
	MaxDepth int
	// MaxResolvedNodes limits unique resolved semantic target values.
	MaxResolvedNodes int
	// MaxReferenceDepth limits consecutive Reference Object targets.
	MaxReferenceDepth int
	// LeftResourceURI is the retrieval URI used to resolve left references.
	LeftResourceURI string
	// RightResourceURI is the retrieval URI used to resolve right references.
	RightResourceURI string
	// LeftResolver explicitly authorizes external left resource retrieval.
	LeftResolver reference.Resolver
	// RightResolver explicitly authorizes external right resource retrieval.
	RightResolver reference.Resolver
	// ExtensionClassification selects the policy for extension changes.
	ExtensionClassification Classification
}

// DefaultOptions returns a conservative change bound.
func DefaultOptions() Options {
	return Options{
		MaxChanges: 100_000, MaxNodes: 1_000_000, MaxDepth: 256,
		MaxResolvedNodes: 1_000_000, MaxReferenceDepth: 128,
		ExtensionClassification: Conditional,
	}
}

func validResourceURI(raw string) bool {
	if raw == "" {
		return true
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Fragment == ""
}

func validClassification(classification Classification) bool {
	switch classification {
	case "", Additive, Breaking, Compatible, Conditional, Unknown:
		return true
	default:
		return false
	}
}

// Operations compares operation surfaces and selected request and response
// contracts while retaining exact source pointers. Policy-dependent response
// set and operation identifier changes remain explicitly conditional.
func Operations(
	ctx context.Context,
	left openapi.Document,
	right openapi.Document,
	options Options,
) (Report, error) {
	if ctx == nil || left == nil || right == nil {
		return Report{}, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	if options.MaxChanges < 0 || options.MaxNodes < 0 || options.MaxDepth < 0 ||
		options.MaxResolvedNodes < 0 ||
		options.MaxReferenceDepth < 0 ||
		!validResourceURI(options.LeftResourceURI) ||
		!validResourceURI(options.RightResourceURI) ||
		!validClassification(options.ExtensionClassification) {
		return Report{}, ErrInvalidOptions
	}
	defaults := DefaultOptions()
	if options.MaxChanges == 0 {
		options.MaxChanges = defaults.MaxChanges
	}
	if options.MaxNodes == 0 {
		options.MaxNodes = defaults.MaxNodes
	}
	if options.MaxDepth == 0 {
		options.MaxDepth = defaults.MaxDepth
	}
	if options.MaxResolvedNodes == 0 {
		options.MaxResolvedNodes = defaults.MaxResolvedNodes
	}
	if options.MaxReferenceDepth == 0 {
		options.MaxReferenceDepth = defaults.MaxReferenceDepth
	}
	if options.ExtensionClassification == "" {
		options.ExtensionClassification = defaults.ExtensionClassification
	}
	if left.SpecificationVersion().Dialect() != right.SpecificationVersion().Dialect() {
		return Report{}, ErrUnsupportedComparison
	}
	if err := boundValue(ctx, left.Raw(), options.MaxNodes, options.MaxDepth); err != nil {
		return Report{}, err
	}
	if err := boundValue(ctx, right.Raw(), options.MaxNodes, options.MaxDepth); err != nil {
		return Report{}, err
	}

	collector := changeCollector{
		ctx: ctx, maximum: options.MaxChanges,
		leftResourceURI:         options.LeftResourceURI,
		rightResourceURI:        options.RightResourceURI,
		leftResolver:            options.LeftResolver,
		rightResolver:           options.RightResolver,
		extensionClassification: options.ExtensionClassification,
		remainingResolvedNodes:  options.MaxResolvedNodes,
		resolved: [2]map[string]resolvedReference{
			make(map[string]resolvedReference),
			make(map[string]resolvedReference),
		},
		referenceLimits: reference.Limits{
			MaxTraversalDepth: options.MaxDepth,
			MaxTraversalNodes: options.MaxNodes,
			MaxReferenceDepth: options.MaxReferenceDepth,
		},
	}
	dialect := left.SpecificationVersion().Dialect()
	if err := compareExtensions(&collector, left.Raw(), right.Raw(), ""); err != nil {
		return Report{}, err
	}
	if dialect == openapi.DialectSwagger20 {
		if err := compareSwaggerEndpoint(&collector, left.Raw(), right.Raw()); err != nil {
			return Report{}, err
		}
	}
	if err := compareSecuritySchemes(
		&collector, left.Raw(), right.Raw(), dialect,
	); err != nil {
		return Report{}, err
	}
	if err := compareDocumentTags(&collector, left.Raw(), right.Raw()); err != nil {
		return Report{}, err
	}
	leftPaths := objectMembers(left.Raw(), "paths", true)
	rightPaths := objectMembers(right.Raw(), "paths", true)
	leftPaths = resolvePathItems(
		&collector, left.Raw(), leftPaths, dialect, leftComparison,
	)
	rightPaths = resolvePathItems(
		&collector, right.Raw(), rightPaths, dialect, rightComparison,
	)
	if err := collectRemovedContainers(&collector, leftPaths, rightPaths, "/paths", PathRemoved); err != nil {
		return Report{}, err
	}
	if err := compareCommonContainerExtensions(
		&collector, leftPaths, rightPaths, "/paths",
	); err != nil {
		return Report{}, err
	}
	if err := collectRemovedOperations(&collector, leftPaths, rightPaths, "/paths", dialect); err != nil {
		return Report{}, err
	}
	if err := collectCommonOperationContent(
		&collector, left.Raw(), right.Raw(), leftPaths, rightPaths, "/paths", dialect,
	); err != nil {
		return Report{}, err
	}
	if err := collectAddedContainers(&collector, leftPaths, rightPaths, "/paths", PathAdded); err != nil {
		return Report{}, err
	}
	if err := collectAddedOperations(&collector, leftPaths, rightPaths, "/paths", dialect); err != nil {
		return Report{}, err
	}

	leftWebhooks := objectMembers(left.Raw(), "webhooks", false)
	rightWebhooks := objectMembers(right.Raw(), "webhooks", false)
	leftWebhooks = resolvePathItems(
		&collector, left.Raw(), leftWebhooks, dialect, leftComparison,
	)
	rightWebhooks = resolvePathItems(
		&collector, right.Raw(), rightWebhooks, dialect, rightComparison,
	)
	if err := collectRemovedContainers(&collector, leftWebhooks, rightWebhooks, "/webhooks", WebhookRemoved); err != nil {
		return Report{}, err
	}
	if err := compareCommonContainerExtensions(
		&collector, leftWebhooks, rightWebhooks, "/webhooks",
	); err != nil {
		return Report{}, err
	}
	if err := collectRemovedOperations(&collector, leftWebhooks, rightWebhooks, "/webhooks", dialect); err != nil {
		return Report{}, err
	}
	if err := collectCommonOperationContent(
		&collector, left.Raw(), right.Raw(), leftWebhooks, rightWebhooks, "/webhooks", dialect,
	); err != nil {
		return Report{}, err
	}
	if err := collectAddedContainers(&collector, leftWebhooks, rightWebhooks, "/webhooks", WebhookAdded); err != nil {
		return Report{}, err
	}
	if err := collectAddedOperations(&collector, leftWebhooks, rightWebhooks, "/webhooks", dialect); err != nil {
		return Report{}, err
	}
	if collector.resolutionErr != nil {
		return Report{}, collector.resolutionErr
	}
	return Report{changes: collector.changes}, nil
}

func compareSecuritySchemes(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	dialect openapi.Dialect,
) error {
	left, pointer := securitySchemeMembers(leftRoot, dialect)
	right, _ := securitySchemeMembers(rightRoot, dialect)
	rightByName := memberValues(right)
	leftNames := memberNames(left)
	for _, scheme := range left {
		if _, exists := rightByName[scheme.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: SecuritySchemeRemoved, classification: Breaking,
			pointer: pointer + "/" + escapePointer(scheme.Name),
		}); err != nil {
			return err
		}
	}
	for _, scheme := range left {
		replacement, exists := rightByName[scheme.Name]
		if !exists {
			continue
		}
		leftResolved, leftValid := resolveInternalComparable(
			collector, leftRoot, scheme.Value, dialect, leftComparison,
			referenceMetadataNames("securitySchemes", dialect),
		)
		rightResolved, rightValid := resolveInternalComparable(
			collector, rightRoot, replacement, dialect, rightComparison,
			referenceMetadataNames("securitySchemes", dialect),
		)
		schemePointer := pointer + "/" + escapePointer(scheme.Name)
		if leftValid && rightValid {
			if err := compareExtensions(
				collector, leftResolved, rightResolved, schemePointer,
			); err != nil {
				return err
			}
		}
		if leftValid && rightValid &&
			equalObjectWithoutExtensions(leftResolved, rightResolved) {
			continue
		}
		if !leftValid && !rightValid &&
			semanticValueEqual(scheme.Value, replacement) {
			continue
		}
		classification := Conditional
		if !leftValid || !rightValid {
			classification = Unknown
		}
		if err := collector.append(Change{
			kind: SecuritySchemeChanged, classification: classification,
			pointer: schemePointer,
		}); err != nil {
			return err
		}
	}
	for _, scheme := range right {
		if _, exists := leftNames[scheme.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: SecuritySchemeAdded, classification: Additive,
			pointer: pointer + "/" + escapePointer(scheme.Name),
		}); err != nil {
			return err
		}
	}
	return nil
}

func securitySchemeMembers(
	root jsonvalue.Value,
	dialect openapi.Dialect,
) ([]jsonvalue.Member, string) {
	if dialect == openapi.DialectSwagger20 {
		return withoutExtensions(objectMembers(root, "securityDefinitions", false)),
			"/securityDefinitions"
	}
	components, exists := root.Lookup("components")
	if !exists {
		return nil, "/components/securitySchemes"
	}
	return withoutExtensions(objectMembers(components, "securitySchemes", false)),
		"/components/securitySchemes"
}

type tagEntry struct {
	name  string
	value jsonvalue.Value
	index int
}

func compareDocumentTags(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
) error {
	left, leftPresent, leftValid := documentTags(leftRoot)
	right, rightPresent, rightValid := documentTags(rightRoot)
	if !leftValid || !rightValid {
		leftValue, _ := leftRoot.Lookup("tags")
		rightValue, _ := rightRoot.Lookup("tags")
		if leftPresent == rightPresent &&
			(!leftPresent || semanticValueEqual(leftValue, rightValue)) {
			return nil
		}
		return collector.append(Change{
			kind: TagChanged, classification: Unknown, pointer: "/tags",
		})
	}
	rightByName := make(map[string]tagEntry, len(right))
	leftByName := make(map[string]tagEntry, len(left))
	for _, tag := range right {
		rightByName[tag.name] = tag
	}
	for _, tag := range left {
		leftByName[tag.name] = tag
		rightTag, exists := rightByName[tag.name]
		if !exists {
			if err := collector.append(Change{
				kind: TagRemoved, classification: Conditional,
				pointer: "/tags/" + fmt.Sprint(tag.index),
			}); err != nil {
				return err
			}
			continue
		}
		if err := compareExtensions(
			collector, tag.value, rightTag.value,
			"/tags/"+fmt.Sprint(rightTag.index),
		); err != nil {
			return err
		}
		if equalObjectWithoutExtensions(tag.value, rightTag.value) {
			continue
		}
		if err := collector.append(Change{
			kind: TagChanged, classification: Conditional,
			pointer: "/tags/" + fmt.Sprint(rightTag.index),
		}); err != nil {
			return err
		}
	}
	for _, tag := range right {
		if _, exists := leftByName[tag.name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: TagAdded, classification: Conditional,
			pointer: "/tags/" + fmt.Sprint(tag.index),
		}); err != nil {
			return err
		}
	}
	return nil
}

func documentTags(root jsonvalue.Value) ([]tagEntry, bool, bool) {
	value, present := root.Lookup("tags")
	if !present {
		return nil, false, true
	}
	elements, valid := value.Elements()
	if !valid {
		return nil, true, false
	}
	result := make([]tagEntry, 0, len(elements))
	seen := make(map[string]struct{}, len(elements))
	for index, element := range elements {
		nameValue, namePresent := element.Lookup("name")
		name, nameValid := nameValue.Text()
		if !namePresent || !nameValid {
			return nil, true, false
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, true, false
		}
		seen[name] = struct{}{}
		result = append(result, tagEntry{name: name, value: element, index: index})
	}
	return result, true, true
}

type swaggerScheme struct {
	name  string
	index int
}

func compareSwaggerEndpoint(
	collector *changeCollector,
	left jsonvalue.Value,
	right jsonvalue.Value,
) error {
	if err := compareSwaggerSchemes(collector, left, right); err != nil {
		return err
	}
	leftHost, leftHostPresent, leftHostValid := optionalText(left, "host")
	rightHost, rightHostPresent, rightHostValid := optionalText(right, "host")
	if leftHost != rightHost || leftHostPresent != rightHostPresent ||
		leftHostValid != rightHostValid {
		classification := Breaking
		if !leftHostValid || !rightHostValid {
			classification = Unknown
		} else if leftHostPresent != rightHostPresent {
			classification = Conditional
		}
		if err := collector.append(Change{
			kind: SwaggerHostChanged, classification: classification,
			pointer: "/host",
		}); err != nil {
			return err
		}
	}
	leftBasePath, leftBasePathValid := effectiveSwaggerBasePath(left)
	rightBasePath, rightBasePathValid := effectiveSwaggerBasePath(right)
	if leftBasePath != rightBasePath || leftBasePathValid != rightBasePathValid {
		classification := Breaking
		if !leftBasePathValid || !rightBasePathValid {
			classification = Unknown
		}
		if err := collector.append(Change{
			kind: SwaggerBasePathChanged, classification: classification,
			pointer: "/basePath",
		}); err != nil {
			return err
		}
	}
	return nil
}

func compareSwaggerSchemes(
	collector *changeCollector,
	left jsonvalue.Value,
	right jsonvalue.Value,
) error {
	leftSchemes, leftPresent, leftValid := swaggerSchemes(left)
	rightSchemes, rightPresent, rightValid := swaggerSchemes(right)
	if leftPresent != rightPresent {
		return collector.append(Change{
			kind: SwaggerSchemesChanged, classification: Conditional,
			pointer: "/schemes",
		})
	}
	if !leftValid || !rightValid {
		if leftPresent == rightPresent &&
			equalOptionalMember(left, right, "schemes") {
			return nil
		}
		return collector.append(Change{
			kind: SwaggerSchemesChanged, classification: Unknown,
			pointer: "/schemes",
		})
	}
	leftByName := make(map[string]swaggerScheme, len(leftSchemes))
	rightByName := make(map[string]swaggerScheme, len(rightSchemes))
	for _, scheme := range leftSchemes {
		leftByName[scheme.name] = scheme
	}
	for _, scheme := range rightSchemes {
		rightByName[scheme.name] = scheme
	}
	for _, scheme := range leftSchemes {
		if _, exists := rightByName[scheme.name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: SwaggerSchemeRemoved, classification: Breaking,
			pointer: "/schemes/" + fmt.Sprint(scheme.index),
		}); err != nil {
			return err
		}
	}
	for _, scheme := range rightSchemes {
		if _, exists := leftByName[scheme.name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: SwaggerSchemeAdded, classification: Additive,
			pointer: "/schemes/" + fmt.Sprint(scheme.index),
		}); err != nil {
			return err
		}
	}
	return nil
}

func swaggerSchemes(root jsonvalue.Value) ([]swaggerScheme, bool, bool) {
	value, present := root.Lookup("schemes")
	if !present {
		return nil, false, true
	}
	elements, valid := value.Elements()
	if !valid {
		return nil, true, false
	}
	result := make([]swaggerScheme, 0, len(elements))
	seen := make(map[string]struct{}, len(elements))
	for index, element := range elements {
		name, text := element.Text()
		if !text {
			return nil, true, false
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, swaggerScheme{name: name, index: index})
	}
	return result, true, true
}

func effectiveSwaggerBasePath(root jsonvalue.Value) (string, bool) {
	value, present := root.Lookup("basePath")
	if !present {
		return "/", true
	}
	text, valid := value.Text()
	return text, valid
}

type boundedValue struct {
	value jsonvalue.Value
	depth int
}

func boundValue(
	ctx context.Context,
	root jsonvalue.Value,
	maxNodes int,
	maxDepth int,
) error {
	pending := []boundedValue{{value: root, depth: 1}}
	visited := 0
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		visited++
		if visited > maxNodes {
			return ErrLimitExceeded
		}
		childCount, _ := node.value.Length()
		if !diffChildrenFit(
			childCount, visited, len(pending), node.depth,
			maxNodes, maxDepth,
		) {
			return ErrLimitExceeded
		}
		switch node.value.Kind() {
		case jsonvalue.ArrayKind:
			elements, _ := node.value.Elements()
			for _, element := range slices.Backward(elements) {
				pending = append(pending, boundedValue{
					value: element, depth: node.depth + 1,
				})
			}
		case jsonvalue.ObjectKind:
			members, _ := node.value.Members()
			for _, member := range slices.Backward(members) {
				pending = append(pending, boundedValue{
					value: member.Value, depth: node.depth + 1,
				})
			}
		}
	}
	return nil
}

func diffChildrenFit(
	childCount int,
	visited int,
	queued int,
	depth int,
	maxNodes int,
	maxDepth int,
) bool {
	if childCount == 0 {
		return true
	}
	if depth >= maxDepth {
		return false
	}
	return childCount <= maxNodes-visited-queued
}

func collectCommonOperationContent(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	left []jsonvalue.Member,
	right []jsonvalue.Member,
	base string,
	dialect openapi.Dialect,
) error {
	rightContainers := memberValues(right)
	for _, leftContainer := range left {
		rightContainer, exists := rightContainers[leftContainer.Name]
		if !exists {
			continue
		}
		rightOperations := memberValues(operationMembers(rightContainer, dialect))
		for _, leftOperation := range operationMembers(leftContainer.Value, dialect) {
			rightOperation, exists := rightOperations[leftOperation.Name]
			if !exists {
				continue
			}
			pointer := operationPointer(base, leftContainer.Name, leftOperation.Name)
			containerPointer := base + "/" + escapePointer(leftContainer.Name)
			if err := compareParameters(
				collector,
				leftRoot,
				rightRoot,
				leftContainer.Value,
				leftOperation.Value,
				rightContainer,
				rightOperation,
				containerPointer,
				pointer,
				dialect,
			); err != nil {
				return err
			}
			if err := compareOperationContent(
				collector, leftRoot, rightRoot,
				leftContainer.Value, rightContainer,
				leftOperation.Value, rightOperation, pointer, dialect,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

type parameterEntry struct {
	key      string
	location string
	value    jsonvalue.Value
	pointer  string
}

type parameterSet struct {
	identified []parameterEntry
	unresolved []parameterEntry
	invalid    []string
}

func compareParameters(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftPathItem jsonvalue.Value,
	leftOperation jsonvalue.Value,
	rightPathItem jsonvalue.Value,
	rightOperation jsonvalue.Value,
	pathPointer string,
	operationPointer string,
	dialect openapi.Dialect,
) error {
	left := effectiveParameters(
		collector, leftRoot, leftPathItem, leftOperation,
		pathPointer, operationPointer, dialect, leftComparison,
	)
	right := effectiveParameters(
		collector, rightRoot, rightPathItem, rightOperation,
		pathPointer, operationPointer, dialect, rightComparison,
	)
	if !sameStrings(left.invalid, right.invalid) {
		var pointer string
		if len(right.invalid) > 0 {
			pointer = right.invalid[0]
		} else {
			pointer = left.invalid[0]
		}
		if err := collector.append(Change{
			kind: ParameterChanged, classification: Unknown, pointer: pointer,
		}); err != nil {
			return err
		}
	}
	rightByKey := make(map[string]parameterEntry, len(right.identified))
	leftByKey := make(map[string]parameterEntry, len(left.identified))
	for _, parameter := range right.identified {
		rightByKey[parameter.key] = parameter
	}
	for _, parameter := range left.identified {
		leftByKey[parameter.key] = parameter
		rightParameter, exists := rightByKey[parameter.key]
		if !exists {
			continue
		}
		leftRequired, leftValid := parameterRequired(parameter)
		rightRequired, rightValid := parameterRequired(rightParameter)
		if !leftValid || !rightValid {
			if leftValid != rightValid {
				if err := collector.append(Change{
					kind: ParameterChanged, classification: Unknown,
					pointer: rightParameter.pointer + "/required",
				}); err != nil {
					return err
				}
			}
			continue
		}
		if leftRequired == rightRequired {
			if err := compareParameterContractWithRoots(
				collector, leftRoot, rightRoot,
				parameter, rightParameter, dialect,
			); err != nil {
				return err
			}
			continue
		}
		kind := ParameterOptional
		classification := Compatible
		if rightRequired {
			kind = ParameterRequired
			classification = Breaking
		}
		if err := collector.append(Change{
			kind: kind, classification: classification,
			pointer: rightParameter.pointer + "/required",
		}); err != nil {
			return err
		}
		if err := compareParameterContractWithRoots(
			collector, leftRoot, rightRoot,
			parameter, rightParameter, dialect,
		); err != nil {
			return err
		}
	}
	for _, parameter := range left.identified {
		if _, exists := rightByKey[parameter.key]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: ParameterRemoved, classification: Conditional,
			pointer: parameter.pointer,
		}); err != nil {
			return err
		}
	}
	for _, parameter := range right.identified {
		if _, exists := leftByKey[parameter.key]; exists {
			continue
		}
		required, valid := parameterRequired(parameter)
		classification := Additive
		if !valid {
			classification = Unknown
		} else if required {
			classification = Breaking
		}
		if err := collector.append(Change{
			kind: ParameterAdded, classification: classification,
			pointer: parameter.pointer,
		}); err != nil {
			return err
		}
	}
	commonUnresolved := min(len(left.unresolved), len(right.unresolved))
	for index := 0; index < commonUnresolved; index++ {
		if left.unresolved[index].key == right.unresolved[index].key {
			continue
		}
		if err := collector.append(Change{
			kind: ParameterChanged, classification: Unknown,
			pointer: right.unresolved[index].pointer,
		}); err != nil {
			return err
		}
	}
	for _, parameter := range left.unresolved[commonUnresolved:] {
		if err := collector.append(Change{
			kind: ParameterRemoved, classification: Unknown,
			pointer: parameter.pointer,
		}); err != nil {
			return err
		}
	}
	for _, parameter := range right.unresolved[commonUnresolved:] {
		if err := collector.append(Change{
			kind: ParameterAdded, classification: Unknown,
			pointer: parameter.pointer,
		}); err != nil {
			return err
		}
	}
	return nil
}

func compareParameterContract(
	collector *changeCollector,
	left parameterEntry,
	right parameterEntry,
	dialect openapi.Dialect,
) error {
	emptyRoot, _ := jsonvalue.Object(nil)
	return compareParameterContractWithRoots(
		collector, emptyRoot, emptyRoot, left, right, dialect,
	)
}

func compareParameterContractWithRoots(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	left parameterEntry,
	right parameterEntry,
	dialect openapi.Dialect,
) error {
	if dialect == openapi.DialectSwagger20 {
		if err := compareTextSetting(
			collector, left.value, right.value, "collectionFormat", "csv",
			right.pointer, Breaking,
		); err != nil {
			return err
		}
		if err := compareBoolSetting(
			collector, left.value, right.value, "allowEmptyValue", false,
			right.pointer, Conditional,
		); err != nil {
			return err
		}
		if err := compareSwaggerParameterSchema(collector, left, right); err != nil {
			return err
		}
		return compareExtensions(
			collector, left.value, right.value, right.pointer,
		)
	}

	leftStyle, leftStyleValid := effectiveParameterStyle(left.value, left.location)
	rightStyle, rightStyleValid := effectiveParameterStyle(right.value, right.location)
	styleChanged := leftStyle != rightStyle || leftStyleValid != rightStyleValid
	if styleChanged {
		classification := Breaking
		if !leftStyleValid || !rightStyleValid {
			classification = Unknown
		}
		if err := collector.append(Change{
			kind: ParameterSerializationChanged, classification: classification,
			pointer: right.pointer + "/style",
		}); err != nil {
			return err
		}
	}
	leftExplode, leftExplodeValid := effectiveExplode(left.value, leftStyle, leftStyleValid)
	rightExplode, rightExplodeValid := effectiveExplode(right.value, rightStyle, rightStyleValid)
	if !styleChanged &&
		(leftExplode != rightExplode || leftExplodeValid != rightExplodeValid) {
		classification := Breaking
		if !leftExplodeValid || !rightExplodeValid {
			classification = Unknown
		}
		if err := collector.append(Change{
			kind: ParameterSerializationChanged, classification: classification,
			pointer: right.pointer + "/explode",
		}); err != nil {
			return err
		}
	}
	if err := compareBoolSetting(
		collector, left.value, right.value, "allowReserved", false,
		right.pointer, Conditional,
	); err != nil {
		return err
	}
	if !equalResolvedSchemaMember(
		collector, leftRoot, rightRoot,
		left.value, right.value, "schema", dialect,
	) {
		if err := collector.append(Change{
			kind: ParameterSchemaChanged, classification: Unknown,
			pointer: right.pointer + "/schema",
		}); err != nil {
			return err
		}
	}
	if !equalOptionalMember(left.value, right.value, "content") {
		if err := collector.append(Change{
			kind: ParameterContentChanged, classification: Unknown,
			pointer: right.pointer + "/content",
		}); err != nil {
			return err
		}
	}
	for _, name := range []string{"example", "examples"} {
		if equalOptionalMember(left.value, right.value, name) {
			continue
		}
		if err := collector.append(Change{
			kind: ParameterExampleChanged, classification: Conditional,
			pointer: right.pointer + "/" + name,
		}); err != nil {
			return err
		}
	}
	return compareExtensions(collector, left.value, right.value, right.pointer)
}

func compareTextSetting(
	collector *changeCollector,
	left jsonvalue.Value,
	right jsonvalue.Value,
	name string,
	defaultValue string,
	pointer string,
	classification Classification,
) error {
	leftValue, leftValid := effectiveText(left, name, defaultValue)
	rightValue, rightValid := effectiveText(right, name, defaultValue)
	if leftValue == rightValue && leftValid == rightValid {
		return nil
	}
	if !leftValid || !rightValid {
		classification = Unknown
	}
	return collector.append(Change{
		kind: ParameterSerializationChanged, classification: classification,
		pointer: pointer + "/" + name,
	})
}

func compareBoolSetting(
	collector *changeCollector,
	left jsonvalue.Value,
	right jsonvalue.Value,
	name string,
	defaultValue bool,
	pointer string,
	classification Classification,
) error {
	leftValue, leftValid := effectiveBool(left, name, defaultValue)
	rightValue, rightValid := effectiveBool(right, name, defaultValue)
	if leftValue == rightValue && leftValid == rightValid {
		return nil
	}
	if !leftValid || !rightValid {
		classification = Unknown
	}
	return collector.append(Change{
		kind: ParameterSerializationChanged, classification: classification,
		pointer: pointer + "/" + name,
	})
}

func effectiveParameterStyle(value jsonvalue.Value, location string) (string, bool) {
	defaultValue := ""
	switch location {
	case "query", "cookie":
		defaultValue = "form"
	case "path", "header":
		defaultValue = "simple"
	}
	return effectiveText(value, "style", defaultValue)
}

func effectiveExplode(
	value jsonvalue.Value,
	style string,
	styleValid bool,
) (bool, bool) {
	if !styleValid {
		return false, false
	}
	return effectiveBool(value, "explode", style == "form")
}

func effectiveText(value jsonvalue.Value, name string, fallback string) (string, bool) {
	member, present := value.Lookup(name)
	if !present {
		return fallback, true
	}
	text, valid := member.Text()
	return text, valid
}

func effectiveBool(value jsonvalue.Value, name string, fallback bool) (bool, bool) {
	member, present := value.Lookup(name)
	if !present {
		return fallback, true
	}
	boolean, valid := member.Bool()
	return boolean, valid
}

func compareSwaggerParameterSchema(
	collector *changeCollector,
	left parameterEntry,
	right parameterEntry,
) error {
	if !equalOptionalMember(left.value, right.value, "default") {
		if err := collector.append(Change{
			kind: ParameterDefaultChanged, classification: Conditional,
			pointer: right.pointer + "/default",
		}); err != nil {
			return err
		}
	}
	for _, name := range []string{
		"schema", "type", "format", "items", "maximum", "exclusiveMaximum",
		"minimum", "exclusiveMinimum", "maxLength", "minLength", "pattern",
		"maxItems", "minItems", "uniqueItems", "enum", "multipleOf",
	} {
		if equalOptionalMember(left.value, right.value, name) {
			continue
		}
		return collector.append(Change{
			kind: ParameterSchemaChanged, classification: Unknown,
			pointer: right.pointer + "/" + name,
		})
	}
	return nil
}

func equalOptionalMember(left jsonvalue.Value, right jsonvalue.Value, name string) bool {
	leftMember, leftPresent := left.Lookup(name)
	rightMember, rightPresent := right.Lookup(name)
	return leftPresent == rightPresent &&
		(!leftPresent || semanticValueEqual(leftMember, rightMember))
}

type valuePair struct {
	left  jsonvalue.Value
	right jsonvalue.Value
}

func semanticValueEqual(left jsonvalue.Value, right jsonvalue.Value) bool {
	pending := []valuePair{{left: left, right: right}}
	for len(pending) > 0 {
		pair := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if pair.left.Kind() != pair.right.Kind() {
			return false
		}
		switch pair.left.Kind() {
		case jsonvalue.NullKind:
		case jsonvalue.BooleanKind:
			leftValue, _ := pair.left.Bool()
			rightValue, _ := pair.right.Bool()
			if leftValue != rightValue {
				return false
			}
		case jsonvalue.NumberKind:
			leftValue, _ := pair.left.NumberText()
			rightValue, _ := pair.right.NumberText()
			if leftValue != rightValue {
				return false
			}
		case jsonvalue.StringKind:
			leftValue, _ := pair.left.Text()
			rightValue, _ := pair.right.Text()
			if leftValue != rightValue {
				return false
			}
		case jsonvalue.ArrayKind:
			leftValues, _ := pair.left.Elements()
			rightValues, _ := pair.right.Elements()
			if len(leftValues) != len(rightValues) {
				return false
			}
			for index := range leftValues {
				pending = append(pending, valuePair{
					left: leftValues[index], right: rightValues[index],
				})
			}
		case jsonvalue.ObjectKind:
			leftMembers, _ := pair.left.Members()
			rightMembers, _ := pair.right.Members()
			if len(leftMembers) != len(rightMembers) {
				return false
			}
			rightValues := memberValues(rightMembers)
			for _, member := range leftMembers {
				rightValue, exists := rightValues[member.Name]
				if !exists {
					return false
				}
				pending = append(pending, valuePair{
					left: member.Value, right: rightValue,
				})
			}
		default:
			return false
		}
	}
	return true
}

func effectiveParameters(
	collector *changeCollector,
	root jsonvalue.Value,
	pathItem jsonvalue.Value,
	operation jsonvalue.Value,
	pathPointer string,
	operationPointer string,
	dialect openapi.Dialect,
	side comparisonSide,
) parameterSet {
	result := parameterSet{}
	positions := make(map[string]int)
	appendParameters(
		collector, root, &result, positions, pathItem,
		pathPointer+"/parameters", dialect, side,
	)
	appendParameters(
		collector, root, &result, positions, operation,
		operationPointer+"/parameters", dialect, side,
	)
	return result
}

func appendParameters(
	collector *changeCollector,
	root jsonvalue.Value,
	result *parameterSet,
	positions map[string]int,
	owner jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
	side comparisonSide,
) {
	parameters, present := owner.Lookup("parameters")
	if !present {
		return
	}
	elements, valid := parameters.Elements()
	if !valid {
		result.invalid = append(result.invalid, pointer)
		return
	}
	for index, value := range elements {
		parameterPointer := pointer + "/" + fmt.Sprint(index)
		if resolved, valid := resolveInternalComparable(
			collector, root, value, dialect, side,
			referenceMetadataNames("parameters", dialect),
		); valid {
			value = resolved
		}
		key, location, identified := parameterIdentity(value)
		entry := parameterEntry{
			key: key, location: location, value: value, pointer: parameterPointer,
		}
		if !identified {
			result.unresolved = append(result.unresolved, entry)
			continue
		}
		if position, exists := positions[key]; exists {
			result.identified[position] = entry
			continue
		}
		positions[key] = len(result.identified)
		result.identified = append(result.identified, entry)
	}
}

func parameterIdentity(value jsonvalue.Value) (string, string, bool) {
	if reference, present := value.Lookup("$ref"); present {
		raw, valid := reference.Text()
		if !valid {
			return "invalid-ref", "", false
		}
		return "ref\x00" + raw, "", false
	}
	name, namePresent, nameValid := optionalText(value, "name")
	location, locationPresent, locationValid := optionalText(value, "in")
	if !namePresent || !nameValid || !locationPresent || !locationValid {
		return "", "", false
	}
	if location == "header" {
		name = strings.ToLower(name)
	}
	return location + "\x00" + name, location, true
}

func parameterRequired(parameter parameterEntry) (bool, bool) {
	if parameter.location == "path" {
		return true, true
	}
	return requiredValue(parameter.value)
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func compareOperationContent(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftPathItem jsonvalue.Value,
	rightPathItem jsonvalue.Value,
	left jsonvalue.Value,
	right jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
) error {
	if err := compareExtensions(collector, left, right, pointer); err != nil {
		return err
	}
	leftID, leftIDPresent, leftIDValid := optionalText(left, "operationId")
	rightID, rightIDPresent, rightIDValid := optionalText(right, "operationId")
	if leftIDPresent != rightIDPresent || leftIDValid != rightIDValid || leftID != rightID {
		if err := collector.append(Change{
			kind: OperationIDChanged, classification: Conditional,
			pointer: pointer + "/operationId",
		}); err != nil {
			return err
		}
	}
	if err := compareOperationTags(collector, left, right, pointer); err != nil {
		return err
	}
	if dialect != openapi.DialectSwagger20 {
		if err := compareRequestBody(
			collector, leftRoot, rightRoot, left, right, pointer, dialect,
		); err != nil {
			return err
		}
		if err := compareCallbacksWithRoots(
			collector, leftRoot, rightRoot, left, right, pointer, dialect,
		); err != nil {
			return err
		}
	} else if err := compareSwaggerMediaTypes(
		collector, leftRoot, rightRoot, left, right, pointer,
	); err != nil {
		return err
	}
	if err := compareEffectiveSecurity(
		collector, leftRoot, rightRoot, left, right, pointer,
	); err != nil {
		return err
	}
	if dialect != openapi.DialectSwagger20 {
		if err := compareEffectiveServers(
			collector, leftRoot, rightRoot, leftPathItem, rightPathItem,
			left, right, pointer,
		); err != nil {
			return err
		}
	}
	return compareResponses(
		collector, leftRoot, rightRoot, left, right, pointer, dialect,
	)
}

func compareOperationTags(
	collector *changeCollector,
	leftOperation jsonvalue.Value,
	rightOperation jsonvalue.Value,
	pointer string,
) error {
	leftValue, leftPresent := leftOperation.Lookup("tags")
	rightValue, rightPresent := rightOperation.Lookup("tags")
	left, leftValid := indexedStrings(leftValue, leftPresent)
	right, rightValid := indexedStrings(rightValue, rightPresent)
	if !leftValid || !rightValid {
		if leftPresent == rightPresent &&
			(!leftPresent || semanticValueEqual(leftValue, rightValue)) {
			return nil
		}
		return collector.append(Change{
			kind: TagChanged, classification: Unknown,
			pointer: pointer + "/tags",
		})
	}
	leftByName := make(map[string]swaggerScheme, len(left))
	rightByName := make(map[string]swaggerScheme, len(right))
	for _, tag := range left {
		leftByName[tag.name] = tag
	}
	for _, tag := range right {
		rightByName[tag.name] = tag
	}
	for _, tag := range left {
		if _, exists := rightByName[tag.name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: TagRemoved, classification: Conditional,
			pointer: pointer + "/tags/" + fmt.Sprint(tag.index),
		}); err != nil {
			return err
		}
	}
	for _, tag := range right {
		if _, exists := leftByName[tag.name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: TagAdded, classification: Conditional,
			pointer: pointer + "/tags/" + fmt.Sprint(tag.index),
		}); err != nil {
			return err
		}
	}
	return nil
}

func compareSwaggerMediaTypes(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftOperation jsonvalue.Value,
	rightOperation jsonvalue.Value,
	pointer string,
) error {
	if err := compareEffectiveSwaggerStringSet(
		collector, leftRoot, rightRoot, leftOperation, rightOperation,
		"consumes", pointer, RequestMediaTypeRemoved, RequestMediaTypeAdded,
		RequestMediaTypesChanged,
	); err != nil {
		return err
	}
	return compareEffectiveSwaggerStringSet(
		collector, leftRoot, rightRoot, leftOperation, rightOperation,
		"produces", pointer, ResponseMediaTypeRemoved, ResponseMediaTypeAdded,
		ResponseMediaTypesChanged,
	)
}

func compareEffectiveSwaggerStringSet(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftOperation jsonvalue.Value,
	rightOperation jsonvalue.Value,
	name string,
	pointer string,
	removed Kind,
	added Kind,
	changed Kind,
) error {
	leftValue, leftPresent := inheritedSwaggerValue(leftRoot, leftOperation, name)
	rightValue, rightPresent := inheritedSwaggerValue(rightRoot, rightOperation, name)
	if leftPresent != rightPresent {
		return collector.append(Change{
			kind: changed, classification: Conditional,
			pointer: pointer + "/" + name,
		})
	}
	leftValues, leftValid := indexedStrings(leftValue, leftPresent)
	rightValues, rightValid := indexedStrings(rightValue, rightPresent)
	if !leftValid || !rightValid {
		if leftPresent == rightPresent &&
			(!leftPresent || semanticValueEqual(leftValue, rightValue)) {
			return nil
		}
		return collector.append(Change{
			kind: changed, classification: Unknown,
			pointer: pointer + "/" + name,
		})
	}
	leftByName := make(map[string]swaggerScheme, len(leftValues))
	rightByName := make(map[string]swaggerScheme, len(rightValues))
	for _, value := range leftValues {
		leftByName[value.name] = value
	}
	for _, value := range rightValues {
		rightByName[value.name] = value
	}
	for _, value := range leftValues {
		if _, exists := rightByName[value.name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: removed, classification: Breaking,
			pointer: pointer + "/" + name + "/" + fmt.Sprint(value.index),
		}); err != nil {
			return err
		}
	}
	for _, value := range rightValues {
		if _, exists := leftByName[value.name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: added, classification: Additive,
			pointer: pointer + "/" + name + "/" + fmt.Sprint(value.index),
		}); err != nil {
			return err
		}
	}
	return nil
}

func inheritedSwaggerValue(
	root jsonvalue.Value,
	operation jsonvalue.Value,
	name string,
) (jsonvalue.Value, bool) {
	if value, present := operation.Lookup(name); present {
		return value, true
	}
	return root.Lookup(name)
}

func indexedStrings(value jsonvalue.Value, present bool) ([]swaggerScheme, bool) {
	if !present {
		return nil, true
	}
	elements, valid := value.Elements()
	if !valid {
		return nil, false
	}
	result := make([]swaggerScheme, 0, len(elements))
	seen := make(map[string]struct{}, len(elements))
	for index, element := range elements {
		name, text := element.Text()
		if !text {
			return nil, false
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, swaggerScheme{name: name, index: index})
	}
	return result, true
}

func compareCallbacks(
	collector *changeCollector,
	leftOperation jsonvalue.Value,
	rightOperation jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
) error {
	emptyRoot, _ := jsonvalue.Object(nil)
	return compareCallbacksWithRoots(
		collector, emptyRoot, emptyRoot,
		leftOperation, rightOperation, pointer, dialect,
	)
}

func compareCallbacksWithRoots(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftOperation jsonvalue.Value,
	rightOperation jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
) error {
	left, leftPresent, leftValid := optionalObjectMembers(leftOperation, "callbacks")
	right, rightPresent, rightValid := optionalObjectMembers(rightOperation, "callbacks")
	if !leftValid || !rightValid {
		leftValue, _ := leftOperation.Lookup("callbacks")
		rightValue, _ := rightOperation.Lookup("callbacks")
		if leftPresent == rightPresent && semanticValueEqual(leftValue, rightValue) {
			return nil
		}
		return collector.append(Change{
			kind: CallbackChanged, classification: Unknown,
			pointer: pointer + "/callbacks",
		})
	}
	leftCallbacks, _ := leftOperation.Lookup("callbacks")
	rightCallbacks, _ := rightOperation.Lookup("callbacks")
	if err := compareExtensions(
		collector, leftCallbacks, rightCallbacks, pointer+"/callbacks",
	); err != nil {
		return err
	}
	left = withoutExtensions(left)
	right = withoutExtensions(right)
	rightByName := memberValues(right)
	leftNames := memberNames(left)
	for _, callback := range left {
		if _, exists := rightByName[callback.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: CallbackRemoved, classification: Conditional,
			pointer: pointer + "/callbacks/" + escapePointer(callback.Name),
		}); err != nil {
			return err
		}
	}
	for _, leftCallback := range left {
		rightCallback, exists := rightByName[leftCallback.Name]
		if !exists {
			continue
		}
		callbackPointer := pointer + "/callbacks/" + escapePointer(leftCallback.Name)
		leftResolved, leftValid := resolveInternalComparable(
			collector, leftRoot, leftCallback.Value, dialect, leftComparison,
			referenceMetadataNames("callbacks", dialect),
		)
		rightResolved, rightValid := resolveInternalComparable(
			collector, rightRoot, rightCallback, dialect, rightComparison,
			referenceMetadataNames("callbacks", dialect),
		)
		if !leftValid || !rightValid {
			if !leftValid && !rightValid &&
				semanticValueEqual(leftCallback.Value, rightCallback) {
				continue
			}
			if err := collector.append(Change{
				kind: CallbackChanged, classification: Unknown,
				pointer: callbackPointer,
			}); err != nil {
				return err
			}
			continue
		}
		leftCallback.Value = leftResolved
		rightCallback = rightResolved
		if err := compareExtensions(
			collector, leftCallback.Value, rightCallback, callbackPointer,
		); err != nil {
			return err
		}
		if err := compareCallbackExpressionsWithRoots(
			collector, leftRoot, rightRoot, leftCallback.Value, rightCallback,
			callbackPointer, dialect,
		); err != nil {
			return err
		}
	}
	for _, callback := range right {
		if _, exists := leftNames[callback.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: CallbackAdded, classification: Conditional,
			pointer: pointer + "/callbacks/" + escapePointer(callback.Name),
		}); err != nil {
			return err
		}
	}
	return nil
}

func compareCallbackExpressions(
	collector *changeCollector,
	leftCallback jsonvalue.Value,
	rightCallback jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
) error {
	emptyRoot, _ := jsonvalue.Object(nil)
	return compareCallbackExpressionsWithRoots(
		collector, emptyRoot, emptyRoot,
		leftCallback, rightCallback, pointer, dialect,
	)
}

func compareCallbackExpressionsWithRoots(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftCallback jsonvalue.Value,
	rightCallback jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
) error {
	left := callbackMembers(leftCallback)
	right := callbackMembers(rightCallback)
	rightByExpression := memberValues(right)
	leftExpressions := memberNames(left)
	for _, expression := range left {
		if _, exists := rightByExpression[expression.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: CallbackExpressionRemoved, classification: Conditional,
			pointer: pointer + "/" + escapePointer(expression.Name),
		}); err != nil {
			return err
		}
	}
	for _, leftExpression := range left {
		rightExpression, exists := rightByExpression[leftExpression.Name]
		if !exists {
			continue
		}
		expressionPointer := pointer + "/" + escapePointer(leftExpression.Name)
		leftResolved, leftValid := resolveInternalComparable(
			collector, leftRoot, leftExpression.Value, dialect, leftComparison,
			referenceMetadataNames("pathItems", dialect),
		)
		rightResolved, rightValid := resolveInternalComparable(
			collector, rightRoot, rightExpression, dialect, rightComparison,
			referenceMetadataNames("pathItems", dialect),
		)
		if !leftValid || !rightValid {
			if !leftValid && !rightValid &&
				semanticValueEqual(leftExpression.Value, rightExpression) {
				continue
			}
			if err := collector.append(Change{
				kind: CallbackExpressionChanged, classification: Unknown,
				pointer: expressionPointer,
			}); err != nil {
				return err
			}
			continue
		}
		if err := compareCallbackOperationsWithRoots(
			collector, leftRoot, rightRoot, leftResolved, rightResolved,
			expressionPointer, dialect,
		); err != nil {
			return err
		}
	}
	for _, expression := range right {
		if _, exists := leftExpressions[expression.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: CallbackExpressionAdded, classification: Conditional,
			pointer: pointer + "/" + escapePointer(expression.Name),
		}); err != nil {
			return err
		}
	}
	return nil
}

func compareCallbackOperations(
	collector *changeCollector,
	leftPathItem jsonvalue.Value,
	rightPathItem jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
) error {
	emptyRoot, _ := jsonvalue.Object(nil)
	return compareCallbackOperationsWithRoots(
		collector, emptyRoot, emptyRoot,
		leftPathItem, rightPathItem, pointer, dialect,
	)
}

func compareCallbackOperationsWithRoots(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftPathItem jsonvalue.Value,
	rightPathItem jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
) error {
	if err := compareExtensions(
		collector, leftPathItem, rightPathItem, pointer,
	); err != nil {
		return err
	}
	left := operationMembers(leftPathItem, dialect)
	right := operationMembers(rightPathItem, dialect)
	rightNames := memberNames(right)
	leftNames := memberNames(left)
	for _, operation := range left {
		if _, exists := rightNames[operation.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: CallbackOperationRemoved, classification: Conditional,
			pointer: pointer + "/" + operation.Name,
		}); err != nil {
			return err
		}
	}
	rightByName := memberValues(right)
	for _, leftOperation := range left {
		rightOperation, exists := rightByName[leftOperation.Name]
		if !exists {
			continue
		}
		operationLocation := pointer + "/" + leftOperation.Name
		if err := compareParameters(
			collector, leftRoot, rightRoot,
			leftPathItem, leftOperation.Value,
			rightPathItem, rightOperation, pointer, operationLocation, dialect,
		); err != nil {
			return err
		}
		if err := compareOperationContent(
			collector, leftRoot, rightRoot, leftPathItem, rightPathItem,
			leftOperation.Value, rightOperation, operationLocation, dialect,
		); err != nil {
			return err
		}
	}
	for _, operation := range right {
		if _, exists := leftNames[operation.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: CallbackOperationAdded, classification: Conditional,
			pointer: pointer + "/" + operation.Name,
		}); err != nil {
			return err
		}
	}
	return nil
}

func optionalObjectMembers(
	owner jsonvalue.Value,
	name string,
) ([]jsonvalue.Member, bool, bool) {
	value, present := owner.Lookup(name)
	if !present {
		return nil, false, true
	}
	members, valid := value.Members()
	return members, true, valid
}

func callbackMembers(value jsonvalue.Value) []jsonvalue.Member {
	members, _ := value.Members()
	return withoutExtensions(members)
}

func withoutExtensions(members []jsonvalue.Member) []jsonvalue.Member {
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		if strings.HasPrefix(strings.ToLower(member.Name), "x-") {
			continue
		}
		result = append(result, member)
	}
	return result
}

type serverContract struct {
	url              string
	variables        jsonvalue.Value
	variablesPresent bool
	index            int
}

func compareEffectiveServers(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftPathItem jsonvalue.Value,
	rightPathItem jsonvalue.Value,
	leftOperation jsonvalue.Value,
	rightOperation jsonvalue.Value,
	pointer string,
) error {
	leftValue, leftPresent := effectiveServers(leftRoot, leftPathItem, leftOperation)
	rightValue, rightPresent := effectiveServers(rightRoot, rightPathItem, rightOperation)
	if leftPresent == rightPresent &&
		(!leftPresent || semanticValueEqual(leftValue, rightValue)) {
		return nil
	}
	left, leftValid := normalizeServers(leftValue, leftPresent)
	right, rightValid := normalizeServers(rightValue, rightPresent)
	if !leftValid || !rightValid {
		return collector.append(Change{
			kind: ServerChanged, classification: Unknown,
			pointer: pointer + "/servers",
		})
	}
	rightByURL := make(map[string]serverContract, len(right))
	leftByURL := make(map[string]serverContract, len(left))
	for _, server := range right {
		rightByURL[server.url] = server
	}
	for _, server := range left {
		leftByURL[server.url] = server
		rightServer, exists := rightByURL[server.url]
		if !exists {
			if err := collector.append(Change{
				kind: ServerRemoved, classification: Breaking,
				pointer: pointer + "/servers/" + fmt.Sprint(server.index),
			}); err != nil {
				return err
			}
			continue
		}
		if equalServerVariables(server, rightServer) {
			continue
		}
		if err := collector.append(Change{
			kind: ServerChanged, classification: Conditional,
			pointer: pointer + "/servers/" + fmt.Sprint(rightServer.index),
		}); err != nil {
			return err
		}
	}
	for _, server := range right {
		if _, exists := leftByURL[server.url]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: ServerAdded, classification: Additive,
			pointer: pointer + "/servers/" + fmt.Sprint(server.index),
		}); err != nil {
			return err
		}
	}
	if len(left) == len(right) && sameServerURLs(left, right) {
		return nil
	}
	if len(left) == len(right) && sameServerURLSet(leftByURL, rightByURL) {
		return collector.append(Change{
			kind: ServerOrderChanged, classification: Conditional,
			pointer: pointer + "/servers",
		})
	}
	return nil
}

func effectiveServers(
	root jsonvalue.Value,
	pathItem jsonvalue.Value,
	operation jsonvalue.Value,
) (jsonvalue.Value, bool) {
	if value, present := operation.Lookup("servers"); present {
		return value, true
	}
	if value, present := pathItem.Lookup("servers"); present {
		return value, true
	}
	return root.Lookup("servers")
}

func normalizeServers(value jsonvalue.Value, present bool) ([]serverContract, bool) {
	if !present {
		return []serverContract{{url: "/", index: 0}}, true
	}
	elements, valid := value.Elements()
	if !valid || len(elements) == 0 {
		return nil, false
	}
	result := make([]serverContract, 0, len(elements))
	seen := make(map[string]struct{}, len(elements))
	for index, element := range elements {
		urlValue, urlPresent := element.Lookup("url")
		url, urlValid := urlValue.Text()
		if !urlPresent || !urlValid {
			return nil, false
		}
		if _, duplicate := seen[url]; duplicate {
			return nil, false
		}
		seen[url] = struct{}{}
		variables, variablesPresent := element.Lookup("variables")
		if variablesPresent && variables.Kind() != jsonvalue.ObjectKind {
			return nil, false
		}
		result = append(result, serverContract{
			url: url, variables: variables, variablesPresent: variablesPresent,
			index: index,
		})
	}
	return result, true
}

func equalServerVariables(left serverContract, right serverContract) bool {
	return left.variablesPresent == right.variablesPresent &&
		(!left.variablesPresent || semanticValueEqual(left.variables, right.variables))
}

func sameServerURLs(left []serverContract, right []serverContract) bool {
	for index := range left {
		if left[index].url != right[index].url {
			return false
		}
	}
	return true
}

func sameServerURLSet(
	left map[string]serverContract,
	right map[string]serverContract,
) bool {
	for url := range left {
		if _, exists := right[url]; !exists {
			return false
		}
	}
	return true
}

type securityState uint8

const (
	securityAnonymous securityState = iota
	securityAuthenticated
)

func compareEffectiveSecurity(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftOperation jsonvalue.Value,
	rightOperation jsonvalue.Value,
	pointer string,
) error {
	left, leftPresent := effectiveSecurity(leftRoot, leftOperation)
	right, rightPresent := effectiveSecurity(rightRoot, rightOperation)
	if leftPresent == rightPresent &&
		(!leftPresent || semanticValueEqual(left, right)) {
		return nil
	}
	leftState, leftKey, leftValid := normalizeSecurity(left, leftPresent)
	rightState, rightKey, rightValid := normalizeSecurity(right, rightPresent)
	if leftValid && rightValid && leftState == rightState && leftKey == rightKey {
		return nil
	}
	change := Change{
		kind: SecurityChanged, classification: Conditional,
		pointer: pointer + "/security",
	}
	if !leftValid || !rightValid {
		change.classification = Unknown
	} else if leftState != rightState {
		if rightState == securityAuthenticated {
			change.kind = SecurityRequired
			change.classification = Breaking
		} else {
			change.kind = SecurityOptional
			change.classification = Compatible
		}
	}
	return collector.append(change)
}

func effectiveSecurity(
	root jsonvalue.Value,
	operation jsonvalue.Value,
) (jsonvalue.Value, bool) {
	if value, present := operation.Lookup("security"); present {
		return value, true
	}
	return root.Lookup("security")
}

func normalizeSecurity(
	value jsonvalue.Value,
	present bool,
) (securityState, string, bool) {
	if !present {
		return securityAnonymous, "", true
	}
	requirements, valid := value.Elements()
	if !valid {
		return securityAnonymous, "", false
	}
	if len(requirements) == 0 {
		return securityAnonymous, "", true
	}
	normalized := make(map[string]struct{}, len(requirements))
	for _, requirement := range requirements {
		members, object := requirement.Members()
		if !object {
			return securityAnonymous, "", false
		}
		if len(members) == 0 {
			return securityAnonymous, "", true
		}
		schemes := make([]string, 0, len(members))
		for _, member := range members {
			scopes, array := member.Value.Elements()
			if !array {
				return securityAnonymous, "", false
			}
			scopeNames := make([]string, 0, len(scopes))
			for _, scope := range scopes {
				name, text := scope.Text()
				if !text {
					return securityAnonymous, "", false
				}
				scopeNames = append(scopeNames, name)
			}
			sort.Strings(scopeNames)
			schemes = append(schemes, member.Name+"\x00"+strings.Join(scopeNames, "\x00"))
		}
		sort.Strings(schemes)
		normalized[strings.Join(schemes, "\x01")] = struct{}{}
	}
	alternatives := make([]string, 0, len(normalized))
	for alternative := range normalized {
		alternatives = append(alternatives, alternative)
	}
	sort.Strings(alternatives)
	return securityAuthenticated, strings.Join(alternatives, "\x02"), true
}

func compareRequestBody(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftOperation jsonvalue.Value,
	rightOperation jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
) error {
	left, leftPresent := leftOperation.Lookup("requestBody")
	right, rightPresent := rightOperation.Lookup("requestBody")
	switch {
	case !leftPresent && rightPresent:
		required, valid := requiredValue(right)
		classification := Additive
		if !valid {
			classification = Unknown
		} else if required {
			classification = Breaking
		}
		return collector.append(Change{
			kind: RequestBodyAdded, classification: classification,
			pointer: pointer + "/requestBody",
		})
	case leftPresent && !rightPresent:
		return collector.append(Change{
			kind: RequestBodyRemoved, classification: Conditional,
			pointer: pointer + "/requestBody",
		})
	case !leftPresent:
		return nil
	}
	leftOriginal := left
	rightOriginal := right
	leftResolved, leftValid := resolveInternalComparable(
		collector, leftRoot, left, dialect, leftComparison,
		referenceMetadataNames("requestBodies", dialect),
	)
	rightResolved, rightValid := resolveInternalComparable(
		collector, rightRoot, right, dialect, rightComparison,
		referenceMetadataNames("requestBodies", dialect),
	)
	if !leftValid || !rightValid {
		if !leftValid && !rightValid &&
			semanticValueEqual(leftOriginal, rightOriginal) {
			return nil
		}
		return collector.append(Change{
			kind: RequestBodyChanged, classification: Unknown,
			pointer: pointer + "/requestBody",
		})
	}
	left = leftResolved
	right = rightResolved
	if err := compareExtensions(
		collector, left, right, pointer+"/requestBody",
	); err != nil {
		return err
	}
	leftRequired, leftValid := requiredValue(left)
	rightRequired, rightValid := requiredValue(right)
	if !leftValid || !rightValid {
		if leftValid != rightValid {
			return collector.append(Change{
				kind: RequestBodyChanged, classification: Unknown,
				pointer: pointer + "/requestBody/required",
			})
		}
	} else if leftRequired != rightRequired {
		kind := RequestBodyOptional
		classification := Compatible
		if rightRequired {
			kind = RequestBodyRequired
			classification = Breaking
		}
		if err := collector.append(Change{
			kind: kind, classification: classification,
			pointer: pointer + "/requestBody/required",
		}); err != nil {
			return err
		}
	}
	leftContent := contentMembers(left)
	rightContent := contentMembers(right)
	if err := compareMediaTypes(
		collector,
		leftContent,
		rightContent,
		pointer+"/requestBody/content",
		RequestMediaTypeRemoved,
		RequestMediaTypeAdded,
	); err != nil {
		return err
	}
	if err := compareCommonMediaTypeSchemas(
		collector, leftRoot, rightRoot, leftContent, rightContent,
		pointer+"/requestBody/content", schemaInRequest, dialect,
	); err != nil {
		return err
	}
	return compareCommonMediaTypeMetadata(
		collector, leftContent, rightContent,
		pointer+"/requestBody/content", schemaInRequest,
	)
}

func compareResponses(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftOperation jsonvalue.Value,
	rightOperation jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
) error {
	leftResponses := namedObjectMembers(leftOperation, "responses")
	rightResponses := namedObjectMembers(rightOperation, "responses")
	rightByName := memberValues(rightResponses)
	leftNames := memberNames(leftResponses)
	for _, response := range leftResponses {
		if _, exists := rightByName[response.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: ResponseRemoved, classification: Conditional,
			pointer: pointer + "/responses/" + escapePointer(response.Name),
		}); err != nil {
			return err
		}
	}
	for _, response := range rightResponses {
		if _, exists := leftNames[response.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: ResponseAdded, classification: Conditional,
			pointer: pointer + "/responses/" + escapePointer(response.Name),
		}); err != nil {
			return err
		}
	}
	for _, leftResponse := range leftResponses {
		rightResponse, exists := rightByName[leftResponse.Name]
		if !exists {
			continue
		}
		responsePointer := pointer + "/responses/" + escapePointer(leftResponse.Name)
		leftOriginal := leftResponse.Value
		rightOriginal := rightResponse
		leftResolved, leftValid := resolveInternalComparable(
			collector, leftRoot, leftOriginal, dialect, leftComparison,
			referenceMetadataNames("responses", dialect),
		)
		rightResolved, rightValid := resolveInternalComparable(
			collector, rightRoot, rightOriginal, dialect, rightComparison,
			referenceMetadataNames("responses", dialect),
		)
		if !leftValid || !rightValid {
			if !leftValid && !rightValid &&
				semanticValueEqual(leftOriginal, rightOriginal) {
				continue
			}
			if err := collector.append(Change{
				kind: ResponseChanged, classification: Unknown,
				pointer: responsePointer,
			}); err != nil {
				return err
			}
			continue
		}
		leftResponse.Value = leftResolved
		rightResponse = rightResolved
		if err := compareExtensions(
			collector, leftResponse.Value, rightResponse, responsePointer,
		); err != nil {
			return err
		}
		if dialect == openapi.DialectSwagger20 {
			if err := compareSwaggerResponseContract(
				collector, leftRoot, rightRoot,
				leftResponse.Value, rightResponse, responsePointer, dialect,
			); err != nil {
				return err
			}
			continue
		}
		if err := compareResponseHeaders(
			collector, leftRoot, rightRoot,
			leftResponse.Value, rightResponse, responsePointer, dialect,
		); err != nil {
			return err
		}
		if err := compareResponseLinksWithRoots(
			collector, leftRoot, rightRoot,
			leftResponse.Value, rightResponse, responsePointer, dialect,
		); err != nil {
			return err
		}
		leftContent := contentMembers(leftResponse.Value)
		rightContent := contentMembers(rightResponse)
		if err := compareMediaTypes(
			collector,
			leftContent,
			rightContent,
			responsePointer+"/content",
			ResponseMediaTypeRemoved,
			ResponseMediaTypeAdded,
		); err != nil {
			return err
		}
		if err := compareCommonMediaTypeSchemas(
			collector, leftRoot, rightRoot, leftContent, rightContent,
			responsePointer+"/content", schemaInResponse, dialect,
		); err != nil {
			return err
		}
		if err := compareCommonMediaTypeMetadata(
			collector, leftContent, rightContent,
			responsePointer+"/content", schemaInResponse,
		); err != nil {
			return err
		}
	}
	return nil
}

func compareSwaggerResponseContract(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	left jsonvalue.Value,
	right jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
) error {
	leftSchema, leftPresent := left.Lookup("schema")
	rightSchema, rightPresent := right.Lookup("schema")
	leftResolved := leftSchema
	rightResolved := rightSchema
	leftValid := true
	rightValid := true
	if leftPresent {
		leftResolved, leftValid = resolveInternalSchema(
			collector, leftRoot, leftSchema, dialect, leftComparison,
		)
	}
	if rightPresent {
		rightResolved, rightValid = resolveInternalSchema(
			collector, rightRoot, rightSchema, dialect, rightComparison,
		)
	}
	equalSchema := leftPresent == rightPresent &&
		(!leftPresent || (leftValid && rightValid &&
			semanticValueEqual(leftResolved, rightResolved)) ||
			(!leftValid && !rightValid &&
				semanticValueEqual(leftSchema, rightSchema)))
	if !equalSchema {
		kind, classification := classifyMediaTypeSchema(
			leftResolved, leftPresent, rightResolved, rightPresent,
			schemaInResponse,
		)
		if !leftValid || !rightValid {
			classification = Unknown
		}
		if err := collector.append(Change{
			kind: kind, classification: classification,
			pointer: pointer + "/schema",
		}); err != nil {
			return err
		}
	}
	if err := compareResponseHeaders(
		collector, leftRoot, rightRoot, left, right, pointer, dialect,
	); err != nil {
		return err
	}
	return compareSwaggerResponseExamples(collector, left, right, pointer)
}

func compareResponseHeaders(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftResponse jsonvalue.Value,
	rightResponse jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
) error {
	left := namedObjectMembers(leftResponse, "headers")
	right := namedObjectMembers(rightResponse, "headers")
	leftByName := caseInsensitiveMemberValues(left)
	rightByName := caseInsensitiveMemberValues(right)
	for _, header := range left {
		name := strings.ToLower(header.Name)
		rightHeader, exists := rightByName[name]
		if !exists {
			if err := collector.append(Change{
				kind: ResponseHeaderRemoved, classification: Breaking,
				pointer: pointer + "/headers/" + escapePointer(header.Name),
			}); err != nil {
				return err
			}
			continue
		}
		leftResolved, leftValid := resolveInternalComparable(
			collector, leftRoot, header.Value, dialect, leftComparison,
			referenceMetadataNames("headers", dialect),
		)
		rightResolved, rightValid := resolveInternalComparable(
			collector, rightRoot, rightHeader.Value, dialect, rightComparison,
			referenceMetadataNames("headers", dialect),
		)
		headerPointer := pointer + "/headers/" + escapePointer(rightHeader.Name)
		if leftValid && rightValid {
			if err := compareExtensions(
				collector, leftResolved, rightResolved, headerPointer,
			); err != nil {
				return err
			}
		}
		if leftValid && rightValid &&
			equalObjectWithoutExtensions(leftResolved, rightResolved) {
			continue
		}
		if !leftValid && !rightValid &&
			semanticValueEqual(header.Value, rightHeader.Value) {
			continue
		}
		if err := collector.append(Change{
			kind: ResponseHeaderChanged, classification: Unknown,
			pointer: headerPointer,
		}); err != nil {
			return err
		}
	}
	for _, header := range right {
		if _, exists := leftByName[strings.ToLower(header.Name)]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: ResponseHeaderAdded, classification: Compatible,
			pointer: pointer + "/headers/" + escapePointer(header.Name),
		}); err != nil {
			return err
		}
	}
	return nil
}

func compareSwaggerResponseExamples(
	collector *changeCollector,
	leftResponse jsonvalue.Value,
	rightResponse jsonvalue.Value,
	pointer string,
) error {
	left := namedObjectMembers(leftResponse, "examples")
	right := namedObjectMembers(rightResponse, "examples")
	leftByName := memberValues(left)
	rightByName := memberValues(right)
	for _, example := range left {
		replacement, exists := rightByName[example.Name]
		if exists && semanticValueEqual(example.Value, replacement) {
			continue
		}
		if err := collector.append(Change{
			kind: ResponseExampleChanged, classification: Conditional,
			pointer: pointer + "/examples/" + escapePointer(example.Name),
		}); err != nil {
			return err
		}
	}
	for _, example := range right {
		if _, exists := leftByName[example.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: ResponseExampleChanged, classification: Conditional,
			pointer: pointer + "/examples/" + escapePointer(example.Name),
		}); err != nil {
			return err
		}
	}
	return nil
}

func caseInsensitiveMemberValues(
	members []jsonvalue.Member,
) map[string]jsonvalue.Member {
	result := make(map[string]jsonvalue.Member, len(members))
	for _, member := range members {
		result[strings.ToLower(member.Name)] = member
	}
	return result
}

func compareCommonMediaTypeMetadata(
	collector *changeCollector,
	left []jsonvalue.Member,
	right []jsonvalue.Member,
	pointer string,
	direction schemaDirection,
) error {
	rightValues := memberValues(right)
	for _, leftMediaType := range left {
		rightMediaType, exists := rightValues[leftMediaType.Name]
		if !exists {
			continue
		}
		mediaPointer := pointer + "/" + escapePointer(leftMediaType.Name)
		if err := compareExtensions(
			collector, leftMediaType.Value, rightMediaType, mediaPointer,
		); err != nil {
			return err
		}
		if !equalOptionalMember(leftMediaType.Value, rightMediaType, "encoding") {
			kind := RequestEncodingChanged
			if direction == schemaInResponse {
				kind = ResponseEncodingChanged
			}
			classification := Breaking
			leftEncoding, leftPresent := leftMediaType.Value.Lookup("encoding")
			rightEncoding, rightPresent := rightMediaType.Lookup("encoding")
			if (leftPresent && leftEncoding.Kind() != jsonvalue.ObjectKind) ||
				(rightPresent && rightEncoding.Kind() != jsonvalue.ObjectKind) {
				classification = Unknown
			}
			if err := collector.append(Change{
				kind: kind, classification: classification,
				pointer: mediaPointer + "/encoding",
			}); err != nil {
				return err
			}
		}
		exampleChanged := !equalOptionalMember(
			leftMediaType.Value, rightMediaType, "example",
		)
		examplesChanged := !equalOptionalMember(
			leftMediaType.Value, rightMediaType, "examples",
		)
		if !exampleChanged && !examplesChanged {
			continue
		}
		kind := RequestExampleChanged
		if direction == schemaInResponse {
			kind = ResponseExampleChanged
		}
		name := "example"
		if !exampleChanged {
			name = "examples"
		}
		if err := collector.append(Change{
			kind: kind, classification: Conditional,
			pointer: mediaPointer + "/" + name,
		}); err != nil {
			return err
		}
	}
	return nil
}

func compareResponseLinks(
	collector *changeCollector,
	leftResponse jsonvalue.Value,
	rightResponse jsonvalue.Value,
	pointer string,
) error {
	emptyRoot, _ := jsonvalue.Object(nil)
	return compareResponseLinksWithRoots(
		collector, emptyRoot, emptyRoot,
		leftResponse, rightResponse, pointer, openapi.DialectOAS32,
	)
}

func compareResponseLinksWithRoots(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	leftResponse jsonvalue.Value,
	rightResponse jsonvalue.Value,
	pointer string,
	dialect openapi.Dialect,
) error {
	left, leftPresent, leftValid := optionalObjectMembers(leftResponse, "links")
	right, rightPresent, rightValid := optionalObjectMembers(rightResponse, "links")
	if !leftValid || !rightValid {
		leftValue, _ := leftResponse.Lookup("links")
		rightValue, _ := rightResponse.Lookup("links")
		if leftPresent == rightPresent && semanticValueEqual(leftValue, rightValue) {
			return nil
		}
		return collector.append(Change{
			kind: LinkChanged, classification: Unknown,
			pointer: pointer + "/links",
		})
	}
	leftLinks, _ := leftResponse.Lookup("links")
	rightLinks, _ := rightResponse.Lookup("links")
	if err := compareExtensions(
		collector, leftLinks, rightLinks, pointer+"/links",
	); err != nil {
		return err
	}
	left = withoutExtensions(left)
	right = withoutExtensions(right)
	rightByName := memberValues(right)
	leftNames := memberNames(left)
	for _, link := range left {
		if _, exists := rightByName[link.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: LinkRemoved, classification: Conditional,
			pointer: pointer + "/links/" + escapePointer(link.Name),
		}); err != nil {
			return err
		}
	}
	for _, leftLink := range left {
		rightLink, exists := rightByName[leftLink.Name]
		if !exists {
			continue
		}
		leftResolved, leftValid := resolveInternalComparable(
			collector, leftRoot, leftLink.Value, dialect, leftComparison,
			referenceMetadataNames("links", dialect),
		)
		rightResolved, rightValid := resolveInternalComparable(
			collector, rightRoot, rightLink, dialect, rightComparison,
			referenceMetadataNames("links", dialect),
		)
		linkPointer := pointer + "/links/" + escapePointer(leftLink.Name)
		if leftValid && rightValid {
			if err := compareExtensions(
				collector, leftResolved, rightResolved, linkPointer,
			); err != nil {
				return err
			}
		}
		if leftValid && rightValid &&
			equalObjectWithoutExtensions(leftResolved, rightResolved) {
			continue
		}
		classification := Conditional
		if !leftValid || !rightValid {
			if !leftValid && !rightValid &&
				equalObjectWithoutExtensions(leftLink.Value, rightLink) {
				continue
			}
			classification = Unknown
		}
		if err := collector.append(Change{
			kind: LinkChanged, classification: classification,
			pointer: linkPointer,
		}); err != nil {
			return err
		}
	}
	for _, link := range right {
		if _, exists := leftNames[link.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: LinkAdded, classification: Additive,
			pointer: pointer + "/links/" + escapePointer(link.Name),
		}); err != nil {
			return err
		}
	}
	return nil
}

func equalObjectWithoutExtensions(left jsonvalue.Value, right jsonvalue.Value) bool {
	leftMembers, leftValid := left.Members()
	rightMembers, rightValid := right.Members()
	if !leftValid || !rightValid {
		return false
	}
	leftMembers = withoutExtensions(leftMembers)
	rightMembers = withoutExtensions(rightMembers)
	if len(leftMembers) != len(rightMembers) {
		return false
	}
	rightValues := memberValues(rightMembers)
	for _, member := range leftMembers {
		rightValue, exists := rightValues[member.Name]
		if !exists || !semanticValueEqual(member.Value, rightValue) {
			return false
		}
	}
	return true
}

type schemaDirection uint8

const (
	schemaInRequest schemaDirection = iota
	schemaInResponse
)

func compareCommonMediaTypeSchemas(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	left []jsonvalue.Member,
	right []jsonvalue.Member,
	pointer string,
	direction schemaDirection,
	dialect openapi.Dialect,
) error {
	rightValues := memberValues(right)
	for _, leftMediaType := range left {
		rightMediaType, exists := rightValues[leftMediaType.Name]
		if !exists {
			continue
		}
		leftSchema, leftPresent := leftMediaType.Value.Lookup("schema")
		rightSchema, rightPresent := rightMediaType.Lookup("schema")
		leftComparable := leftSchema
		rightComparable := rightSchema
		leftValid := true
		rightValid := true
		if leftPresent {
			leftComparable, leftValid = resolveInternalSchema(
				collector, leftRoot, leftSchema, dialect, leftComparison,
			)
		}
		if rightPresent {
			rightComparable, rightValid = resolveInternalSchema(
				collector, rightRoot, rightSchema, dialect, rightComparison,
			)
		}
		if leftPresent == rightPresent &&
			(!leftPresent || (leftValid && rightValid &&
				semanticValueEqual(leftComparable, rightComparable)) ||
				(!leftValid && !rightValid &&
					semanticValueEqual(leftSchema, rightSchema))) {
			continue
		}
		kind, classification := classifyMediaTypeSchema(
			leftComparable, leftPresent,
			rightComparable, rightPresent, direction,
		)
		if (leftPresent && !leftValid) || (rightPresent && !rightValid) {
			classification = Unknown
		}
		if err := collector.append(Change{
			kind: kind, classification: classification,
			pointer: pointer + "/" + escapePointer(leftMediaType.Name) + "/schema",
		}); err != nil {
			return err
		}
	}
	return nil
}

func classifyMediaTypeSchema(
	left jsonvalue.Value,
	leftPresent bool,
	right jsonvalue.Value,
	rightPresent bool,
	direction schemaDirection,
) (Kind, Classification) {
	if !leftPresent {
		if direction == schemaInRequest {
			return RequestSchemaAdded, Breaking
		}
		return ResponseSchemaAdded, Compatible
	}
	if !rightPresent {
		if direction == schemaInRequest {
			return RequestSchemaRemoved, Compatible
		}
		return ResponseSchemaRemoved, Breaking
	}
	classification := Unknown
	leftBoolean, leftIsBoolean := left.Bool()
	rightBoolean, rightIsBoolean := right.Bool()
	if leftIsBoolean && rightIsBoolean && leftBoolean != rightBoolean {
		widened := !leftBoolean && rightBoolean
		if (direction == schemaInRequest && widened) ||
			(direction == schemaInResponse && !widened) {
			classification = Compatible
		} else {
			classification = Breaking
		}
	}
	if direction == schemaInRequest {
		return RequestSchemaChanged, classification
	}
	return ResponseSchemaChanged, classification
}

func compareMediaTypes(
	collector *changeCollector,
	left []jsonvalue.Member,
	right []jsonvalue.Member,
	pointer string,
	removed Kind,
	added Kind,
) error {
	rightNames := memberNames(right)
	leftNames := memberNames(left)
	for _, mediaType := range left {
		if _, exists := rightNames[mediaType.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: removed, classification: Breaking,
			pointer: pointer + "/" + escapePointer(mediaType.Name),
		}); err != nil {
			return err
		}
	}
	for _, mediaType := range right {
		if _, exists := leftNames[mediaType.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: added, classification: Additive,
			pointer: pointer + "/" + escapePointer(mediaType.Name),
		}); err != nil {
			return err
		}
	}
	return nil
}

func optionalText(value jsonvalue.Value, name string) (string, bool, bool) {
	member, present := value.Lookup(name)
	if !present {
		return "", false, true
	}
	text, valid := member.Text()
	return text, true, valid
}

func requiredValue(value jsonvalue.Value) (bool, bool) {
	if value.Kind() != jsonvalue.ObjectKind {
		return false, false
	}
	required, present := value.Lookup("required")
	if !present {
		return false, true
	}
	result, valid := required.Bool()
	return result, valid
}

type comparisonSide uint8

const (
	leftComparison comparisonSide = iota
	rightComparison
)

func resolveInternalComparable(
	collector *changeCollector,
	root jsonvalue.Value,
	value jsonvalue.Value,
	dialect openapi.Dialect,
	side comparisonSide,
	metadataNames []string,
) (jsonvalue.Value, bool) {
	referenceValue, present := value.Lookup("$ref")
	if !present {
		return value, value.Kind() == jsonvalue.ObjectKind
	}
	raw, valid := referenceValue.Text()
	if !valid {
		return value, false
	}
	limits := collector.referenceLimits
	if limits.MaxTraversalDepth == 0 {
		limits = reference.DefaultLimits()
	}
	resourceURI := collector.leftResourceURI
	resolver := collector.leftResolver
	if side == rightComparison {
		resourceURI = collector.rightResourceURI
		resolver = collector.rightResolver
	}
	if collector.resolved[side] == nil {
		collector.resolved[side] = make(map[string]resolvedReference)
	}
	cacheKey := resourceURI + "\x00" + raw + "\x00" +
		strings.Join(metadataNames, "\x00")
	if cached, exists := collector.resolved[side][cacheKey]; exists {
		if !cached.valid {
			return value, false
		}
		return overlayUsageMetadata(
			cached.value, value, dialect, metadataNames,
		), true
	}
	chain, err := reference.ResolveChain(
		collector.ctx,
		reference.Resource{RetrievalURI: resourceURI, Root: root},
		raw,
		resolver,
		limits,
	)
	if err != nil {
		collector.captureResolutionError(
			err, externalReference(resourceURI, raw) && resolverAvailable(resolver),
		)
		collector.resolved[side][cacheKey] = resolvedReference{}
		return value, false
	}
	if chain.Circular() {
		collector.resolved[side][cacheKey] = resolvedReference{}
		return value, false
	}
	targets := chain.Targets()
	resolved := targets[len(targets)-1].Value
	switch dialect {
	case openapi.DialectOAS31, openapi.DialectOAS32:
		for _, target := range slices.Backward(targets[:len(targets)-1]) {
			resolved = overlayReferenceMetadata(
				resolved, target.Value, metadataNames,
			)
		}
	}
	if collector.referenceLimits.MaxTraversalDepth != 0 {
		if err := collector.consumeResolvedValue(resolved); err != nil {
			collector.captureResolutionError(err, false)
			collector.resolved[side][cacheKey] = resolvedReference{}
			return value, false
		}
	}
	collector.resolved[side][cacheKey] = resolvedReference{
		value: resolved, valid: true,
	}
	return overlayUsageMetadata(resolved, value, dialect, metadataNames), true
}

func overlayUsageMetadata(
	resolved jsonvalue.Value,
	usage jsonvalue.Value,
	dialect openapi.Dialect,
	metadataNames []string,
) jsonvalue.Value {
	if dialect != openapi.DialectOAS31 && dialect != openapi.DialectOAS32 {
		return resolved
	}
	return overlayReferenceMetadata(resolved, usage, metadataNames)
}

func externalReference(baseURI string, raw string) bool {
	base, baseErr := url.Parse(baseURI)
	referenceURI, referenceErr := url.Parse(raw)
	if baseErr != nil || referenceErr != nil {
		return !strings.HasPrefix(raw, "#")
	}
	resolved := base.ResolveReference(referenceURI)
	resolved.Fragment = ""
	resolved.RawFragment = ""
	base.Fragment = ""
	base.RawFragment = ""
	return resolved.String() != base.String()
}

func resolverAvailable(resolver reference.Resolver) bool {
	if resolver == nil {
		return false
	}
	value := reflect.ValueOf(resolver)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

func resolveInternalSchema(
	collector *changeCollector,
	root jsonvalue.Value,
	value jsonvalue.Value,
	dialect openapi.Dialect,
	side comparisonSide,
) (jsonvalue.Value, bool) {
	if _, present := value.Lookup("$ref"); !present {
		return value, value.Kind() == jsonvalue.ObjectKind ||
			value.Kind() == jsonvalue.BooleanKind
	}
	return resolveInternalComparable(
		collector, root, value, dialect, side,
		[]string{"summary", "description"},
	)
}

func equalResolvedSchemaMember(
	collector *changeCollector,
	leftRoot jsonvalue.Value,
	rightRoot jsonvalue.Value,
	left jsonvalue.Value,
	right jsonvalue.Value,
	name string,
	dialect openapi.Dialect,
) bool {
	leftSchema, leftPresent := left.Lookup(name)
	rightSchema, rightPresent := right.Lookup(name)
	if leftPresent != rightPresent {
		return false
	}
	if !leftPresent {
		return true
	}
	leftResolved, leftValid := resolveInternalSchema(
		collector, leftRoot, leftSchema, dialect, leftComparison,
	)
	rightResolved, rightValid := resolveInternalSchema(
		collector, rightRoot, rightSchema, dialect, rightComparison,
	)
	if leftValid && rightValid {
		return semanticValueEqual(leftResolved, rightResolved)
	}
	return !leftValid && !rightValid &&
		semanticValueEqual(leftSchema, rightSchema)
}

func resolvePathItems(
	collector *changeCollector,
	root jsonvalue.Value,
	items []jsonvalue.Member,
	dialect openapi.Dialect,
	side comparisonSide,
) []jsonvalue.Member {
	result := append([]jsonvalue.Member(nil), items...)
	for index := range result {
		members, object := result[index].Value.Members()
		if !object || len(members) != 1 || members[0].Name != "$ref" {
			continue
		}
		resolved, valid := resolveInternalComparable(
			collector, root, result[index].Value, dialect, side,
			referenceMetadataNames("pathItems", dialect),
		)
		if valid {
			result[index].Value = resolved
		}
	}
	return result
}

func overlayReferenceMetadata(
	target jsonvalue.Value,
	referenceValue jsonvalue.Value,
	metadataNames []string,
) jsonvalue.Value {
	targetMembers, targetValid := target.Members()
	if !targetValid {
		return target
	}
	for _, name := range metadataNames {
		override, present := referenceValue.Lookup(name)
		if !present {
			continue
		}
		replaced := false
		for index := range targetMembers {
			if targetMembers[index].Name == name {
				targetMembers[index].Value = override
				replaced = true
				break
			}
		}
		if !replaced {
			targetMembers = append(targetMembers, jsonvalue.Member{
				Name: name, Value: override,
			})
		}
	}
	result, _ := jsonvalue.Object(targetMembers)
	return result
}

func referenceMetadataNames(
	registry string,
	dialect openapi.Dialect,
) []string {
	switch dialect {
	case openapi.DialectOAS31, openapi.DialectOAS32:
	default:
		return nil
	}
	switch registry {
	case "examples", "pathItems":
		return []string{"summary", "description"}
	case "responses":
		switch dialect {
		case openapi.DialectOAS32:
			return []string{"summary", "description"}
		default:
			return []string{"description"}
		}
	case "parameters", "requestBodies", "headers", "securitySchemes", "links":
		return []string{"description"}
	default:
		return nil
	}
}

func contentMembers(value jsonvalue.Value) []jsonvalue.Member {
	return namedObjectMembers(value, "content")
}

func namedObjectMembers(value jsonvalue.Value, name string) []jsonvalue.Member {
	object, exists := value.Lookup(name)
	if !exists || object.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	members, _ := object.Members()
	return members
}

func compareCommonContainerExtensions(
	collector *changeCollector,
	left []jsonvalue.Member,
	right []jsonvalue.Member,
	base string,
) error {
	rightByName := memberValues(right)
	for _, container := range left {
		replacement, exists := rightByName[container.Name]
		if !exists {
			continue
		}
		if err := compareExtensions(
			collector, container.Value, replacement,
			base+"/"+escapePointer(container.Name),
		); err != nil {
			return err
		}
	}
	return nil
}

func compareExtensions(
	collector *changeCollector,
	left jsonvalue.Value,
	right jsonvalue.Value,
	pointer string,
) error {
	leftExtensions := extensionMembers(left)
	rightExtensions := extensionMembers(right)
	rightByName := memberValues(rightExtensions)
	leftNames := memberNames(leftExtensions)
	for _, extension := range leftExtensions {
		replacement, exists := rightByName[extension.Name]
		if exists && semanticValueEqual(extension.Value, replacement) {
			continue
		}
		if err := collector.append(Change{
			kind:           ExtensionChanged,
			classification: collector.extensionClassification,
			pointer:        pointer + "/" + escapePointer(extension.Name),
		}); err != nil {
			return err
		}
	}
	for _, extension := range rightExtensions {
		if _, exists := leftNames[extension.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind:           ExtensionChanged,
			classification: collector.extensionClassification,
			pointer:        pointer + "/" + escapePointer(extension.Name),
		}); err != nil {
			return err
		}
	}
	return nil
}

func extensionMembers(value jsonvalue.Value) []jsonvalue.Member {
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		if strings.HasPrefix(strings.ToLower(member.Name), "x-") {
			result = append(result, member)
		}
	}
	return result
}

type changeCollector struct {
	ctx                     context.Context
	maximum                 int
	leftResourceURI         string
	rightResourceURI        string
	leftResolver            reference.Resolver
	rightResolver           reference.Resolver
	extensionClassification Classification
	referenceLimits         reference.Limits
	remainingResolvedNodes  int
	resolved                [2]map[string]resolvedReference
	resolutionErr           error
	changes                 []Change
}

type resolvedReference struct {
	value jsonvalue.Value
	valid bool
}

func (collector *changeCollector) consumeResolvedValue(
	root jsonvalue.Value,
) error {
	pending := []boundedValue{{value: root, depth: 1}}
	for len(pending) > 0 {
		if err := collector.ctx.Err(); err != nil {
			return err
		}
		if collector.remainingResolvedNodes < 1 {
			return ErrLimitExceeded
		}
		collector.remainingResolvedNodes--
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		childCount, _ := node.value.Length()
		if !diffChildrenFit(
			childCount, 0, len(pending), node.depth,
			collector.remainingResolvedNodes,
			collector.referenceLimits.MaxTraversalDepth,
		) {
			return ErrLimitExceeded
		}
		switch node.value.Kind() {
		case jsonvalue.ArrayKind:
			elements, _ := node.value.Elements()
			for _, element := range slices.Backward(elements) {
				pending = append(pending, boundedValue{
					value: element, depth: node.depth + 1,
				})
			}
		case jsonvalue.ObjectKind:
			members, _ := node.value.Members()
			for _, member := range slices.Backward(members) {
				pending = append(pending, boundedValue{
					value: member.Value, depth: node.depth + 1,
				})
			}
		}
	}
	return nil
}

func (collector *changeCollector) captureResolutionError(
	err error,
	explicitExternal bool,
) {
	if collector.resolutionErr != nil {
		return
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		collector.resolutionErr = err
	case errors.Is(err, reference.ErrLimitExceeded), errors.Is(err, ErrLimitExceeded):
		collector.resolutionErr = ErrLimitExceeded
	case explicitExternal:
		collector.resolutionErr = err
	}
}

func (collector *changeCollector) append(change Change) error {
	if collector.resolutionErr != nil {
		return collector.resolutionErr
	}
	if err := collector.ctx.Err(); err != nil {
		return err
	}
	if len(collector.changes) >= collector.maximum {
		return ErrLimitExceeded
	}
	collector.changes = append(collector.changes, change)
	return nil
}

func collectRemovedContainers(
	collector *changeCollector,
	left []jsonvalue.Member,
	right []jsonvalue.Member,
	base string,
	kind Kind,
) error {
	rightNames := memberNames(right)
	for _, member := range left {
		if _, exists := rightNames[member.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: kind, classification: Breaking,
			pointer: base + "/" + escapePointer(member.Name),
		}); err != nil {
			return err
		}
	}
	return nil
}

func collectAddedContainers(
	collector *changeCollector,
	left []jsonvalue.Member,
	right []jsonvalue.Member,
	base string,
	kind Kind,
) error {
	leftNames := memberNames(left)
	for _, member := range right {
		if _, exists := leftNames[member.Name]; exists {
			continue
		}
		if err := collector.append(Change{
			kind: kind, classification: Additive,
			pointer: base + "/" + escapePointer(member.Name),
		}); err != nil {
			return err
		}
	}
	return nil
}

func collectRemovedOperations(
	collector *changeCollector,
	left []jsonvalue.Member,
	right []jsonvalue.Member,
	base string,
	dialect openapi.Dialect,
) error {
	rightValues := memberValues(right)
	for _, container := range left {
		rightValue, exists := rightValues[container.Name]
		if !exists {
			continue
		}
		leftOperations := operationMembers(container.Value, dialect)
		rightOperations := memberNames(operationMembers(rightValue, dialect))
		for _, operation := range leftOperations {
			if _, exists := rightOperations[operation.Name]; exists {
				continue
			}
			if err := collector.append(Change{
				kind: OperationRemoved, classification: Breaking,
				pointer: operationPointer(base, container.Name, operation.Name),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func collectAddedOperations(
	collector *changeCollector,
	left []jsonvalue.Member,
	right []jsonvalue.Member,
	base string,
	dialect openapi.Dialect,
) error {
	leftValues := memberValues(left)
	for _, container := range right {
		leftValue, exists := leftValues[container.Name]
		if !exists {
			continue
		}
		leftOperations := memberNames(operationMembers(leftValue, dialect))
		for _, operation := range operationMembers(container.Value, dialect) {
			if _, exists := leftOperations[operation.Name]; exists {
				continue
			}
			if err := collector.append(Change{
				kind: OperationAdded, classification: Additive,
				pointer: operationPointer(base, container.Name, operation.Name),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func objectMembers(root jsonvalue.Value, name string, paths bool) []jsonvalue.Member {
	value, exists := root.Lookup(name)
	if !exists || value.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		if strings.HasPrefix(strings.ToLower(member.Name), "x-") ||
			paths && !strings.HasPrefix(member.Name, "/") ||
			member.Value.Kind() != jsonvalue.ObjectKind {
			continue
		}
		result = append(result, member)
	}
	return result
}

func operationMembers(pathItem jsonvalue.Value, dialect openapi.Dialect) []jsonvalue.Member {
	allowed := map[string]struct{}{
		"get": {}, "put": {}, "post": {}, "delete": {},
		"options": {}, "head": {}, "patch": {},
	}
	if dialect != openapi.DialectSwagger20 {
		allowed["trace"] = struct{}{}
	}
	if dialect == openapi.DialectOAS32 {
		allowed["query"] = struct{}{}
	}
	members, _ := pathItem.Members()
	var result []jsonvalue.Member
	for _, member := range members {
		if _, exists := allowed[member.Name]; exists &&
			member.Value.Kind() == jsonvalue.ObjectKind {
			result = append(result, member)
		}
	}
	if dialect == openapi.DialectOAS32 {
		additional, exists := pathItem.Lookup("additionalOperations")
		if exists && additional.Kind() == jsonvalue.ObjectKind {
			additionalMembers, _ := additional.Members()
			for _, member := range additionalMembers {
				if member.Value.Kind() == jsonvalue.ObjectKind {
					member.Name = "additionalOperations/" + escapePointer(member.Name)
					result = append(result, member)
				}
			}
		}
	}
	return result
}

func memberNames(members []jsonvalue.Member) map[string]struct{} {
	result := make(map[string]struct{}, len(members))
	for _, member := range members {
		result[member.Name] = struct{}{}
	}
	return result
}

func memberValues(members []jsonvalue.Member) map[string]jsonvalue.Value {
	result := make(map[string]jsonvalue.Value, len(members))
	for _, member := range members {
		result[member.Name] = member.Value
	}
	return result
}

func operationPointer(base string, container string, operation string) string {
	return fmt.Sprintf("%s/%s/%s", base, escapePointer(container), operation)
}

func escapePointer(value string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(value)
}
