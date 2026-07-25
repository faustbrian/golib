package reference

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

var (
	// ErrUnsupportedBundleTarget reports a reference that cannot be placed in
	// a version-appropriate reusable-object registry without guessing its type.
	ErrUnsupportedBundleTarget = errors.New("unsupported component bundle target")
	// ErrBundleConflict reports a malformed or occupied destination registry.
	ErrBundleConflict = errors.New("OpenAPI component bundle conflict")
)

// BundleOptions bounds one explicit component-bundling operation.
type BundleOptions struct {
	ReferenceLimits       Limits
	MaxReferences         int
	MaxComponents         int
	MaxNodes              int
	MaxDepth              int
	MaxComponentNameBytes int
}

// DefaultBundleOptions returns conservative bounds for untrusted resources.
func DefaultBundleOptions() BundleOptions {
	return BundleOptions{
		ReferenceLimits:       DefaultLimits(),
		MaxReferences:         100_000,
		MaxComponents:         100_000,
		MaxNodes:              1_000_000,
		MaxDepth:              256,
		MaxComponentNameBytes: 1_024,
	}
}

// BundleEntry records one reference rewritten during component bundling.
type BundleEntry struct {
	sourceResource string
	sourcePointer  string
	rawReference   string
	targetResource string
	targetPointer  string
	localReference string
}

// SourceResource returns the source resource identity.
func (entry BundleEntry) SourceResource() string {
	return entry.sourceResource
}

// SourcePointer returns the escaped pointer to the source $ref member.
func (entry BundleEntry) SourcePointer() string {
	return entry.sourcePointer
}

// RawReference returns the original URI-reference spelling.
func (entry BundleEntry) RawReference() string {
	return entry.rawReference
}

// TargetResource returns the resolved target resource identity.
func (entry BundleEntry) TargetResource() string {
	return entry.targetResource
}

// TargetPointer returns the resolved target pointer in its resource.
func (entry BundleEntry) TargetPointer() string {
	return entry.targetPointer
}

// LocalReference returns the rewritten internal component reference.
func (entry BundleEntry) LocalReference() string {
	return entry.localReference
}

// BundleResult owns a bundled document and source-ordered rewrite provenance.
type BundleResult struct {
	document openapi.Document
	entries  []BundleEntry
}

// Document returns the immutable bundled document.
func (result BundleResult) Document() openapi.Document {
	return result.document
}

// Entries returns caller-owned rewrite provenance.
func (result BundleResult) Entries() []BundleEntry {
	return append([]BundleEntry(nil), result.entries...)
}

// BundleComponents localizes external references whose resolved targets live
// in version-appropriate component registries. Resolver is the only external
// I/O boundary. Unsupported target kinds fail instead of being guessed.
func BundleComponents(
	ctx context.Context,
	base Resource,
	resolver Resolver,
	options BundleOptions,
) (BundleResult, error) {
	if ctx == nil {
		return BundleResult{}, errors.New("bundle components: nil context")
	}
	if err := ctx.Err(); err != nil {
		return BundleResult{}, err
	}
	if err := options.validate(); err != nil {
		return BundleResult{}, err
	}
	document, err := openapi.Decode(base.Root)
	if err != nil {
		return BundleResult{}, fmt.Errorf("bundle components: %w", err)
	}
	base = withOpenAPI32Self(base)
	bundler, err := newComponentBundler(ctx, base, document, resolver, options)
	if err != nil {
		return BundleResult{}, err
	}
	root, err := bundler.rewriteValue(base, base.Root, "", 1, "")
	if err != nil {
		return BundleResult{}, err
	}
	root, err = bundler.insertAdditions(root)
	if err != nil {
		return BundleResult{}, err
	}
	// Bundling only adds registries and rewrites string references, preserving
	// the already decoded root version marker.
	bundled, _ := openapi.Decode(root)
	return BundleResult{document: bundled, entries: bundler.entries}, nil
}

func (options BundleOptions) validate() error {
	err := options.ReferenceLimits.validate()
	switch err {
	case nil:
	default:
		return err
	}
	if options.MaxReferences < 1 || options.MaxComponents < 1 ||
		options.MaxNodes < 1 || options.MaxDepth < 1 ||
		options.MaxComponentNameBytes < 1 {
		return ErrLimitExceeded
	}
	return nil
}

type componentBundler struct {
	ctx         context.Context
	base        Resource
	dialect     specversion.Dialect
	resolver    Resolver
	options     BundleOptions
	nodes       int
	references  int
	components  int
	allocations map[string]string
	occupied    map[string]map[string]bool
	additions   []bundleAddition
	entries     []BundleEntry
}

type bundleAddition struct {
	registry string
	name     string
	value    jsonvalue.Value
}

type bundleLocation struct {
	registry string
	name     string
}

type cachedResolver struct {
	resolver  Resolver
	base      Resource
	resources map[string]Resource
}

func (resolver *cachedResolver) Resolve(
	ctx context.Context,
	identifier string,
) (Resource, error) {
	if sameResource(identifier, resolver.base) {
		return resolver.base, nil
	}
	if resource, ok := resolver.resources[identifier]; ok {
		return resource, nil
	}
	resource, err := resolver.resolver.Resolve(ctx, identifier)
	if err != nil {
		return Resource{}, err
	}
	resource = withOpenAPI32Self(resource)
	resolver.resources[identifier] = resource
	return resource, nil
}

func newComponentBundler(
	ctx context.Context,
	base Resource,
	document openapi.Document,
	resolver Resolver,
	options BundleOptions,
) (*componentBundler, error) {
	bundler := &componentBundler{
		ctx:         ctx,
		base:        base,
		dialect:     document.SpecificationVersion().Dialect(),
		resolver:    resolver,
		options:     options,
		allocations: make(map[string]string),
		occupied:    make(map[string]map[string]bool),
	}
	if !resolverIsNil(resolver) {
		bundler.resolver = &cachedResolver{
			resolver:  resolver,
			base:      base,
			resources: make(map[string]Resource),
		}
	}
	if err := bundler.inventoryExisting(base.Root); err != nil {
		return nil, err
	}
	return bundler, nil
}

func (bundler *componentBundler) inventoryExisting(root jsonvalue.Value) error {
	if bundler.dialect == specversion.DialectSwagger20 {
		for registry := range swaggerBundleRegistries {
			if err := bundler.inventoryRegistry(root, registry); err != nil {
				return err
			}
		}
		return nil
	}
	components, exists := root.Lookup("components")
	if !exists {
		return nil
	}
	if components.Kind() != jsonvalue.ObjectKind {
		return ErrBundleConflict
	}
	for registry := range oasBundleRegistries[bundler.dialect] {
		if err := bundler.inventoryRegistry(components, registry); err != nil {
			return err
		}
	}
	return nil
}

func (bundler *componentBundler) inventoryRegistry(
	container jsonvalue.Value,
	registry string,
) error {
	value, exists := container.Lookup(registry)
	if !exists || value.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	memberCount, _ := value.Length()
	if !itemsFitBudget(
		memberCount, bundler.components, bundler.options.MaxComponents,
	) {
		return ErrLimitExceeded
	}
	members, _ := value.Members()
	bundler.components += memberCount
	occupied := bundler.registryNames(registry)
	for _, member := range members {
		occupied[member.Name] = true
	}
	return nil
}

func (bundler *componentBundler) registryNames(registry string) map[string]bool {
	if bundler.occupied[registry] == nil {
		bundler.occupied[registry] = make(map[string]bool)
	}
	return bundler.occupied[registry]
}

func (bundler *componentBundler) rewriteValue(
	resource Resource,
	value jsonvalue.Value,
	pointer string,
	depth int,
	registryHint string,
) (jsonvalue.Value, error) {
	if err := bundler.ctx.Err(); err != nil {
		return jsonvalue.Value{}, err
	}
	if depth > bundler.options.MaxDepth {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	bundler.nodes++
	if bundler.nodes > bundler.options.MaxNodes {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	childCount, _ := value.Length()
	if !childrenFitBudget(
		childCount, bundler.nodes, 0, depth,
		bundler.options.MaxNodes, bundler.options.MaxDepth,
	) {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	switch value.Kind() {
	case jsonvalue.ArrayKind:
		elements, _ := value.Elements()
		for index := range elements {
			transformed, err := bundler.rewriteValue(
				resource,
				elements[index],
				pointer+"/"+strconv.Itoa(index),
				depth+1,
				registryHint,
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
			memberPointer := pointer + "/" + escapeBundlePointer(members[index].Name)
			if bundler.opaqueMember(
				pointer, members[index].Name, registryHint,
			) {
				continue
			}
			if members[index].Name == "$ref" {
				raw, ok := members[index].Value.Text()
				if !ok {
					return jsonvalue.Value{}, fmt.Errorf(
						"%w at %s", ErrInvalidReference, memberPointer,
					)
				}
				localized, err := bundler.bundleReference(
					resource, raw, memberPointer, registryHint,
				)
				if err != nil {
					return jsonvalue.Value{}, err
				}
				members[index].Value, _ = jsonvalue.String(localized)
				continue
			}
			transformed, err := bundler.rewriteValue(
				resource, members[index].Value, memberPointer, depth+1,
				registryHint,
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

func (bundler *componentBundler) opaqueMember(
	pointer string,
	name string,
	registryHint string,
) bool {
	current, err := ParsePointer(pointer)
	if err == nil && bundleMapNamesMayStartWithX(current.Tokens()) {
		return false
	}
	if bundleExtensionMember(pointer, name) {
		return true
	}
	if name == "example" {
		return true
	}
	objectRegistry := bundler.sourceRegistry(pointer+"/$ref", registryHint)
	if objectRegistry == "examples" && (name == "value" || name == "dataValue") {
		return true
	}
	if objectRegistry == "links" && name == "requestBody" {
		return true
	}
	if bundler.dialect == specversion.DialectSwagger20 &&
		objectRegistry == "responses" && name == "examples" {
		return true
	}
	if !schemaReferencePointer(
		mustBundlePointerTokens(pointer + "/" + escapeBundlePointer(name) + "/$ref"),
	) {
		return false
	}
	switch name {
	case "const", "default", "enum", "examples":
		return true
	default:
		return false
	}
}

func bundleExtensionMember(pointer string, name string) bool {
	if len(name) < 2 || !strings.EqualFold(name[:min(len(name), 2)], "x-") {
		return false
	}
	parsed, err := ParsePointer(pointer)
	switch err {
	case nil:
	default:
		return true
	}
	return !bundleMapNamesMayStartWithX(parsed.Tokens())
}

func bundleMapNamesMayStartWithX(tokens []string) bool {
	if len(tokens) == 2 && tokens[0] == "components" {
		return true
	}
	if len(tokens) == 1 {
		switch tokens[0] {
		case "definitions", "parameters", "responses", "securityDefinitions":
			return true
		}
	}
	if len(tokens) == 0 {
		return false
	}
	switch tokens[len(tokens)-1] {
	case "$defs", "properties", "patternProperties", "dependentSchemas",
		"examples", "headers", "links", "callbacks", "content", "encoding":
		return true
	default:
		return false
	}
}

func mustBundlePointerTokens(pointer string) []string {
	parsed, err := ParsePointer(pointer)
	if err != nil {
		return nil
	}
	return parsed.Tokens()
}

func (bundler *componentBundler) bundleReference(
	resource Resource,
	raw string,
	sourcePointer string,
	registryHint string,
) (string, error) {
	bundler.references++
	if bundler.references > bundler.options.MaxReferences {
		return "", ErrLimitExceeded
	}
	target, err := Resolve(
		bundler.ctx, resource, raw, bundler.resolver, bundler.options.ReferenceLimits,
	)
	if err != nil {
		return "", fmt.Errorf("bundle reference at %s: %w", sourcePointer, err)
	}
	if sameResource(target.RequestedURI, bundler.base) {
		if sameResource(resourceIdentifier(resource), bundler.base) {
			return raw, nil
		}
		return localFragmentReference(target.Fragment)
	}
	identity := targetIdentity(target)
	if local, exists := bundler.allocations[identity]; exists {
		bundler.recordEntry(resource, sourcePointer, raw, target, local)
		return local, nil
	}
	location, err := bundler.targetLocation(
		target.Fragment, sourcePointer, registryHint,
	)
	if err != nil {
		return "", fmt.Errorf("bundle reference at %s: %w", sourcePointer, err)
	}
	name, err := bundler.allocateName(location.registry, location.name)
	if err != nil {
		return "", fmt.Errorf("bundle reference at %s: %w", sourcePointer, err)
	}
	local := bundler.localReference(location.registry, name)
	bundler.allocations[identity] = local
	additionIndex := len(bundler.additions)
	bundler.additions = append(bundler.additions, bundleAddition{
		registry: location.registry,
		name:     name,
	})
	bundler.recordEntry(resource, sourcePointer, raw, target, local)
	targetPointer := target.Fragment.Pointer().String()
	transformed, err := bundler.rewriteValue(
		target.Resource, target.Value, targetPointer, 1, location.registry,
	)
	if err != nil {
		return "", err
	}
	bundler.additions[additionIndex].value = transformed
	return local, nil
}

func (bundler *componentBundler) recordEntry(
	resource Resource,
	sourcePointer string,
	raw string,
	target Target,
	local string,
) {
	bundler.entries = append(bundler.entries, BundleEntry{
		sourceResource: resourceIdentifier(resource),
		sourcePointer:  sourcePointer,
		rawReference:   raw,
		targetResource: resourceIdentifier(target.Resource),
		targetPointer:  target.Fragment.Pointer().String(),
		localReference: local,
	})
}

func (bundler *componentBundler) targetLocation(
	fragment Fragment,
	sourcePointer string,
	registryHint string,
) (bundleLocation, error) {
	sourceRegistry := bundler.sourceRegistry(sourcePointer, registryHint)
	if location, known := bundler.knownTargetLocation(fragment); known {
		if sourceRegistry != "" && sourceRegistry != location.registry {
			return bundleLocation{}, ErrUnsupportedBundleTarget
		}
		return location, nil
	}
	if sourceRegistry == "" {
		return bundleLocation{}, ErrUnsupportedBundleTarget
	}
	name := derivedBundleName(fragment)
	if name == "" {
		return bundleLocation{}, ErrUnsupportedBundleTarget
	}
	return bundleLocation{registry: sourceRegistry, name: name}, nil
}

func (bundler *componentBundler) knownTargetLocation(
	fragment Fragment,
) (bundleLocation, bool) {
	return knownBundleTargetLocation(bundler.dialect, fragment)
}

func knownBundleTargetLocation(
	dialect specversion.Dialect,
	fragment Fragment,
) (bundleLocation, bool) {
	switch fragment.Kind() {
	case FragmentPointer:
	default:
		return bundleLocation{}, false
	}
	tokens := fragment.Pointer().Tokens()
	switch dialect {
	case specversion.DialectSwagger20:
		switch len(tokens) {
		case 2:
			if !swaggerBundleRegistries[tokens[0]] {
				return bundleLocation{}, false
			}
			return bundleLocation{registry: tokens[0], name: tokens[1]}, true
		}
		return bundleLocation{}, false
	}
	registries := oasBundleRegistries[dialect]
	switch len(tokens) {
	case 3:
		switch tokens[0] {
		case "components":
			if registries[tokens[1]] {
				return bundleLocation{registry: tokens[1], name: tokens[2]}, true
			}
		}
	}
	return bundleLocation{}, false
}

func (bundler *componentBundler) sourceRegistry(
	pointer string,
	registryHint string,
) string {
	parsed, err := ParsePointer(pointer)
	switch err {
	case nil:
	default:
		return ""
	}
	tokens := parsed.Tokens()
	switch len(tokens) {
	case 0, 1:
		return ""
	}
	switch tokens[len(tokens)-1] {
	case "$ref":
	default:
		return ""
	}
	switch bundler.dialect {
	case specversion.DialectSwagger20:
		switch len(tokens) {
		case 3:
			if swaggerBundleRegistries[tokens[0]] {
				return tokens[0]
			}
		}
	}
	if schemaReferencePointer(tokens) {
		if bundler.dialect == specversion.DialectSwagger20 {
			return "definitions"
		}
		return "schemas"
	}
	switch len(tokens) {
	case 3:
		if tokens[0] != "paths" && tokens[0] != "webhooks" {
			break
		}
		if bundler.dialect != specversion.DialectSwagger20 &&
			oasBundleRegistries[bundler.dialect]["pathItems"] {
			return "pathItems"
		}
		return ""
	}
	if len(tokens) < 3 {
		return ""
	}
	kind := tokens[len(tokens)-3]
	if bundler.dialect == specversion.DialectSwagger20 {
		if swaggerBundleRegistries[kind] {
			return kind
		}
		return ""
	}
	if oasBundleRegistries[bundler.dialect][kind] {
		return kind
	}
	if tokens[len(tokens)-2] == "requestBody" &&
		oasBundleRegistries[bundler.dialect]["requestBodies"] {
		return "requestBodies"
	}
	switch registryHint {
	case "schemas", "definitions":
		return registryHint
	}
	return ""
}

func schemaReferencePointer(tokens []string) bool {
	switch tokens[0] {
	case "definitions":
		return true
	case "components":
		switch len(tokens) {
		case 1:
		default:
			if tokens[1] == "schemas" {
				return true
			}
		}
	}
	for _, token := range tokens[:len(tokens)-1] {
		switch token {
		case "schema":
			return true
		}
	}
	return false
}

func derivedBundleName(fragment Fragment) string {
	var raw string
	switch fragment.Kind() {
	case FragmentRoot:
		raw = "root"
	case FragmentAnchor:
		raw = fragment.Anchor()
	case FragmentPointer:
		tokens := fragment.Pointer().Tokens()
		if len(tokens) == 0 {
			raw = "root"
		} else {
			raw = tokens[len(tokens)-1]
		}
	default:
		return ""
	}
	var result strings.Builder
	for _, character := range raw {
		if bundleNamePattern.MatchString(string(character)) {
			result.WriteRune(character)
		} else {
			result.WriteByte('_')
		}
	}
	if result.Len() == 0 {
		return "bundled"
	}
	return result.String()
}

var bundleNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func (bundler *componentBundler) allocateName(
	registry string,
	original string,
) (string, error) {
	if len(original) > bundler.options.MaxComponentNameBytes {
		return "", ErrLimitExceeded
	}
	if !bundleNamePattern.MatchString(original) {
		return "", ErrUnsupportedBundleTarget
	}
	if bundler.components >= bundler.options.MaxComponents {
		return "", ErrLimitExceeded
	}
	bundler.components++
	occupied := bundler.registryNames(registry)
	if !occupied[original] {
		occupied[original] = true
		return original, nil
	}
	base := original + "_bundled"
	if !occupied[base] {
		occupied[base] = true
		return base, nil
	}
	for offset := range bundler.options.MaxComponents {
		candidate := base + "_" + strconv.Itoa(offset+2)
		if len(candidate) > bundler.options.MaxComponentNameBytes {
			return "", ErrLimitExceeded
		}
		if !occupied[candidate] {
			occupied[candidate] = true
			return candidate, nil
		}
	}
	return "", ErrLimitExceeded
}

func (bundler *componentBundler) localReference(
	registry string,
	name string,
) string {
	if bundler.dialect == specversion.DialectSwagger20 {
		return "#/" + escapeBundlePointer(registry) + "/" + escapeBundlePointer(name)
	}
	return "#/components/" + escapeBundlePointer(registry) +
		"/" + escapeBundlePointer(name)
}

func (bundler *componentBundler) insertAdditions(
	root jsonvalue.Value,
) (jsonvalue.Value, error) {
	for _, addition := range bundler.additions {
		var err error
		if bundler.dialect == specversion.DialectSwagger20 {
			root, err = appendBundleEntry(
				root, addition.registry, addition.name, addition.value,
			)
		} else {
			components, exists := root.Lookup("components")
			if !exists {
				components, _ = jsonvalue.Object(nil)
			}
			components, err = appendBundleEntry(
				components, addition.registry, addition.name, addition.value,
			)
			if err == nil {
				root, err = replaceOrAppendBundleMember(root, "components", components)
			}
		}
		if err != nil {
			return jsonvalue.Value{}, err
		}
	}
	return root, nil
}

func appendBundleEntry(
	container jsonvalue.Value,
	registry string,
	name string,
	value jsonvalue.Value,
) (jsonvalue.Value, error) {
	registryValue, exists := container.Lookup(registry)
	if !exists {
		registryValue, _ = jsonvalue.Object(nil)
	}
	if registryValue.Kind() != jsonvalue.ObjectKind {
		return jsonvalue.Value{}, ErrBundleConflict
	}
	if _, occupied := registryValue.Lookup(name); occupied {
		return jsonvalue.Value{}, ErrBundleConflict
	}
	members, _ := registryValue.Members()
	members = append(members, jsonvalue.Member{Name: name, Value: value})
	registryValue, _ = jsonvalue.Object(members)
	return replaceOrAppendBundleMember(container, registry, registryValue)
}

func replaceOrAppendBundleMember(
	object jsonvalue.Value,
	name string,
	value jsonvalue.Value,
) (jsonvalue.Value, error) {
	members, ok := object.Members()
	if !ok {
		return jsonvalue.Value{}, ErrBundleConflict
	}
	for index := range members {
		if members[index].Name == name {
			members[index].Value = value
			result, _ := jsonvalue.Object(members)
			return result, nil
		}
	}
	members = append(members, jsonvalue.Member{Name: name, Value: value})
	result, _ := jsonvalue.Object(members)
	return result, nil
}

func localFragmentReference(fragment Fragment) (string, error) {
	switch fragment.Kind() {
	case FragmentRoot:
		return "#", nil
	case FragmentPointer:
		return (&url.URL{Fragment: fragment.Pointer().String()}).String(), nil
	case FragmentAnchor:
		return (&url.URL{Fragment: fragment.Anchor()}).String(), nil
	default:
		return "", ErrInvalidFragment
	}
}

func resourceIdentifier(resource Resource) string {
	if resource.CanonicalURI != "" {
		return resource.CanonicalURI
	}
	return resource.RetrievalURI
}

func escapeBundlePointer(value string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(value)
}

var swaggerBundleRegistries = map[string]bool{
	"definitions":         true,
	"parameters":          true,
	"responses":           true,
	"securityDefinitions": true,
}

var oasBundleRegistries = map[specversion.Dialect]map[string]bool{
	specversion.DialectOAS30: {
		"schemas":         true,
		"responses":       true,
		"parameters":      true,
		"examples":        true,
		"requestBodies":   true,
		"headers":         true,
		"securitySchemes": true,
		"links":           true,
		"callbacks":       true,
	},
	specversion.DialectOAS31: {
		"schemas":         true,
		"responses":       true,
		"parameters":      true,
		"examples":        true,
		"requestBodies":   true,
		"headers":         true,
		"securitySchemes": true,
		"links":           true,
		"callbacks":       true,
		"pathItems":       true,
	},
	specversion.DialectOAS32: {
		"schemas":         true,
		"responses":       true,
		"parameters":      true,
		"examples":        true,
		"requestBodies":   true,
		"headers":         true,
		"securitySchemes": true,
		"links":           true,
		"callbacks":       true,
		"pathItems":       true,
		"mediaTypes":      true,
	},
}
