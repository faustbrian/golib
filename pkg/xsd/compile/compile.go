package compile

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/datatype"
	"github.com/faustbrian/golib/pkg/xsd/resolve"
)

var (
	ErrLimitExceeded       = errors.New("xsd compile: resource limit exceeded")
	ErrNamespace           = errors.New("xsd compile: namespace mismatch")
	ErrResourceIdentity    = errors.New("xsd compile: resource identity mismatch")
	ErrDuplicateComponent  = errors.New("xsd compile: duplicate component")
	ErrInvalidComponent    = errors.New("xsd compile: invalid component")
	ErrUnresolvedComponent = errors.New("xsd compile: unresolved component")
)

const (
	defaultMaxSchemas    = 256
	defaultMaxDepth      = 64
	defaultMaxReferences = 4096
	defaultMaxBytes      = 64 << 20
	defaultMaxComponents = 100000
	defaultMaxParticles  = 1000000
)

// Limits bounds graph construction. Zero values select conservative defaults.
type Limits struct {
	MaxSchemas    int
	MaxDepth      int
	MaxReferences int
	MaxBytes      int64
	MaxComponents int
	MaxParticles  int
}

// Options configures a Compiler. A nil Resolver denies every external load.
type Options struct {
	Resolver resolve.Resolver
	Limits   Limits
}

// Source is the caller-owned root schema resource.
type Source struct {
	URI     string
	Content []byte
}

// Compiler is immutable and safe for concurrent use.
type Compiler struct {
	resolver resolve.Resolver
	limits   Limits
}

// New validates options and creates a reusable compiler.
func New(options Options) (*Compiler, error) {
	limits := options.Limits
	if limits.MaxSchemas == 0 {
		limits.MaxSchemas = defaultMaxSchemas
	}
	if limits.MaxDepth == 0 {
		limits.MaxDepth = defaultMaxDepth
	}
	if limits.MaxReferences == 0 {
		limits.MaxReferences = defaultMaxReferences
	}
	if limits.MaxBytes == 0 {
		limits.MaxBytes = defaultMaxBytes
	}
	if limits.MaxComponents == 0 {
		limits.MaxComponents = defaultMaxComponents
	}
	if limits.MaxParticles == 0 {
		limits.MaxParticles = defaultMaxParticles
	}
	if limits.MaxSchemas < 0 || limits.MaxDepth < 0 ||
		limits.MaxReferences < 0 || limits.MaxBytes < 0 || limits.MaxComponents < 0 ||
		limits.MaxParticles < 0 {
		return nil, fmt.Errorf("xsd compile: limits must not be negative")
	}
	resolver := options.Resolver
	if resolver == nil {
		resolver = resolve.Deny()
	}
	return &Compiler{resolver: resolver, limits: limits}, nil
}

// Document describes one schema document in one effective namespace.
type Document struct {
	URI          string
	Namespace    string
	Chameleon    bool
	Dependencies []string
}

// Set is an immutable, concurrency-safe schema document graph.
type Set struct {
	documents         []Document
	elements          map[xsd.QName]xsd.Element
	attributes        map[xsd.QName]xsd.Attribute
	simpleTypes       map[xsd.QName]xsd.SimpleType
	complexTypes      map[xsd.QName]xsd.ComplexType
	modelGroups       map[xsd.QName]xsd.ModelGroupDefinition
	attributeGroups   map[xsd.QName]xsd.AttributeGroup
	notations         map[xsd.QName]xsd.Notation
	substitutionHeads map[xsd.QName]xsd.QName
}

// ElementNames returns global element names in expanded-name order.
func (s *Set) ElementNames() []xsd.QName { return sortedComponentNames(s.elements) }

// AttributeNames returns global attribute names in expanded-name order.
func (s *Set) AttributeNames() []xsd.QName { return sortedComponentNames(s.attributes) }

// SimpleTypeNames returns global simple type names in expanded-name order.
func (s *Set) SimpleTypeNames() []xsd.QName { return sortedComponentNames(s.simpleTypes) }

// ComplexTypeNames returns global complex type names in expanded-name order.
func (s *Set) ComplexTypeNames() []xsd.QName { return sortedComponentNames(s.complexTypes) }

// ModelGroupNames returns global model group names in expanded-name order.
func (s *Set) ModelGroupNames() []xsd.QName { return sortedComponentNames(s.modelGroups) }

// AttributeGroupNames returns global attribute group names in expanded-name order.
func (s *Set) AttributeGroupNames() []xsd.QName { return sortedComponentNames(s.attributeGroups) }

// NotationNames returns global notation names in expanded-name order.
func (s *Set) NotationNames() []xsd.QName { return sortedComponentNames(s.notations) }

func sortedComponentNames[T any](components map[xsd.QName]T) []xsd.QName {
	names := make([]xsd.QName, 0, len(components))
	for name := range components {
		names = append(names, name)
	}
	sort.Slice(names, func(left, right int) bool {
		if names[left].Namespace == names[right].Namespace {
			return names[left].Local < names[right].Local
		}
		return names[left].Namespace < names[right].Namespace
	})
	return names
}

// ModelGroup returns a named model group by expanded name.
func (s *Set) ModelGroup(name xsd.QName) (xsd.ModelGroupDefinition, bool) {
	group, ok := s.modelGroups[name]
	if !ok {
		return xsd.ModelGroupDefinition{}, false
	}
	group.Content = cloneModelGroup(group.Content)
	group.Annotation = cloneAnnotation(group.Annotation)
	return group, true
}

// AttributeGroup returns a named attribute group by expanded name.
func (s *Set) AttributeGroup(name xsd.QName) (xsd.AttributeGroup, bool) {
	group, ok := s.attributeGroups[name]
	if !ok {
		return xsd.AttributeGroup{}, false
	}
	return cloneAttributeGroup(group), true
}

// Notation returns a global notation declaration by expanded name.
func (s *Set) Notation(name xsd.QName) (xsd.Notation, bool) {
	notation, ok := s.notations[name]
	if !ok {
		return xsd.Notation{}, false
	}
	notation.Annotation = cloneAnnotation(notation.Annotation)
	return notation, true
}

// SubstitutionMember resolves member when it may substitute for head.
func (s *Set) SubstitutionMember(head xsd.QName, member xsd.QName) (xsd.Element, bool) {
	headDeclaration, ok := s.elements[head]
	if !ok || headDeclaration.Block.Contains(xsd.DerivationSubstitution) {
		return xsd.Element{}, false
	}
	current := member
	for current.Local != "" {
		direct, affiliated := s.substitutionHeads[current]
		if !affiliated {
			return xsd.Element{}, false
		}
		if direct == head {
			declaration, exists := s.elements[member]
			if !exists {
				return xsd.Element{}, false
			}
			methods, derived := setElementTypeDerivationMethods(s, declaration, headDeclaration.Type)
			if !derived {
				return xsd.Element{}, false
			}
			var typeBlock xsd.DerivationSet
			if headType, exists := s.complexTypes[headDeclaration.Type]; exists {
				typeBlock = headType.Block
			}
			for _, method := range methods {
				if headDeclaration.Block.Contains(method) || typeBlock.Contains(method) {
					return xsd.Element{}, false
				}
			}
			return cloneElement(declaration), true
		}
		directDeclaration, exists := s.elements[direct]
		if !exists || directDeclaration.Block.Contains(xsd.DerivationSubstitution) {
			return xsd.Element{}, false
		}
		current = direct
	}
	return xsd.Element{}, false
}

func setElementTypeDerivationMethods(
	s *Set,
	element xsd.Element,
	base xsd.QName,
) ([]xsd.Derivation, bool) {
	if element.InlineComplexType != nil {
		rest, ok := setTypeDerivationMethods(s, element.InlineComplexType.Base, base)
		return append([]xsd.Derivation{element.InlineComplexType.Derivation}, rest...), ok
	}
	if element.InlineSimpleType != nil {
		rest, ok := setTypeDerivationMethods(s, element.InlineSimpleType.Base, base)
		return append([]xsd.Derivation{xsd.DerivationRestriction}, rest...), ok
	}
	return setTypeDerivationMethods(s, element.Type, base)
}

func setTypeDerivationMethods(
	s *Set,
	derived xsd.QName,
	base xsd.QName,
) ([]xsd.Derivation, bool) {
	state := compileState{simpleTypes: s.simpleTypes, complexTypes: s.complexTypes}
	return state.typeDerivationMethods(derived, base)
}

func cloneAttributeGroup(group xsd.AttributeGroup) xsd.AttributeGroup {
	group.Attributes = cloneAttributeUses(group.Attributes)
	group.References = append([]xsd.QName(nil), group.References...)
	group.AttributeGroupReferences = append(
		[]xsd.AttributeGroupReference(nil),
		group.AttributeGroupReferences...,
	)
	for index := range group.AttributeGroupReferences {
		group.AttributeGroupReferences[index].Annotation = cloneAnnotation(
			group.AttributeGroupReferences[index].Annotation,
		)
	}
	group.Wildcard = cloneWildcard(group.Wildcard)
	group.Annotation = cloneAnnotation(group.Annotation)
	return group
}

func cloneWildcard(wildcard *xsd.Wildcard) *xsd.Wildcard {
	if wildcard == nil {
		return nil
	}
	clone := *wildcard
	clone.Namespaces = append([]string(nil), wildcard.Namespaces...)
	clone.Annotation = cloneAnnotation(wildcard.Annotation)
	return &clone
}

// Documents returns a deep copy in deterministic URI and namespace order.
func (s *Set) Documents() []Document {
	result := make([]Document, len(s.documents))
	for index, document := range s.documents {
		result[index] = cloneDocument(document)
	}
	return result
}

// Document returns the first compiled document with the resource URI. A
// chameleon resource compiled into multiple namespaces is returned in sorted
// namespace order and remains visible in full through Documents.
func (s *Set) Document(uri string) (Document, bool) {
	for _, document := range s.documents {
		if document.URI == uri {
			return cloneDocument(document), true
		}
	}
	return Document{}, false
}

// Element returns a global element declaration by expanded name.
func (s *Set) Element(name xsd.QName) (xsd.Element, bool) {
	element, ok := s.elements[name]
	if !ok {
		return xsd.Element{}, false
	}
	return cloneElement(element), true
}

func cloneElement(element xsd.Element) xsd.Element {
	element.Annotation = cloneAnnotation(element.Annotation)
	element.ValueNamespaces = cloneNamespaceMap(element.ValueNamespaces)
	constraints := element.IdentityConstraints
	element.IdentityConstraints = make(
		[]xsd.IdentityConstraint,
		len(constraints),
	)
	for index, constraint := range constraints {
		constraint.Fields = append([]string(nil), constraint.Fields...)
		constraint.FieldIDs = append([]string(nil), constraint.FieldIDs...)
		constraint.Namespaces = cloneNamespaceMap(constraint.Namespaces)
		constraint.Annotation = cloneAnnotation(constraint.Annotation)
		constraint.SelectorAnnotation = cloneAnnotation(constraint.SelectorAnnotation)
		constraint.FieldAnnotations = append(
			[]*xsd.Annotation(nil),
			constraint.FieldAnnotations...,
		)
		for fieldIndex := range constraint.FieldAnnotations {
			constraint.FieldAnnotations[fieldIndex] = cloneAnnotation(
				constraint.FieldAnnotations[fieldIndex],
			)
		}
		element.IdentityConstraints[index] = constraint
	}
	if element.InlineSimpleType != nil {
		typeDefinition := cloneSimpleType(*element.InlineSimpleType)
		element.InlineSimpleType = &typeDefinition
	}
	if element.InlineComplexType != nil {
		typeDefinition := cloneComplexType(*element.InlineComplexType)
		element.InlineComplexType = &typeDefinition
	}
	return element
}

func cloneNamespaceMap(namespaces map[string]string) map[string]string {
	clone := make(map[string]string, len(namespaces))
	for prefix, namespace := range namespaces {
		clone[prefix] = namespace
	}
	return clone
}

// Attribute returns a global attribute declaration by expanded name.
func (s *Set) Attribute(name xsd.QName) (xsd.Attribute, bool) {
	attribute, ok := s.attributes[name]
	if !ok {
		return xsd.Attribute{}, false
	}
	return cloneAttribute(attribute), true
}

func cloneAttribute(attribute xsd.Attribute) xsd.Attribute {
	attribute.Annotation = cloneAnnotation(attribute.Annotation)
	attribute.ValueNamespaces = cloneNamespaceMap(attribute.ValueNamespaces)
	if attribute.InlineSimpleType != nil {
		typeDefinition := cloneSimpleType(*attribute.InlineSimpleType)
		attribute.InlineSimpleType = &typeDefinition
	}
	return attribute
}

func cloneAttributeUses(attributes []xsd.AttributeUse) []xsd.AttributeUse {
	clone := make([]xsd.AttributeUse, len(attributes))
	for index, attribute := range attributes {
		clone[index] = attribute
		clone[index].ValueNamespaces = cloneNamespaceMap(attribute.ValueNamespaces)
		clone[index].Annotation = cloneAnnotation(attribute.Annotation)
		if attribute.InlineSimpleType != nil {
			typeDefinition := cloneSimpleType(*attribute.InlineSimpleType)
			clone[index].InlineSimpleType = &typeDefinition
		}
	}
	return clone
}

// SimpleType returns a global simple type definition by expanded name.
func (s *Set) SimpleType(name xsd.QName) (xsd.SimpleType, bool) {
	simpleType, ok := s.simpleTypes[name]
	if !ok {
		return xsd.SimpleType{}, false
	}
	return cloneSimpleType(simpleType), true
}

func cloneSimpleType(simpleType xsd.SimpleType) xsd.SimpleType {
	simpleType.Annotation = cloneAnnotation(simpleType.Annotation)
	simpleType.VarietyAnnotation = cloneAnnotation(simpleType.VarietyAnnotation)
	simpleType.Facets = append([]xsd.Facet(nil), simpleType.Facets...)
	for index := range simpleType.Facets {
		simpleType.Facets[index].Namespaces = cloneNamespaceMap(
			simpleType.Facets[index].Namespaces,
		)
		simpleType.Facets[index].Annotation = cloneAnnotation(
			simpleType.Facets[index].Annotation,
		)
	}
	simpleType.MemberTypes = append([]xsd.QName(nil), simpleType.MemberTypes...)
	if simpleType.InlineBase != nil {
		base := cloneSimpleType(*simpleType.InlineBase)
		simpleType.InlineBase = &base
	}
	if simpleType.InlineItem != nil {
		item := cloneSimpleType(*simpleType.InlineItem)
		simpleType.InlineItem = &item
	}
	simpleType.InlineMembers = append([]xsd.SimpleType(nil), simpleType.InlineMembers...)
	for index := range simpleType.InlineMembers {
		simpleType.InlineMembers[index] = cloneSimpleType(simpleType.InlineMembers[index])
	}
	return simpleType
}

// ComplexType returns a global complex type definition by expanded name.
func (s *Set) ComplexType(name xsd.QName) (xsd.ComplexType, bool) {
	complexType, ok := s.complexTypes[name]
	if !ok {
		return xsd.ComplexType{}, false
	}
	return cloneComplexType(complexType), true
}

func cloneComplexType(complexType xsd.ComplexType) xsd.ComplexType {
	complexType.Annotation = cloneAnnotation(complexType.Annotation)
	complexType.ContentAnnotation = cloneAnnotation(complexType.ContentAnnotation)
	complexType.DerivationAnnotation = cloneAnnotation(complexType.DerivationAnnotation)
	if complexType.InlineSimpleType != nil {
		typeDefinition := cloneSimpleType(*complexType.InlineSimpleType)
		complexType.InlineSimpleType = &typeDefinition
	}
	complexType.SimpleFacets = append([]xsd.Facet(nil), complexType.SimpleFacets...)
	for index := range complexType.SimpleFacets {
		complexType.SimpleFacets[index].Namespaces = cloneNamespaceMap(
			complexType.SimpleFacets[index].Namespaces,
		)
		complexType.SimpleFacets[index].Annotation = cloneAnnotation(
			complexType.SimpleFacets[index].Annotation,
		)
	}
	complexType.Attributes = cloneAttributeUses(complexType.Attributes)
	complexType.Content = cloneModelGroup(complexType.Content)
	complexType.AttributeGroupRefs = append(
		[]xsd.QName(nil),
		complexType.AttributeGroupRefs...,
	)
	complexType.AttributeGroupReferences = append(
		[]xsd.AttributeGroupReference(nil),
		complexType.AttributeGroupReferences...,
	)
	for index := range complexType.AttributeGroupReferences {
		complexType.AttributeGroupReferences[index].Annotation = cloneAnnotation(
			complexType.AttributeGroupReferences[index].Annotation,
		)
	}
	complexType.AttributeWildcard = cloneWildcard(complexType.AttributeWildcard)
	return complexType
}

func cloneModelGroup(group *xsd.ModelGroup) *xsd.ModelGroup {
	if group == nil {
		return nil
	}
	clone := &xsd.ModelGroup{
		Compositor: group.Compositor,
		MinOccurs:  group.MinOccurs,
		MaxOccurs:  group.MaxOccurs,
		Unbounded:  group.Unbounded,
		OccursSet:  group.OccursSet,
		Annotation: cloneAnnotation(group.Annotation),
	}
	clone.Particles = make([]xsd.Particle, len(group.Particles))
	for index, particle := range group.Particles {
		clone.Particles[index] = particle
		clone.Particles[index].Annotation = cloneAnnotation(particle.Annotation)
		if particle.Element != nil {
			element := cloneElement(*particle.Element)
			clone.Particles[index].Element = &element
		}
		clone.Particles[index].Group = cloneModelGroup(particle.Group)
		clone.Particles[index].Wildcard = cloneWildcard(particle.Wildcard)
	}
	return clone
}

func cloneAnnotation(annotation *xsd.Annotation) *xsd.Annotation {
	if annotation == nil {
		return nil
	}
	clone := *annotation
	clone.Documentation = append([]xsd.Documentation(nil), annotation.Documentation...)
	clone.AppInformation = append([]xsd.AppInfo(nil), annotation.AppInformation...)
	return &clone
}

func cloneDocument(document Document) Document {
	document.Dependencies = append([]string(nil), document.Dependencies...)
	return document
}

// Compile parses and resolves a complete bounded schema graph.
func (c *Compiler) Compile(ctx context.Context, root Source) (*Set, error) {
	if err := validateIdentity(root.URI); err != nil {
		return nil, err
	}
	if int64(len(root.Content)) > c.limits.MaxBytes {
		return nil, fmt.Errorf("%w: schema bytes exceed %d", ErrLimitExceeded, c.limits.MaxBytes)
	}
	state := compileState{
		compiler:          c,
		resources:         map[string]resourceDocument{},
		instances:         map[instanceKey]*Document{},
		elements:          map[xsd.QName]xsd.Element{},
		attributes:        map[xsd.QName]xsd.Attribute{},
		simpleTypes:       map[xsd.QName]xsd.SimpleType{},
		complexTypes:      map[xsd.QName]xsd.ComplexType{},
		modelGroups:       map[xsd.QName]xsd.ModelGroupDefinition{},
		attributeGroups:   map[xsd.QName]xsd.AttributeGroup{},
		notations:         map[xsd.QName]xsd.Notation{},
		substitutionHeads: map[xsd.QName]xsd.QName{},
		typeKinds:         map[xsd.QName]string{},
		bytes:             int64(len(root.Content)),
	}
	document, err := xsd.Parse(ctx, root.Content, xsd.ParseOptions{
		SystemID:         root.URI,
		MaxDocumentBytes: c.limits.MaxBytes,
	})
	if err != nil {
		return nil, err
	}
	state.resources[root.URI] = resourceDocument{document: document}
	if err := state.compileDocument(ctx, root.URI, document.TargetNamespace, 1); err != nil {
		return nil, err
	}
	if err := state.expandGroups(); err != nil {
		return nil, err
	}
	if err := state.compileComplexExtensions(); err != nil {
		return nil, err
	}
	if err := state.compileAnonymousComplexTypes(); err != nil {
		return nil, err
	}
	if err := state.compileSubstitutions(); err != nil {
		return nil, err
	}
	if err := state.validateComponents(); err != nil {
		return nil, err
	}

	documents := make([]Document, 0, len(state.instances))
	for _, document := range state.instances {
		documents = append(documents, cloneDocument(*document))
	}
	sort.Slice(documents, func(left, right int) bool {
		if documents[left].URI == documents[right].URI {
			return documents[left].Namespace < documents[right].Namespace
		}
		return documents[left].URI < documents[right].URI
	})
	return &Set{
		documents:         documents,
		elements:          state.elements,
		attributes:        state.attributes,
		simpleTypes:       state.simpleTypes,
		complexTypes:      state.complexTypes,
		modelGroups:       state.modelGroups,
		attributeGroups:   state.attributeGroups,
		notations:         state.notations,
		substitutionHeads: state.substitutionHeads,
	}, nil
}

var builtInTypes = map[string]string{
	"anyType":       "complex",
	"anySimpleType": "simple",
	"string":        "simple", "boolean": "simple", "decimal": "simple",
	"float": "simple", "double": "simple", "duration": "simple",
	"dateTime": "simple", "time": "simple", "date": "simple",
	"gYearMonth": "simple", "gYear": "simple", "gMonthDay": "simple",
	"gDay": "simple", "gMonth": "simple", "hexBinary": "simple",
	"base64Binary": "simple", "anyURI": "simple", "QName": "simple",
	"NOTATION": "simple", "normalizedString": "simple", "token": "simple",
	"language": "simple", "Name": "simple", "NCName": "simple",
	"ID": "simple", "IDREF": "simple", "IDREFS": "simple",
	"ENTITY": "simple", "ENTITIES": "simple", "NMTOKEN": "simple",
	"NMTOKENS": "simple", "integer": "simple", "nonPositiveInteger": "simple",
	"negativeInteger": "simple", "long": "simple", "int": "simple",
	"short": "simple", "byte": "simple", "nonNegativeInteger": "simple",
	"unsignedLong": "simple", "unsignedInt": "simple", "unsignedShort": "simple",
	"unsignedByte": "simple", "positiveInteger": "simple",
}

func (s *compileState) compileSubstitutions() error {
	for member, declaration := range s.elements {
		if declaration.SubstitutionGroup.Local == "" {
			continue
		}
		if _, ok := s.elements[declaration.SubstitutionGroup]; !ok {
			return unresolvedComponent("substitution group head", declaration.SubstitutionGroup)
		}
		s.substitutionHeads[member] = declaration.SubstitutionGroup
	}
	colors := make(map[xsd.QName]uint8, len(s.substitutionHeads))
	for member := range s.substitutionHeads {
		if err := s.validateSubstitution(member, colors); err != nil {
			return err
		}
	}
	return nil
}

func (s *compileState) validateSubstitution(
	member xsd.QName,
	colors map[xsd.QName]uint8,
) error {
	switch colors[member] {
	case 1:
		return invalidComponent("element", member, "substitution affiliation is recursive")
	case 2:
		return nil
	}
	colors[member] = 1
	head := s.substitutionHeads[member]
	if _, transitive := s.substitutionHeads[head]; transitive {
		if err := s.validateSubstitution(head, colors); err != nil {
			return err
		}
	}
	memberDeclaration := s.elements[member]
	headDeclaration := s.elements[head]
	if memberDeclaration.Type.Local == "" && memberDeclaration.InlineSimpleType == nil &&
		memberDeclaration.InlineComplexType == nil {
		memberDeclaration.Type = headDeclaration.Type
		memberDeclaration.InlineSimpleType = headDeclaration.InlineSimpleType
		memberDeclaration.InlineComplexType = headDeclaration.InlineComplexType
		s.elements[member] = memberDeclaration
	}
	headType := headDeclaration.Type
	methods, derived := s.elementTypeDerivationMethods(memberDeclaration, headType)
	if !derived {
		return invalidComponent(
			"element",
			member,
			"type is not validly derived from its substitution group head",
		)
	}
	for _, method := range methods {
		if headDeclaration.Final.Contains(method) {
			return invalidComponent("element", member, "head excludes the type derivation method")
		}
	}
	colors[member] = 2
	return nil
}

func (s *compileState) elementTypeDerivationMethods(
	element xsd.Element,
	head xsd.QName,
) ([]xsd.Derivation, bool) {
	if element.InlineComplexType != nil {
		methods := []xsd.Derivation{element.InlineComplexType.Derivation}
		rest, ok := s.typeDerivationMethods(element.InlineComplexType.Base, head)
		return append(methods, rest...), ok
	}
	if element.InlineSimpleType != nil {
		base := element.InlineSimpleType.Base
		if element.InlineSimpleType.InlineBase != nil {
			base = element.InlineSimpleType.InlineBase.Base
		}
		rest, ok := s.typeDerivationMethods(base, head)
		return append([]xsd.Derivation{xsd.DerivationRestriction}, rest...), ok
	}
	return s.typeDerivationMethods(element.Type, head)
}

func (s *compileState) typeDerivationMethods(
	derived xsd.QName,
	base xsd.QName,
) ([]xsd.Derivation, bool) {
	if base.Local == "" || base == (xsd.QName{Namespace: xsd.Namespace, Local: "anyType"}) {
		return nil, true
	}
	methods := make([]xsd.Derivation, 0)
	seen := make(map[xsd.QName]struct{})
	for derived != base {
		if _, duplicate := seen[derived]; duplicate {
			return nil, false
		}
		seen[derived] = struct{}{}
		if complexType, ok := s.complexTypes[derived]; ok {
			methods = append(methods, complexType.Derivation)
			derived = complexType.Base
			continue
		}
		if simpleType, ok := s.simpleTypes[derived]; ok {
			method := xsd.Derivation(simpleType.Variety)
			next := simpleType.Base
			if simpleType.Variety == xsd.SimpleList ||
				simpleType.Variety == xsd.SimpleUnion {
				next = xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}
			}
			methods = append(methods, method)
			derived = next
			continue
		}
		if derived.Namespace == xsd.Namespace {
			parent, method, ok := datatype.BuiltInDerivation(derived.Local)
			if !ok {
				return nil, false
			}
			methods = append(methods, xsd.Derivation(method))
			derived = xsd.QName{Namespace: xsd.Namespace, Local: parent}
			continue
		}
		return nil, false
	}
	return methods, true
}

func (s *compileState) expandGroups() error {
	modelColors := make(map[xsd.QName]uint8, len(s.modelGroups))
	for name := range s.modelGroups {
		if _, err := s.expandModelGroup(name, modelColors); err != nil {
			return err
		}
	}
	attributeColors := make(map[xsd.QName]uint8, len(s.attributeGroups))
	for name := range s.attributeGroups {
		if _, err := s.expandAttributeGroup(name, attributeColors); err != nil {
			return err
		}
	}
	for name, typeDefinition := range s.complexTypes {
		content, err := s.expandModelGroupContent(typeDefinition.Content, modelColors)
		if err != nil {
			return err
		}
		typeDefinition.Content = content
		wildcardSeen := typeDefinition.AttributeWildcard != nil
		for _, reference := range typeDefinition.AttributeGroupRefs {
			group, err := s.expandAttributeGroup(reference, attributeColors)
			if err != nil {
				return err
			}
			typeDefinition.Attributes = append(typeDefinition.Attributes, group.Attributes...)
			if group.Wildcard != nil {
				if wildcardSeen {
					typeDefinition.AttributeWildcard = intersectWildcards(
						typeDefinition.AttributeWildcard,
						group.Wildcard,
					)
				} else {
					typeDefinition.AttributeWildcard = cloneWildcard(group.Wildcard)
					wildcardSeen = true
				}
			}
		}
		typeDefinition.AttributeGroupRefs = nil
		s.complexTypes[name] = typeDefinition
	}
	return nil
}

func (s *compileState) expandModelGroup(
	name xsd.QName,
	colors map[xsd.QName]uint8,
) (xsd.ModelGroupDefinition, error) {
	switch colors[name] {
	case 1:
		return xsd.ModelGroupDefinition{}, invalidComponent(
			"model group",
			name,
			"references are recursive",
		)
	case 2:
		return s.modelGroups[name], nil
	}
	group, ok := s.modelGroups[name]
	if !ok {
		return xsd.ModelGroupDefinition{}, unresolvedComponent("model group", name)
	}
	colors[name] = 1
	content, err := s.expandModelGroupContent(group.Content, colors)
	if err != nil {
		return xsd.ModelGroupDefinition{}, err
	}
	group.Content = content
	s.modelGroups[name] = group
	colors[name] = 2
	return group, nil
}

func (s *compileState) expandModelGroupContent(
	content *xsd.ModelGroup,
	colors map[xsd.QName]uint8,
) (*xsd.ModelGroup, error) {
	content = cloneModelGroup(content)
	if content == nil {
		return nil, nil
	}
	for index := range content.Particles {
		particle := &content.Particles[index]
		if particle.GroupRef.Local != "" {
			definition, err := s.expandModelGroup(particle.GroupRef, colors)
			if err != nil {
				return nil, err
			}
			particle.Group = cloneModelGroup(definition.Content)
			particle.GroupRef = xsd.QName{}
			continue
		}
		nested, err := s.expandModelGroupContent(particle.Group, colors)
		if err != nil {
			return nil, err
		}
		particle.Group = nested
	}
	return content, nil
}

func (s *compileState) expandAttributeGroup(
	name xsd.QName,
	colors map[xsd.QName]uint8,
) (xsd.AttributeGroup, error) {
	switch colors[name] {
	case 1:
		return xsd.AttributeGroup{}, invalidComponent(
			"attribute group",
			name,
			"references are recursive",
		)
	case 2:
		return s.attributeGroups[name], nil
	}
	group, ok := s.attributeGroups[name]
	if !ok {
		return xsd.AttributeGroup{}, unresolvedComponent("attribute group", name)
	}
	colors[name] = 1
	attributes := append([]xsd.AttributeUse(nil), group.Attributes...)
	wildcardSeen := group.Wildcard != nil
	for _, reference := range group.References {
		referenced, err := s.expandAttributeGroup(reference, colors)
		if err != nil {
			return xsd.AttributeGroup{}, err
		}
		attributes = append(attributes, referenced.Attributes...)
		if referenced.Wildcard != nil {
			if wildcardSeen {
				group.Wildcard = intersectWildcards(group.Wildcard, referenced.Wildcard)
			} else {
				group.Wildcard = cloneWildcard(referenced.Wildcard)
				wildcardSeen = true
			}
		}
	}
	group.Attributes = attributes
	group.References = nil
	s.attributeGroups[name] = group
	colors[name] = 2
	return group, nil
}

func (s *compileState) compileComplexExtensions() error {
	colors := make(map[xsd.QName]uint8, len(s.complexTypes))
	for name := range s.complexTypes {
		if err := s.compileComplexType(name, colors); err != nil {
			return err
		}
	}
	return nil
}

func (s *compileState) compileAnonymousComplexTypes() error {
	for name, element := range s.elements {
		if err := s.compileElementAnonymousType(&element, name.Namespace); err != nil {
			return err
		}
		s.elements[name] = element
	}
	for name, typeDefinition := range s.complexTypes {
		if err := s.compileAnonymousTypesInGroup(typeDefinition.Content, name.Namespace); err != nil {
			return err
		}
		s.complexTypes[name] = typeDefinition
	}
	return nil
}

func (s *compileState) compileElementAnonymousType(element *xsd.Element, namespace string) error {
	if element.InlineComplexType == nil {
		return nil
	}
	typeDefinition := cloneComplexType(*element.InlineComplexType)
	if err := s.expandAnonymousComplexType(&typeDefinition); err != nil {
		return err
	}
	if err := s.compileAnonymousTypesInGroup(typeDefinition.Content, namespace); err != nil {
		return err
	}
	element.InlineComplexType = &typeDefinition
	return nil
}

func (s *compileState) expandAnonymousComplexType(typeDefinition *xsd.ComplexType) error {
	modelColors := make(map[xsd.QName]uint8, len(s.modelGroups))
	content, err := s.expandModelGroupContent(typeDefinition.Content, modelColors)
	if err != nil {
		return err
	}
	typeDefinition.Content = content
	attributeColors := make(map[xsd.QName]uint8, len(s.attributeGroups))
	wildcardSeen := typeDefinition.AttributeWildcard != nil
	for _, reference := range typeDefinition.AttributeGroupRefs {
		group, expandErr := s.expandAttributeGroup(reference, attributeColors)
		if expandErr != nil {
			return expandErr
		}
		typeDefinition.Attributes = append(typeDefinition.Attributes, group.Attributes...)
		if group.Wildcard != nil {
			if wildcardSeen {
				typeDefinition.AttributeWildcard = intersectWildcards(
					typeDefinition.AttributeWildcard,
					group.Wildcard,
				)
			} else {
				typeDefinition.AttributeWildcard = cloneWildcard(group.Wildcard)
				wildcardSeen = true
			}
		}
	}
	typeDefinition.AttributeGroupRefs = nil

	if typeDefinition.Derivation == "" {
		return nil
	}
	if typeDefinition.Base.Local == "" {
		return fmt.Errorf("%w: anonymous complex type derivation has no base", ErrInvalidComponent)
	}
	if typeDefinition.Derivation != xsd.DerivationExtension &&
		typeDefinition.Derivation != xsd.DerivationRestriction {
		return fmt.Errorf("%w: anonymous complex type has an invalid derivation method", ErrInvalidComponent)
	}
	if typeDefinition.SimpleContent {
		if s.typeExists(typeDefinition.Base, "simple") {
			if typeDefinition.Derivation != xsd.DerivationExtension {
				return fmt.Errorf(
					"%w: anonymous complex type: simple-content derivation from a simple type must be extension",
					ErrInvalidComponent,
				)
			}
			typeDefinition.SimpleBase = typeDefinition.Base
			if err := s.compileSimpleContentValueType(typeDefinition, nil); err != nil {
				return fmt.Errorf("%w: anonymous complex type: %s", ErrInvalidComponent, err)
			}
			return nil
		}
		base, ok := s.complexTypes[typeDefinition.Base]
		if !ok {
			return unresolvedComponent("simple content base", typeDefinition.Base)
		}
		return s.applySimpleContentDerivation(typeDefinition, base)
	}
	if typeDefinition.Base == (xsd.QName{Namespace: xsd.Namespace, Local: "anyType"}) {
		return nil
	}
	base, ok := s.complexTypes[typeDefinition.Base]
	if !ok {
		return unresolvedComponent("complex type", typeDefinition.Base)
	}
	if base.Final.Contains(typeDefinition.Derivation) {
		return fmt.Errorf("%w: anonymous base type prohibits this derivation", ErrInvalidComponent)
	}
	if typeDefinition.Derivation == xsd.DerivationRestriction {
		if err := s.validateComplexRestriction(*typeDefinition, base); err != nil {
			return fmt.Errorf("%w: anonymous complex type: %s", ErrInvalidComponent, err)
		}
		typeDefinition.Attributes = restrictedAttributes(
			base.Attributes,
			typeDefinition.Attributes,
		)
		return nil
	}
	if !typeDefinition.MixedSet {
		typeDefinition.Mixed = base.Mixed
	} else if typeDefinition.Mixed != base.Mixed {
		return fmt.Errorf("%w: anonymous extension changes the base mixed-content policy", ErrInvalidComponent)
	}
	typeDefinition.Content = extendContent(base.Content, typeDefinition.Content)
	typeDefinition.Attributes = append(
		append([]xsd.AttributeUse(nil), base.Attributes...),
		typeDefinition.Attributes...,
	)
	if typeDefinition.AttributeWildcard == nil {
		typeDefinition.AttributeWildcard = cloneWildcard(base.AttributeWildcard)
	} else if base.AttributeWildcard != nil {
		typeDefinition.AttributeWildcard = unionWildcards(
			base.AttributeWildcard,
			typeDefinition.AttributeWildcard,
		)
	}
	return nil
}

func (s *compileState) compileAnonymousTypesInGroup(group *xsd.ModelGroup, namespace string) error {
	if group == nil {
		return nil
	}
	for index := range group.Particles {
		particle := &group.Particles[index]
		if particle.Element != nil {
			if err := s.compileElementAnonymousType(particle.Element, namespace); err != nil {
				return err
			}
		}
		if err := s.compileAnonymousTypesInGroup(particle.Group, namespace); err != nil {
			return err
		}
	}
	return nil
}

func (s *compileState) compileComplexType(
	name xsd.QName,
	colors map[xsd.QName]uint8,
) error {
	switch colors[name] {
	case 1:
		return invalidComponent("complex type", name, "derivation is recursive")
	case 2:
		return nil
	}
	colors[name] = 1
	typeDefinition := cloneComplexType(s.complexTypes[name])
	if typeDefinition.Derivation == "" {
		colors[name] = 2
		return nil
	}
	if typeDefinition.Base.Local == "" {
		return invalidComponent("complex type", name, "derivation has no base")
	}
	if typeDefinition.Derivation != xsd.DerivationExtension &&
		typeDefinition.Derivation != xsd.DerivationRestriction {
		return invalidComponent("complex type", name, "has an invalid derivation method")
	}
	if typeDefinition.SimpleContent {
		if s.typeExists(typeDefinition.Base, "simple") {
			if typeDefinition.Derivation != xsd.DerivationExtension {
				return invalidComponent(
					"complex type",
					name,
					"simple-content derivation from a simple type must be extension",
				)
			}
			typeDefinition.SimpleBase = typeDefinition.Base
			if err := s.compileSimpleContentValueType(&typeDefinition, nil); err != nil {
				return invalidComponent("complex type", name, err.Error())
			}
			s.complexTypes[name] = typeDefinition
			colors[name] = 2
			return nil
		}
		_, ok := s.complexTypes[typeDefinition.Base]
		if !ok {
			return unresolvedComponent("simple content base", typeDefinition.Base)
		}
		if err := s.compileComplexType(typeDefinition.Base, colors); err != nil {
			return err
		}
		base := s.complexTypes[typeDefinition.Base]
		if err := s.applySimpleContentDerivation(&typeDefinition, base); err != nil {
			return invalidComponent("complex type", name, err.Error())
		}
		s.complexTypes[name] = typeDefinition
		colors[name] = 2
		return nil
	}
	if typeDefinition.Base == (xsd.QName{Namespace: xsd.Namespace, Local: "anyType"}) {
		colors[name] = 2
		return nil
	}
	base, ok := s.complexTypes[typeDefinition.Base]
	if !ok {
		return unresolvedComponent("complex type", typeDefinition.Base)
	}
	if err := s.compileComplexType(typeDefinition.Base, colors); err != nil {
		return err
	}
	base = s.complexTypes[typeDefinition.Base]
	if base.Final.Contains(typeDefinition.Derivation) {
		return invalidComponent("complex type", name, "base type prohibits this derivation")
	}
	if typeDefinition.Derivation == xsd.DerivationRestriction {
		if err := s.validateComplexRestriction(typeDefinition, base); err != nil {
			return invalidComponent("complex type", name, err.Error())
		}
		typeDefinition.Attributes = restrictedAttributes(
			base.Attributes,
			typeDefinition.Attributes,
		)
		s.complexTypes[name] = typeDefinition
		colors[name] = 2
		return nil
	}
	if !typeDefinition.MixedSet {
		typeDefinition.Mixed = base.Mixed
	} else if typeDefinition.Mixed != base.Mixed {
		return invalidComponent(
			"complex type",
			name,
			"extension changes the base mixed-content policy",
		)
	}
	typeDefinition.Content = extendContent(base.Content, typeDefinition.Content)
	typeDefinition.Attributes = append(
		append([]xsd.AttributeUse(nil), base.Attributes...),
		typeDefinition.Attributes...,
	)
	if typeDefinition.AttributeWildcard == nil {
		typeDefinition.AttributeWildcard = cloneWildcard(base.AttributeWildcard)
	} else if base.AttributeWildcard != nil {
		typeDefinition.AttributeWildcard = unionWildcards(
			base.AttributeWildcard,
			typeDefinition.AttributeWildcard,
		)
	}
	s.complexTypes[name] = typeDefinition
	colors[name] = 2
	return nil
}

func (s *compileState) applySimpleContentDerivation(
	derived *xsd.ComplexType,
	base xsd.ComplexType,
) error {
	if base.Final.Contains(derived.Derivation) {
		return errors.New("base type prohibits this derivation")
	}
	var inherited *xsd.SimpleType
	if base.SimpleContent || base.SimpleBase.Local != "" ||
		s.typeExists(base.Base, "simple") {
		derived.SimpleBase = base.SimpleBase
		if derived.SimpleBase.Local == "" {
			derived.SimpleBase = base.Base
		}
		inherited = base.InlineSimpleType
		if derived.Derivation == xsd.DerivationRestriction &&
			derived.InlineSimpleType != nil &&
			(inherited != nil || !s.inlineSimpleTypeDerivesFrom(
				*derived.InlineSimpleType,
				derived.SimpleBase,
			)) {
			return errors.New(
				"inline simple content type is not derived from its base content type",
			)
		}
	} else {
		if derived.Derivation != xsd.DerivationRestriction || !base.Mixed ||
			!modelGroupNullable(base.Content) || derived.InlineSimpleType == nil {
			return errors.New(
				"simple content base must have simple content or emptiable mixed content",
			)
		}
	}
	if err := s.compileSimpleContentValueType(derived, inherited); err != nil {
		return err
	}
	if derived.Derivation == xsd.DerivationRestriction {
		if !s.attributesRestrictContext(
			derived.Attributes,
			base.Attributes,
			base.AttributeWildcard,
			derived.Base.Namespace,
		) {
			return errors.New("attribute uses are not a valid restriction of their base")
		}
		if derived.AttributeWildcard != nil &&
			(base.AttributeWildcard == nil ||
				!wildcardRestricts(derived.AttributeWildcard, base.AttributeWildcard)) {
			return errors.New("attribute wildcard is not a valid restriction of its base")
		}
		derived.Attributes = restrictedAttributes(base.Attributes, derived.Attributes)
		return nil
	}
	derived.Attributes = append(
		append([]xsd.AttributeUse(nil), base.Attributes...),
		derived.Attributes...,
	)
	if derived.AttributeWildcard == nil {
		derived.AttributeWildcard = cloneWildcard(base.AttributeWildcard)
	} else if base.AttributeWildcard != nil {
		derived.AttributeWildcard = unionWildcards(base.AttributeWildcard, derived.AttributeWildcard)
	}
	return nil
}

func (s *compileState) compileSimpleContentValueType(
	derived *xsd.ComplexType,
	inherited *xsd.SimpleType,
) error {
	if derived.Derivation == xsd.DerivationExtension {
		if derived.InlineSimpleType != nil || len(derived.SimpleFacets) != 0 {
			return errors.New("simple-content extension cannot declare facets")
		}
		if inherited != nil {
			typeDefinition := cloneSimpleType(*inherited)
			derived.InlineSimpleType = &typeDefinition
		}
		return nil
	}
	restriction := xsd.SimpleType{
		Variety: xsd.SimpleRestriction,
		Facets:  append([]xsd.Facet(nil), derived.SimpleFacets...),
	}
	if derived.InlineSimpleType != nil {
		base := cloneSimpleType(*derived.InlineSimpleType)
		restriction.InlineBase = &base
	} else if inherited != nil {
		base := cloneSimpleType(*inherited)
		restriction.InlineBase = &base
	} else {
		restriction.Base = derived.SimpleBase
	}
	if err := s.validateSimpleTypeDefinition(restriction); err != nil {
		return fmt.Errorf("simple-content restriction: %w", err)
	}
	derived.InlineSimpleType = &restriction
	derived.SimpleFacets = nil
	return nil
}

func (s *compileState) validateComplexRestriction(derived, base xsd.ComplexType) error {
	if derived.Mixed && !base.Mixed {
		return errors.New("restriction enables mixed content")
	}
	if !s.modelGroupRestricts(derived.Content, base.Content) {
		return errors.New("content model is not a valid restriction of its base")
	}
	if !s.attributesRestrictContext(
		derived.Attributes,
		base.Attributes,
		base.AttributeWildcard,
		derived.Base.Namespace,
	) {
		return errors.New("attribute uses are not a valid restriction of their base")
	}
	if derived.AttributeWildcard != nil &&
		(base.AttributeWildcard == nil || !wildcardRestricts(derived.AttributeWildcard, base.AttributeWildcard)) {
		return errors.New("attribute wildcard is not a valid restriction of its base")
	}
	return nil
}

func modelGroupRestricts(derived, base *xsd.ModelGroup) bool {
	return (&compileState{}).modelGroupRestricts(derived, base)
}

func (s *compileState) modelGroupRestricts(derived, base *xsd.ModelGroup) bool {
	if derived == nil {
		return base == nil || modelGroupNullable(base)
	}
	if base == nil || derived.Compositor != base.Compositor {
		return false
	}

	switch derived.Compositor {
	case xsd.Sequence:
		baseIndex := 0
		for _, particle := range derived.Particles {
			for baseIndex < len(base.Particles) &&
				!s.particleRestricts(particle, base.Particles[baseIndex]) {
				if !particleNullable(base.Particles[baseIndex]) {
					return false
				}
				baseIndex++
			}
			if baseIndex == len(base.Particles) {
				return false
			}
			baseIndex++
		}
		for ; baseIndex < len(base.Particles); baseIndex++ {
			if !particleNullable(base.Particles[baseIndex]) {
				return false
			}
		}
		return true
	case xsd.Choice, xsd.All:
		for _, particle := range derived.Particles {
			matched := false
			for _, baseParticle := range base.Particles {
				if s.particleRestricts(particle, baseParticle) {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		if derived.Compositor == xsd.All {
			for _, baseParticle := range base.Particles {
				if baseParticle.MinOccurs == 0 {
					continue
				}
				matched := false
				for _, particle := range derived.Particles {
					if s.particleRestricts(particle, baseParticle) {
						matched = true
						break
					}
				}
				if !matched {
					return false
				}
			}
		}
		return true
	default:
		return false
	}
}

func modelGroupNullable(group *xsd.ModelGroup) bool {
	if group == nil || len(group.Particles) == 0 {
		return true
	}
	if group.Compositor == xsd.Choice {
		for _, particle := range group.Particles {
			if particleNullable(particle) {
				return true
			}
		}
		return false
	}
	for _, particle := range group.Particles {
		if !particleNullable(particle) {
			return false
		}
	}
	return true
}

func particleRestricts(derived, base xsd.Particle) bool {
	return (&compileState{}).particleRestricts(derived, base)
}

func (s *compileState) particleRestricts(derived, base xsd.Particle) bool {
	if derived.MinOccurs < base.MinOccurs ||
		(!base.Unbounded && (derived.Unbounded || derived.MaxOccurs > base.MaxOccurs)) {
		return false
	}
	if derived.Element != nil && base.Element != nil {
		return s.elementTermRestricts(*derived.Element, *base.Element)
	}
	if derived.Group != nil && base.Group != nil {
		return s.modelGroupRestricts(derived.Group, base.Group)
	}
	if derived.Wildcard != nil && base.Wildcard != nil {
		return wildcardRestricts(derived.Wildcard, base.Wildcard)
	}
	return false
}

func elementTermEqual(derived, base xsd.Element) bool {
	return (&compileState{}).elementTermRestricts(derived, base)
}

func (s *compileState) elementTermRestricts(derived, base xsd.Element) bool {
	if derived.Ref.Local != "" || base.Ref.Local != "" {
		return derived.Ref == base.Ref
	}
	if derived.Name != base.Name || derived.Namespace != base.Namespace ||
		derived.Nillable && !base.Nillable ||
		elementFixedSet(base) &&
			(!elementFixedSet(derived) || derived.Fixed != base.Fixed) {
		return false
	}
	if base.InlineSimpleType != nil || base.InlineComplexType != nil {
		return false
	}
	baseType := base.Type
	if baseType.Local == "" {
		baseType = xsd.QName{Namespace: xsd.Namespace, Local: "anyType"}
	}
	_, valid := s.elementTypeDerivationMethods(derived, baseType)
	return valid
}

func (s *compileState) attributesRestrict(
	derived []xsd.AttributeUse,
	base []xsd.AttributeUse,
	baseWildcard *xsd.Wildcard,
) bool {
	return s.attributesRestrictContext(derived, base, baseWildcard, "")
}

func (s *compileState) attributesRestrictContext(
	derived []xsd.AttributeUse,
	base []xsd.AttributeUse,
	baseWildcard *xsd.Wildcard,
	targetNamespace string,
) bool {
	uses := make(map[xsd.QName]xsd.AttributeUse, len(derived))
	for _, attribute := range derived {
		uses[attributeUseName(attribute)] = attribute
	}
	for _, baseAttribute := range base {
		attribute, ok := uses[attributeUseName(baseAttribute)]
		if !ok {
			if baseAttribute.Use == xsd.AttributeRequired {
				return false
			}
			continue
		}
		delete(uses, attributeUseName(baseAttribute))
		baseFixed, baseFixedSet := s.attributeUseFixedConstraint(baseAttribute)
		fixed, fixedSet := s.attributeUseFixedConstraint(attribute)
		if baseFixedSet && (!fixedSet || fixed != baseFixed) ||
			(baseAttribute.Use == xsd.AttributeRequired && attribute.Use != xsd.AttributeRequired) {
			return false
		}
		if attribute.Use == xsd.AttributeProhibited {
			continue
		}
		if !s.attributeUseTypeRestricts(attribute, baseAttribute) {
			return false
		}
	}
	for name, attribute := range uses {
		if attribute.Use != xsd.AttributeProhibited &&
			!wildcardAllows(baseWildcard, name.Namespace, targetNamespace) {
			return false
		}
	}
	return true
}

func (s *compileState) attributeUseTypeRestricts(
	derived xsd.AttributeUse,
	base xsd.AttributeUse,
) bool {
	if derived.Ref.Local != "" && derived.Ref == base.Ref {
		_, ok := s.attributes[derived.Ref]
		return ok
	}
	baseType, baseInline, ok := s.attributeUseType(base)
	if !ok || baseInline != nil {
		return false
	}
	derivedType, derivedInline, ok := s.attributeUseType(derived)
	if !ok {
		return false
	}
	if derivedInline != nil {
		return s.inlineSimpleTypeDerivesFrom(*derivedInline, baseType)
	}
	return s.simpleTypeDerivesFrom(derivedType, baseType)
}

func (s *compileState) attributeUseType(
	attribute xsd.AttributeUse,
) (xsd.QName, *xsd.SimpleType, bool) {
	if attribute.Ref.Local != "" {
		declaration, ok := s.attributes[attribute.Ref]
		if !ok {
			return xsd.QName{}, nil, false
		}
		if declaration.InlineSimpleType != nil {
			return xsd.QName{}, declaration.InlineSimpleType, true
		}
		typeName := declaration.Type
		if typeName.Local == "" {
			typeName = xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}
		}
		return typeName, nil, true
	}
	if attribute.InlineSimpleType != nil {
		return xsd.QName{}, attribute.InlineSimpleType, true
	}
	typeName := attribute.Type
	if typeName.Local == "" {
		typeName = xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}
	}
	return typeName, nil, true
}

func (s *compileState) attributeUseFixedConstraint(
	attribute xsd.AttributeUse,
) (string, bool) {
	if attributeFixedSet(attribute) {
		return attribute.Fixed, true
	}
	if attribute.Ref.Local == "" {
		return "", false
	}
	declaration, ok := s.attributes[attribute.Ref]
	if !ok || !attributeDeclarationFixedSet(declaration) {
		return "", false
	}
	return declaration.Fixed, true
}

func (s *compileState) simpleTypeDerivesFrom(derived, base xsd.QName) bool {
	if base.Local == "" || base == (xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}) {
		return derived.Local != ""
	}
	seen := make(map[xsd.QName]struct{})
	for derived.Local != "" {
		if derived == base {
			return true
		}
		if _, duplicate := seen[derived]; duplicate {
			return false
		}
		seen[derived] = struct{}{}
		if definition, ok := s.simpleTypes[derived]; ok {
			derived = definition.Base
			continue
		}
		if derived.Namespace != xsd.Namespace {
			return false
		}
		parent, ok := datatype.BuiltInBase(derived.Local)
		if !ok {
			return false
		}
		derived = xsd.QName{Namespace: xsd.Namespace, Local: parent}
	}
	return false
}

func restrictedAttributes(base, derived []xsd.AttributeUse) []xsd.AttributeUse {
	result := append([]xsd.AttributeUse(nil), base...)
	indexes := make(map[xsd.QName]int, len(result))
	for index, attribute := range result {
		indexes[attributeUseName(attribute)] = index
	}
	for _, attribute := range derived {
		name := attributeUseName(attribute)
		if index, ok := indexes[name]; ok {
			result[index] = attribute
			continue
		}
		indexes[name] = len(result)
		result = append(result, attribute)
	}
	return result
}

func attributeUseName(attribute xsd.AttributeUse) xsd.QName {
	if attribute.Ref.Local != "" {
		return attribute.Ref
	}
	return xsd.QName{Namespace: attribute.Namespace, Local: attribute.Name}
}

func wildcardRestricts(derived, base *xsd.Wildcard) bool {
	if derived.ProcessContents == xsd.ProcessSkip && base.ProcessContents != xsd.ProcessSkip ||
		derived.ProcessContents == xsd.ProcessLax && base.ProcessContents == xsd.ProcessStrict {
		return false
	}
	baseNamespaces := make(map[string]struct{}, len(base.Namespaces))
	for _, namespace := range base.Namespaces {
		baseNamespaces[namespace] = struct{}{}
	}
	for _, namespace := range derived.Namespaces {
		if _, ok := baseNamespaces[namespace]; !ok {
			return false
		}
	}
	return true
}

func intersectWildcards(left, right *xsd.Wildcard) *xsd.Wildcard {
	if wildcardHas(left, "##any") {
		return cloneWildcard(right)
	}
	if wildcardHas(right, "##any") {
		return cloneWildcard(left)
	}
	rightNamespaces := make(map[string]struct{}, len(right.Namespaces))
	for _, namespace := range right.Namespaces {
		rightNamespaces[namespace] = struct{}{}
	}
	namespaces := make([]string, 0)
	for _, namespace := range left.Namespaces {
		if _, ok := rightNamespaces[namespace]; ok {
			namespaces = append(namespaces, namespace)
		}
	}
	if len(namespaces) == 0 {
		return nil
	}
	return &xsd.Wildcard{
		Namespaces:      namespaces,
		ProcessContents: strongerProcessContents(left.ProcessContents, right.ProcessContents),
	}
}

func unionWildcards(left, right *xsd.Wildcard) *xsd.Wildcard {
	if wildcardHas(left, "##any") || wildcardHas(right, "##any") {
		return &xsd.Wildcard{
			Namespaces:      []string{"##any"},
			ProcessContents: weakerProcessContents(left.ProcessContents, right.ProcessContents),
		}
	}
	namespaces := append([]string(nil), left.Namespaces...)
	seen := make(map[string]struct{}, len(namespaces))
	for _, namespace := range namespaces {
		seen[namespace] = struct{}{}
	}
	for _, namespace := range right.Namespaces {
		if _, ok := seen[namespace]; !ok {
			namespaces = append(namespaces, namespace)
			seen[namespace] = struct{}{}
		}
	}
	return &xsd.Wildcard{
		Namespaces:      namespaces,
		ProcessContents: weakerProcessContents(left.ProcessContents, right.ProcessContents),
	}
}

func wildcardHas(wildcard *xsd.Wildcard, namespace string) bool {
	for _, candidate := range wildcard.Namespaces {
		if candidate == namespace {
			return true
		}
	}
	return false
}

func strongerProcessContents(left, right xsd.ProcessContents) xsd.ProcessContents {
	if processContentsRank(left) >= processContentsRank(right) {
		return left
	}
	return right
}

func weakerProcessContents(left, right xsd.ProcessContents) xsd.ProcessContents {
	if processContentsRank(left) <= processContentsRank(right) {
		return left
	}
	return right
}

func processContentsRank(value xsd.ProcessContents) int {
	switch value {
	case xsd.ProcessStrict:
		return 3
	case xsd.ProcessLax:
		return 2
	default:
		return 1
	}
}

func extendContent(base *xsd.ModelGroup, extension *xsd.ModelGroup) *xsd.ModelGroup {
	if base == nil {
		return cloneModelGroup(extension)
	}
	if extension == nil {
		return cloneModelGroup(base)
	}
	return &xsd.ModelGroup{
		Compositor: xsd.Sequence,
		Particles: []xsd.Particle{
			{MinOccurs: 1, MaxOccurs: 1, Group: cloneModelGroup(base)},
			{MinOccurs: 1, MaxOccurs: 1, Group: cloneModelGroup(extension)},
		},
	}
}

func (s *compileState) validateComponents() error {
	if err := s.validateIdentityConstraints(); err != nil {
		return err
	}
	for name, element := range s.elements {
		if elementDefaultSet(element) && elementFixedSet(element) {
			return invalidComponent("element", name, "default and fixed are mutually exclusive")
		}
		if element.Ref.Local != "" {
			return invalidComponent("global element", name, "ref is not allowed")
		}
		if element.Type.Local != "" && !s.typeExists(element.Type, "") {
			return unresolvedComponent("type", element.Type)
		}
		if directNotationType(element.Type) {
			return invalidComponent("element", name, "NOTATION cannot be used directly")
		}
		if err := s.validateAnonymousElementType(element, name.Namespace, nil); err != nil {
			return err
		}
		if err := s.validateElementValueConstraint(element); err != nil {
			return invalidComponent("element", name, err.Error())
		}
	}
	for name, attribute := range s.attributes {
		if attributeDeclarationDefaultSet(attribute) && attributeDeclarationFixedSet(attribute) {
			return invalidComponent("attribute", name, "default and fixed are mutually exclusive")
		}
		if attribute.Type.Local != "" && !s.typeExists(attribute.Type, "simple") {
			return unresolvedComponent("simple type", attribute.Type)
		}
		if directNotationType(attribute.Type) {
			return invalidComponent("attribute", name, "NOTATION cannot be used directly")
		}
		if attribute.Type.Local != "" && attribute.InlineSimpleType != nil {
			return invalidComponent(
				"attribute",
				name,
				"has more than one type definition",
			)
		}
		if attribute.InlineSimpleType != nil {
			if err := s.validateSimpleTypeDefinition(*attribute.InlineSimpleType); err != nil {
				return err
			}
		}
		if err := s.validateAttributeDeclarationValueConstraint(attribute); err != nil {
			return invalidComponent("attribute", name, err.Error())
		}
	}
	for name, simpleType := range s.simpleTypes {
		if datatype.ValidateBuiltInLexical("NCName", simpleType.Name) != nil {
			return invalidComponent("simple type", name, "name is not an NCName")
		}
		if err := s.validateSimpleTypeDefinition(simpleType); err != nil {
			return invalidComponent("simple type", name, err.Error())
		}
	}
	colors := make(map[xsd.QName]uint8, len(s.simpleTypes))
	for name := range s.simpleTypes {
		if err := s.validateSimpleTypeAcyclic(name, colors); err != nil {
			return err
		}
	}
	particles := 0
	for name, element := range s.elements {
		if err := s.validateAnonymousElementType(element, name.Namespace, &particles); err != nil {
			return err
		}
	}
	for _, group := range s.modelGroups {
		if err := s.validateModelGroup(group.Content, "", &particles); err != nil {
			return err
		}
	}
	for _, group := range s.attributeGroups {
		if err := validateWildcard(group.Wildcard); err != nil {
			return err
		}
		if err := s.validateAttributeUseSet(group.Attributes); err != nil {
			return err
		}
	}
	for name, complexType := range s.complexTypes {
		if err := s.validateModelGroup(complexType.Content, name.Namespace, &particles); err != nil {
			return err
		}
		if err := validateWildcard(complexType.AttributeWildcard); err != nil {
			return err
		}
		for _, attribute := range complexType.Attributes {
			if err := s.validateAttributeUse(attribute, name.Namespace); err != nil {
				return err
			}
		}
		if err := s.validateAttributeUseSet(complexType.Attributes); err != nil {
			return err
		}
	}
	return nil
}

func (s *compileState) validateAnonymousElementType(
	element xsd.Element,
	namespace string,
	particles *int,
) error {
	typeCount := 0
	if element.Type.Local != "" {
		typeCount++
		if directNotationType(element.Type) {
			return fmt.Errorf("%w: NOTATION cannot be used directly", ErrInvalidComponent)
		}
	}
	if element.InlineSimpleType != nil {
		typeCount++
		if err := s.validateSimpleTypeDefinition(*element.InlineSimpleType); err != nil {
			return err
		}
	}
	if element.InlineComplexType != nil {
		typeCount++
		if err := s.validateAttributeUseSet(
			element.InlineComplexType.Attributes,
		); err != nil {
			return err
		}
		if particles != nil {
			if err := s.validateModelGroup(
				element.InlineComplexType.Content,
				namespace,
				particles,
			); err != nil {
				return err
			}
			for _, attribute := range element.InlineComplexType.Attributes {
				if err := s.validateAttributeUse(attribute, namespace); err != nil {
					return err
				}
			}
		}
	}
	if typeCount > 1 {
		return fmt.Errorf(
			"%w: element has more than one type definition",
			ErrInvalidComponent,
		)
	}
	return nil
}

func (s *compileState) validateAttributeUseSet(attributes []xsd.AttributeUse) error {
	seen := make(map[xsd.QName]struct{}, len(attributes))
	idSeen := false
	id := xsd.QName{Namespace: xsd.Namespace, Local: "ID"}
	for _, attribute := range attributes {
		name := attributeUseName(attribute)
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf(
				"%w: duplicate attribute use {%s}%s",
				ErrInvalidComponent,
				name.Namespace,
				name.Local,
			)
		}
		seen[name] = struct{}{}
		if attribute.Use == xsd.AttributeProhibited {
			continue
		}
		typeName, inline, ok := s.attributeUseType(attribute)
		if !ok {
			continue
		}
		isID := inline != nil && s.inlineSimpleTypeDerivesFrom(*inline, id) ||
			inline == nil && s.simpleTypeDerivesFrom(typeName, id)
		if !isID {
			continue
		}
		if idSeen {
			return fmt.Errorf(
				"%w: an attribute-use set cannot contain multiple ID types",
				ErrInvalidComponent,
			)
		}
		idSeen = true
	}
	return nil
}

func (s *compileState) validateSimpleTypeDefinition(typeDefinition xsd.SimpleType) error {
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		if typeDefinition.Base.Local != "" && typeDefinition.InlineBase != nil {
			return fmt.Errorf("%w: simple restriction has more than one base", ErrInvalidComponent)
		}
		if typeDefinition.InlineBase != nil {
			if err := s.validateSimpleTypeDefinition(*typeDefinition.InlineBase); err != nil {
				return err
			}
		} else {
			if typeDefinition.Base.Local == "" || !s.typeExists(typeDefinition.Base, "simple") {
				return unresolvedComponent("simple type", typeDefinition.Base)
			}
			if base, ok := s.simpleTypes[typeDefinition.Base]; ok &&
				base.Final.Contains(xsd.DerivationRestriction) {
				return fmt.Errorf("%w: base type prohibits restriction", ErrInvalidComponent)
			}
		}
		if err := s.validateRestrictionFacets(typeDefinition); err != nil {
			return err
		}
	case xsd.SimpleList:
		if typeDefinition.ItemType.Local != "" && typeDefinition.InlineItem != nil {
			return fmt.Errorf("%w: list has more than one item type", ErrInvalidComponent)
		}
		if typeDefinition.InlineItem != nil {
			if err := s.validateSimpleTypeDefinition(*typeDefinition.InlineItem); err != nil {
				return err
			}
			if s.definitionShape(*typeDefinition.InlineItem, 0).variety == listShape {
				return fmt.Errorf("%w: list item type cannot itself be a list", ErrInvalidComponent)
			}
		} else {
			if typeDefinition.ItemType.Local == "" || !s.typeExists(typeDefinition.ItemType, "simple") {
				return unresolvedComponent("simple type", typeDefinition.ItemType)
			}
			if item, ok := s.simpleTypes[typeDefinition.ItemType]; ok &&
				item.Final.Contains(xsd.DerivationList) {
				return fmt.Errorf("%w: item type prohibits list derivation", ErrInvalidComponent)
			}
			if s.namedShape(typeDefinition.ItemType, 0).variety == listShape {
				return fmt.Errorf("%w: list item type cannot itself be a list", ErrInvalidComponent)
			}
			if directNotationType(typeDefinition.ItemType) {
				return fmt.Errorf("%w: NOTATION cannot be used directly as a list item", ErrInvalidComponent)
			}
		}
	case xsd.SimpleUnion:
		if len(typeDefinition.MemberTypes) == 0 && len(typeDefinition.InlineMembers) == 0 {
			return fmt.Errorf("%w: anonymous union has no member types", ErrInvalidComponent)
		}
		for _, member := range typeDefinition.MemberTypes {
			if !s.typeExists(member, "simple") {
				return unresolvedComponent("simple type", member)
			}
			if definition, ok := s.simpleTypes[member]; ok &&
				definition.Final.Contains(xsd.DerivationUnion) {
				return fmt.Errorf("%w: member type prohibits union derivation", ErrInvalidComponent)
			}
			if directNotationType(member) {
				return fmt.Errorf("%w: NOTATION cannot be used directly as a union member", ErrInvalidComponent)
			}
		}
		for _, member := range typeDefinition.InlineMembers {
			if err := s.validateSimpleTypeDefinition(member); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%w: anonymous simple type has no variety", ErrInvalidComponent)
	}
	return nil
}

func (s *compileState) validateIdentityConstraints() error {
	constraints := make(map[xsd.QName]xsd.IdentityConstraint)
	collect := func(namespace string, element xsd.Element) error {
		for _, constraint := range element.IdentityConstraints {
			name := xsd.QName{Namespace: namespace, Local: constraint.Name}
			if constraint.Name == "" || constraint.Selector == "" || len(constraint.Fields) == 0 {
				return invalidComponent(
					"identity constraint",
					name,
					"requires a name, selector, and at least one field",
				)
			}
			if _, duplicate := constraints[name]; duplicate {
				return duplicateComponent("identity constraint", name)
			}
			if !validIdentitySelector(constraint.Selector, constraint.Namespaces) {
				return invalidComponent(
					"identity constraint",
					name,
					"selector is outside the XML Schema XPath subset",
				)
			}
			for _, field := range constraint.Fields {
				if !validIdentityField(field, constraint.Namespaces) {
					return invalidComponent(
						"identity constraint",
						name,
						"field is outside the XML Schema XPath subset",
					)
				}
			}
			if constraint.Kind == xsd.IdentityKeyRef && constraint.Refer.Local == "" {
				return invalidComponent(
					"identity constraint",
					name,
					"keyref requires refer",
				)
			}
			if constraint.Kind != xsd.IdentityKeyRef && constraint.Refer.Local != "" {
				return invalidComponent(
					"identity constraint",
					name,
					"only keyref permits refer",
				)
			}
			constraints[name] = constraint
		}
		return nil
	}
	for name, element := range s.elements {
		if err := collect(name.Namespace, element); err != nil {
			return err
		}
		if element.InlineComplexType != nil {
			if err := collectModelIdentityConstraints(
				element.InlineComplexType.Content,
				name.Namespace,
				collect,
			); err != nil {
				return err
			}
		}
	}
	for name, typeDefinition := range s.complexTypes {
		if err := collectModelIdentityConstraints(typeDefinition.Content, name.Namespace, collect); err != nil {
			return err
		}
	}
	for name, constraint := range constraints {
		if constraint.Kind != xsd.IdentityKeyRef {
			continue
		}
		referenced, ok := constraints[constraint.Refer]
		if !ok || referenced.Kind == xsd.IdentityKeyRef {
			return unresolvedComponent("identity constraint", constraint.Refer)
		}
		if len(referenced.Fields) != len(constraint.Fields) {
			return invalidComponent(
				"identity constraint",
				name,
				"keyref field count differs from its referenced constraint",
			)
		}
	}
	return nil
}

func validIdentitySelector(expression string, namespaces map[string]string) bool {
	expression = xsd.NormalizeIdentityXPath(expression)
	for _, branch := range strings.Split(expression, "|") {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			return false
		}
		if branch == "." {
			continue
		}
		branch = strings.TrimPrefix(branch, ".//")
		branch = strings.TrimPrefix(branch, "./")
		for _, step := range strings.Split(branch, "/") {
			if step == "." {
				continue
			}
			step = strings.TrimPrefix(step, "child::")
			if !validIdentityName(step, namespaces, true) {
				return false
			}
		}
	}
	return true
}

func validIdentityField(expression string, namespaces map[string]string) bool {
	expression = xsd.NormalizeIdentityXPath(expression)
	for _, branch := range strings.Split(expression, "|") {
		branch = strings.TrimSpace(branch)
		if branch == "." {
			continue
		}
		branch = strings.TrimPrefix(branch, ".//")
		branch = strings.TrimPrefix(branch, "./")
		steps := strings.Split(branch, "/")
		for index, step := range steps {
			attribute := strings.HasPrefix(step, "@") || strings.HasPrefix(step, "attribute::")
			if attribute {
				if index != len(steps)-1 {
					return false
				}
				step = strings.TrimPrefix(step, "@")
				step = strings.TrimPrefix(step, "attribute::")
			}
			if step == "." && !attribute {
				continue
			}
			step = strings.TrimPrefix(step, "child::")
			if !validIdentityName(step, namespaces, true) {
				return false
			}
		}
	}
	return true
}

func validIdentityName(
	expression string,
	namespaces map[string]string,
	allowWildcard bool,
) bool {
	if expression == "*" {
		return allowWildcard
	}
	parts := strings.Split(expression, ":")
	if len(parts) == 1 {
		return datatype.ValidateBuiltInLexical("NCName", parts[0]) == nil
	}
	if len(parts) != 2 ||
		datatype.ValidateBuiltInLexical("NCName", parts[0]) != nil ||
		(parts[1] != "*" && datatype.ValidateBuiltInLexical("NCName", parts[1]) != nil) {
		return false
	}
	_, declared := namespaces[parts[0]]
	return declared
}

func collectModelIdentityConstraints(
	group *xsd.ModelGroup,
	namespace string,
	collect func(string, xsd.Element) error,
) error {
	if group == nil {
		return nil
	}
	for _, particle := range group.Particles {
		if particle.Element != nil {
			if err := collect(namespace, *particle.Element); err != nil {
				return err
			}
			if particle.Element.InlineComplexType != nil {
				if err := collectModelIdentityConstraints(
					particle.Element.InlineComplexType.Content,
					namespace,
					collect,
				); err != nil {
					return err
				}
			}
		}
		if err := collectModelIdentityConstraints(particle.Group, namespace, collect); err != nil {
			return err
		}
	}
	return nil
}

func (s *compileState) validateSimpleTypeAcyclic(
	name xsd.QName,
	colors map[xsd.QName]uint8,
) error {
	switch colors[name] {
	case 1:
		return invalidComponent("simple type", name, "derivation is recursive")
	case 2:
		return nil
	}
	colors[name] = 1
	typeDefinition := s.simpleTypes[name]
	dependencies := []xsd.QName{}
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		dependencies = append(dependencies, typeDefinition.Base)
	case xsd.SimpleList:
		dependencies = append(dependencies, typeDefinition.ItemType)
	case xsd.SimpleUnion:
		dependencies = append(dependencies, typeDefinition.MemberTypes...)
	}
	for _, dependency := range dependencies {
		if _, userDefined := s.simpleTypes[dependency]; !userDefined {
			continue
		}
		if err := s.validateSimpleTypeAcyclic(dependency, colors); err != nil {
			return err
		}
	}
	colors[name] = 2
	return nil
}

func (s *compileState) validateModelGroup(
	group *xsd.ModelGroup,
	typeNamespace string,
	particles *int,
) error {
	if group == nil {
		return nil
	}
	if err := s.validateUniqueParticleAttribution(group, typeNamespace); err != nil {
		return err
	}
	for _, particle := range group.Particles {
		*particles++
		if *particles > s.compiler.limits.MaxParticles {
			return fmt.Errorf(
				"%w: particle count exceeds %d",
				ErrLimitExceeded,
				s.compiler.limits.MaxParticles,
			)
		}
		if group.Compositor == xsd.All {
			if particle.Group != nil || particle.Wildcard != nil || particle.Unbounded || particle.MaxOccurs > 1 ||
				particle.MinOccurs > 1 {
				return fmt.Errorf(
					"%w: all compositor particles must be elements with 0..1 or 1..1 occurrence",
					ErrInvalidComponent,
				)
			}
		}
		if particle.Element != nil {
			element := particle.Element
			if element.SubstitutionGroup.Local != "" {
				return fmt.Errorf(
					"%w: local element cannot declare a substitution group",
					ErrInvalidComponent,
				)
			}
			if (element.Name == "") == (element.Ref.Local == "") {
				return fmt.Errorf(
					"%w: local element must have exactly one of name or ref",
					ErrInvalidComponent,
				)
			}
			if elementDefaultSet(*element) && elementFixedSet(*element) {
				return fmt.Errorf(
					"%w: local element default and fixed are mutually exclusive",
					ErrInvalidComponent,
				)
			}
			if element.Ref.Local != "" {
				if element.Type.Local != "" || element.Form != "" || element.Abstract ||
					element.Nillable || elementDefaultSet(*element) || elementFixedSet(*element) ||
					element.Block.String() != "" || element.Final.String() != "" ||
					element.InlineSimpleType != nil || element.InlineComplexType != nil ||
					len(element.IdentityConstraints) > 0 {
					return fmt.Errorf(
						"%w: local element reference has declaration-only properties",
						ErrInvalidComponent,
					)
				}
				if _, ok := s.elements[element.Ref]; !ok {
					return unresolvedComponent("element", element.Ref)
				}
			} else if element.Type.Local != "" && !s.typeExists(element.Type, "") {
				return unresolvedComponent("type", element.Type)
			}
			if err := s.validateAnonymousElementType(*element, typeNamespace, particles); err != nil {
				return err
			}
			if element.Ref.Local == "" {
				if err := s.validateElementValueConstraint(*element); err != nil {
					return fmt.Errorf("%w: local element: %s", ErrInvalidComponent, err)
				}
			}
		}
		if err := validateWildcard(particle.Wildcard); err != nil {
			return err
		}
		if particle.Element == nil && particle.Group == nil && particle.Wildcard == nil {
			return fmt.Errorf("%w: particle has no term", ErrInvalidComponent)
		}
		if err := s.validateModelGroup(particle.Group, typeNamespace, particles); err != nil {
			return err
		}
	}
	return nil
}

func (s *compileState) validateElementValueConstraint(element xsd.Element) error {
	lexical := element.Default
	if elementFixedSet(element) {
		lexical = element.Fixed
	}
	if !elementDefaultSet(element) && !elementFixedSet(element) {
		return nil
	}
	if element.InlineSimpleType != nil {
		if s.inlineConstraintValidContext(
			*element.InlineSimpleType,
			lexical,
			element.ValueNamespaces,
		) {
			return nil
		}
		return errors.New("value constraint is invalid for the anonymous simple type")
	}
	if element.InlineComplexType != nil {
		if element.InlineComplexType.SimpleContent &&
			element.InlineComplexType.InlineSimpleType != nil &&
			s.inlineConstraintValidContext(
				*element.InlineComplexType.InlineSimpleType,
				lexical,
				element.ValueNamespaces,
			) {
			return nil
		}
		if element.InlineComplexType.SimpleContent &&
			element.InlineComplexType.InlineSimpleType == nil &&
			s.simpleConstraintValidContext(
				element.InlineComplexType.SimpleBase,
				lexical,
				element.ValueNamespaces,
			) {
			return nil
		}
		if element.InlineComplexType.Mixed {
			return nil
		}
		return errors.New("value constraint requires simple or mixed content")
	}
	if element.Type.Local == "" || element.Type == (xsd.QName{Namespace: xsd.Namespace, Local: "anyType"}) {
		return nil
	}
	id := xsd.QName{Namespace: xsd.Namespace, Local: "ID"}
	if s.typeExists(element.Type, "simple") && s.simpleTypeDerivesFrom(element.Type, id) {
		return errors.New("ID-typed elements cannot have value constraints")
	}
	if s.simpleConstraintValidContext(element.Type, lexical, element.ValueNamespaces) {
		return nil
	}
	if complexType, ok := s.complexTypes[element.Type]; ok {
		if complexType.Mixed {
			return nil
		}
		if complexType.SimpleContent && complexType.InlineSimpleType != nil &&
			s.inlineConstraintValidContext(
				*complexType.InlineSimpleType,
				lexical,
				element.ValueNamespaces,
			) {
			return nil
		}
		if complexType.SimpleContent && complexType.InlineSimpleType == nil &&
			s.simpleConstraintValidContext(
				complexType.SimpleBase,
				lexical,
				element.ValueNamespaces,
			) {
			return nil
		}
	}
	return errors.New("value constraint is invalid for the element type")
}

func (s *compileState) validateAttributeDeclarationValueConstraint(attribute xsd.Attribute) error {
	if !attributeDeclarationDefaultSet(attribute) && !attributeDeclarationFixedSet(attribute) {
		return nil
	}
	lexical := attribute.Default
	if attributeDeclarationFixedSet(attribute) {
		lexical = attribute.Fixed
	}
	return s.validateAttributeConstraintContext(
		attribute.Type,
		attribute.InlineSimpleType,
		lexical,
		attribute.ValueNamespaces,
	)
}

func (s *compileState) validateAttributeUseValueConstraint(attribute xsd.AttributeUse) error {
	if !attributeDefaultSet(attribute) && !attributeFixedSet(attribute) {
		return nil
	}
	lexical := attribute.Default
	if attributeFixedSet(attribute) {
		lexical = attribute.Fixed
	}
	if attribute.Ref.Local != "" {
		declaration, ok := s.attributes[attribute.Ref]
		if !ok {
			return unresolvedComponent("attribute", attribute.Ref)
		}
		return s.validateAttributeConstraintContext(
			declaration.Type,
			declaration.InlineSimpleType,
			lexical,
			attribute.ValueNamespaces,
		)
	}
	return s.validateAttributeConstraintContext(
		attribute.Type,
		attribute.InlineSimpleType,
		lexical,
		attribute.ValueNamespaces,
	)
}

func (s *compileState) validateAttributeConstraint(
	typeName xsd.QName,
	inline *xsd.SimpleType,
	lexical string,
) error {
	return s.validateAttributeConstraintContext(typeName, inline, lexical, nil)
}

func (s *compileState) validateAttributeConstraintContext(
	typeName xsd.QName,
	inline *xsd.SimpleType,
	lexical string,
	namespaces map[string]string,
) error {
	id := xsd.QName{Namespace: xsd.Namespace, Local: "ID"}
	if inline != nil {
		if s.inlineSimpleTypeDerivesFrom(*inline, id) {
			return fmt.Errorf("%w: ID-typed attributes cannot have value constraints", ErrInvalidComponent)
		}
		if s.inlineConstraintValidContext(*inline, lexical, namespaces) {
			return nil
		}
		return fmt.Errorf("%w: value constraint is invalid for the anonymous simple type", ErrInvalidComponent)
	}
	if typeName.Local == "" {
		typeName = xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}
	}
	if s.simpleTypeDerivesFrom(typeName, id) {
		return fmt.Errorf("%w: ID-typed attributes cannot have value constraints", ErrInvalidComponent)
	}
	if s.simpleConstraintValidContext(typeName, lexical, namespaces) {
		return nil
	}
	return fmt.Errorf("%w: value constraint is invalid for the attribute type", ErrInvalidComponent)
}

func (s *compileState) inlineSimpleTypeDerivesFrom(
	typeDefinition xsd.SimpleType,
	base xsd.QName,
) bool {
	for depth := 0; depth <= defaultMaxDepth; depth++ {
		if typeDefinition.Variety != xsd.SimpleRestriction {
			return false
		}
		if typeDefinition.InlineBase == nil {
			return s.simpleTypeDerivesFrom(typeDefinition.Base, base)
		}
		typeDefinition = *typeDefinition.InlineBase
	}
	return false
}

func elementDefaultSet(element xsd.Element) bool {
	return element.DefaultSet || element.Default != ""
}

func elementFixedSet(element xsd.Element) bool {
	return element.FixedSet || element.Fixed != ""
}

func attributeDefaultSet(attribute xsd.AttributeUse) bool {
	return attribute.DefaultSet || attribute.Default != ""
}

func attributeFixedSet(attribute xsd.AttributeUse) bool {
	return attribute.FixedSet || attribute.Fixed != ""
}

func attributeDeclarationDefaultSet(attribute xsd.Attribute) bool {
	return attribute.DefaultSet || attribute.Default != ""
}

func attributeDeclarationFixedSet(attribute xsd.Attribute) bool {
	return attribute.FixedSet || attribute.Fixed != ""
}

func (s *compileState) simpleConstraintValid(typeName xsd.QName, lexical string) bool {
	return s.simpleConstraintValidContext(typeName, lexical, nil)
}

func (s *compileState) simpleConstraintValidContext(
	typeName xsd.QName,
	lexical string,
	namespaces map[string]string,
) bool {
	return s.simpleConstraintValidDepthContext(typeName, lexical, namespaces, 0)
}

func (s *compileState) simpleConstraintValidDepth(
	typeName xsd.QName,
	lexical string,
	depth int,
) bool {
	return s.simpleConstraintValidDepthContext(typeName, lexical, nil, depth)
}

func (s *compileState) simpleConstraintValidDepthContext(
	typeName xsd.QName,
	lexical string,
	namespaces map[string]string,
	depth int,
) bool {
	if depth > defaultMaxDepth {
		return false
	}
	if typeName.Namespace == xsd.Namespace {
		whitespace, _ := s.namedWhitespace(typeName, depth)
		lexical = normalizeConstraintWhitespace(lexical, whitespace)
		switch typeName.Local {
		case "anySimpleType":
			return true
		case "boolean":
			return lexical == "true" || lexical == "false" || lexical == "1" || lexical == "0"
		case "decimal":
			_, err := datatype.ParseDecimal(lexical)
			return err == nil
		case "integer", "nonPositiveInteger", "negativeInteger", "long", "int", "short", "byte",
			"nonNegativeInteger", "unsignedLong", "unsignedInt", "unsignedShort", "unsignedByte",
			"positiveInteger":
			value, err := datatype.ParseInteger(lexical)
			return err == nil && datatype.ValidateBuiltInInteger(typeName.Local, value) == nil
		default:
			valid := datatype.ValidateBuiltInLexical(typeName.Local, lexical) == nil
			if valid && (typeName.Local == "QName" || typeName.Local == "NOTATION") {
				_, valid = resolveConstraintQName(lexical, namespaces)
			}
			return valid
		}
	}
	typeDefinition, ok := s.simpleTypes[typeName]
	return ok && s.inlineConstraintValidDepthContext(
		typeDefinition,
		lexical,
		namespaces,
		depth+1,
	)
}

func (s *compileState) inlineConstraintValid(typeDefinition xsd.SimpleType, lexical string) bool {
	return s.inlineConstraintValidContext(typeDefinition, lexical, nil)
}

func (s *compileState) inlineConstraintValidContext(
	typeDefinition xsd.SimpleType,
	lexical string,
	namespaces map[string]string,
) bool {
	return s.inlineConstraintValidDepthContext(typeDefinition, lexical, namespaces, 0)
}

func (s *compileState) inlineConstraintValidDepth(
	typeDefinition xsd.SimpleType,
	lexical string,
	depth int,
) bool {
	return s.inlineConstraintValidDepthContext(typeDefinition, lexical, nil, depth)
}

func (s *compileState) inlineConstraintValidDepthContext(
	typeDefinition xsd.SimpleType,
	lexical string,
	namespaces map[string]string,
	depth int,
) bool {
	if depth > defaultMaxDepth {
		return false
	}
	switch typeDefinition.Variety {
	case xsd.SimpleRestriction:
		normalized := s.normalizeConstraintLexical(typeDefinition, lexical)
		valid := s.simpleConstraintValidDepthContext(
			typeDefinition.Base,
			normalized,
			namespaces,
			depth+1,
		)
		if typeDefinition.InlineBase != nil {
			valid = s.inlineConstraintValidDepthContext(
				*typeDefinition.InlineBase,
				normalized,
				namespaces,
				depth+1,
			)
		}
		if !valid {
			return false
		}
		hasPattern := false
		patternMatched := false
		for _, facet := range typeDefinition.Facets {
			if facet.Kind != xsd.FacetPattern {
				continue
			}
			hasPattern = true
			pattern, err := datatype.CompilePattern(facet.Value)
			if err != nil {
				return false
			}
			patternMatched = patternMatched || pattern.MatchString(normalized)
		}
		return (!hasPattern || patternMatched) &&
			s.restrictionConstraintFacetsValidContext(
				typeDefinition,
				normalized,
				namespaces,
				depth+1,
			)
	case xsd.SimpleList:
		items := strings.Fields(lexical)
		if len(items) == 0 {
			return false
		}
		for _, item := range items {
			valid := s.simpleConstraintValidDepthContext(
				typeDefinition.ItemType,
				item,
				namespaces,
				depth+1,
			)
			if typeDefinition.InlineItem != nil {
				valid = s.inlineConstraintValidDepthContext(
					*typeDefinition.InlineItem,
					item,
					namespaces,
					depth+1,
				)
			}
			if !valid {
				return false
			}
		}
		return true
	case xsd.SimpleUnion:
		for _, member := range typeDefinition.MemberTypes {
			if s.simpleConstraintValidDepthContext(member, lexical, namespaces, depth+1) {
				return true
			}
		}
		for _, member := range typeDefinition.InlineMembers {
			if s.inlineConstraintValidDepthContext(member, lexical, namespaces, depth+1) {
				return true
			}
		}
	}
	return false
}

type nameClass struct {
	name         *xsd.QName
	alternatives []xsd.QName
	wildcard     *xsd.Wildcard
}

func (s *compileState) validateUniqueParticleAttribution(
	group *xsd.ModelGroup,
	targetNamespace string,
) error {
	state := upaState{compile: s, follow: make(map[int]upaPositions)}
	info := state.group(group, targetNamespace)
	states := make([]upaPositions, 0, len(state.follow)+1)
	states = append(states, info.first)
	for _, positions := range state.follow {
		states = append(states, positions)
	}
	for _, positions := range states {
		values := make([]nameClass, 0, len(positions))
		for _, class := range positions {
			values = append(values, class)
		}
		for left := 0; left < len(values); left++ {
			for right := left + 1; right < len(values); right++ {
				if nameClassesOverlap(values[left], values[right], targetNamespace) {
					return fmt.Errorf(
						"%w: content model violates unique particle attribution",
						ErrInvalidComponent,
					)
				}
			}
		}
	}
	return nil
}

type upaPositions map[int]nameClass

type upaInfo struct {
	nullable bool
	first    upaPositions
	last     upaPositions
}

type upaState struct {
	compile *compileState
	next    int
	follow  map[int]upaPositions
}

func (s *upaState) group(group *xsd.ModelGroup, targetNamespace string) upaInfo {
	children := make([]upaInfo, len(group.Particles))
	for index, particle := range group.Particles {
		children[index] = s.particle(particle, targetNamespace)
	}
	var result upaInfo
	switch group.Compositor {
	case xsd.Choice:
		result = upaInfo{first: upaPositions{}, last: upaPositions{}}
		for _, child := range children {
			result.nullable = result.nullable || child.nullable
			mergeUPAPositions(result.first, child.first)
			mergeUPAPositions(result.last, child.last)
		}
	case xsd.All:
		result = upaInfo{nullable: true, first: upaPositions{}, last: upaPositions{}}
		for _, child := range children {
			result.nullable = result.nullable && child.nullable
			mergeUPAPositions(result.first, child.first)
			mergeUPAPositions(result.last, child.last)
		}
		for left, child := range children {
			for right, next := range children {
				if left != right {
					s.addFollow(child.last, next.first)
				}
			}
		}
	default:
		result = upaInfo{nullable: true, first: upaPositions{}, last: upaPositions{}}
		for _, child := range children {
			if result.nullable {
				mergeUPAPositions(result.first, child.first)
			}
			s.addFollow(result.last, child.first)
			if child.nullable {
				mergeUPAPositions(result.last, child.last)
			} else {
				result.last = cloneUPAPositions(child.last)
			}
			result.nullable = result.nullable && child.nullable
		}
	}
	if group.OccursSet {
		result = s.occurs(result, group.MinOccurs, group.MaxOccurs, group.Unbounded)
	}
	return result
}

func (s *upaState) particle(particle xsd.Particle, targetNamespace string) upaInfo {
	var result upaInfo
	if particle.Element != nil {
		name := particle.Element.Ref
		if name.Local == "" {
			name = xsd.QName{Namespace: particle.Element.Namespace, Local: particle.Element.Name}
		}
		class := nameClass{name: &name}
		if particle.Element.Ref.Local != "" {
			set := &Set{
				elements:          s.compile.elements,
				simpleTypes:       s.compile.simpleTypes,
				complexTypes:      s.compile.complexTypes,
				substitutionHeads: s.compile.substitutionHeads,
			}
			for member := range s.compile.substitutionHeads {
				if _, ok := set.SubstitutionMember(particle.Element.Ref, member); ok {
					class.alternatives = append(class.alternatives, member)
				}
			}
		}
		result = s.leaf(class)
	} else if particle.Wildcard != nil {
		result = s.leaf(nameClass{wildcard: particle.Wildcard})
	} else if particle.Group != nil {
		result = s.group(particle.Group, targetNamespace)
	} else {
		result = upaInfo{nullable: true, first: upaPositions{}, last: upaPositions{}}
	}
	return s.occurs(result, particle.MinOccurs, particle.MaxOccurs, particle.Unbounded)
}

func (s *upaState) leaf(class nameClass) upaInfo {
	position := s.next
	s.next++
	positions := upaPositions{position: class}
	return upaInfo{first: positions, last: cloneUPAPositions(positions)}
}

func (s *upaState) occurs(info upaInfo, minimum uint64, maximum uint64, unbounded bool) upaInfo {
	if unbounded || maximum > 1 {
		s.addFollow(info.last, info.first)
	}
	if minimum == 0 {
		info.nullable = true
	}
	return info
}

func (s *upaState) addFollow(from upaPositions, to upaPositions) {
	for position := range from {
		if s.follow[position] == nil {
			s.follow[position] = upaPositions{}
		}
		mergeUPAPositions(s.follow[position], to)
	}
}

func cloneUPAPositions(source upaPositions) upaPositions {
	clone := make(upaPositions, len(source))
	mergeUPAPositions(clone, source)
	return clone
}

func mergeUPAPositions(target upaPositions, source upaPositions) {
	for position, class := range source {
		target[position] = class
	}
}

func particleNullable(particle xsd.Particle) bool {
	if particle.MinOccurs == 0 {
		return true
	}
	if particle.Group == nil {
		return false
	}
	switch particle.Group.Compositor {
	case xsd.Choice:
		for _, child := range particle.Group.Particles {
			if particleNullable(child) {
				return true
			}
		}
		return false
	case xsd.Sequence, xsd.All:
		for _, child := range particle.Group.Particles {
			if !particleNullable(child) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func nameClassesOverlap(left nameClass, right nameClass, targetNamespace string) bool {
	if left.name != nil && right.name != nil {
		for _, leftName := range nameClassNames(left) {
			for _, rightName := range nameClassNames(right) {
				if leftName == rightName {
					return true
				}
			}
		}
		return false
	}
	if left.name != nil {
		for _, name := range nameClassNames(left) {
			if wildcardAllows(right.wildcard, name.Namespace, targetNamespace) {
				return true
			}
		}
		return false
	}
	if right.name != nil {
		for _, name := range nameClassNames(right) {
			if wildcardAllows(left.wildcard, name.Namespace, targetNamespace) {
				return true
			}
		}
		return false
	}
	candidates := []string{"", targetNamespace, "urn:xsd-wildcard-probe"}
	for _, wildcard := range []*xsd.Wildcard{left.wildcard, right.wildcard} {
		for _, namespace := range wildcard.Namespaces {
			if !strings.HasPrefix(namespace, "##") {
				candidates = append(candidates, namespace)
			}
		}
	}
	for _, namespace := range candidates {
		if wildcardAllows(left.wildcard, namespace, targetNamespace) &&
			wildcardAllows(right.wildcard, namespace, targetNamespace) {
			return true
		}
	}
	return false
}

func nameClassNames(class nameClass) []xsd.QName {
	return append([]xsd.QName{*class.name}, class.alternatives...)
}

func wildcardAllows(wildcard *xsd.Wildcard, namespace string, targetNamespace string) bool {
	if wildcard == nil {
		return false
	}
	for _, constraint := range wildcard.Namespaces {
		switch constraint {
		case "##any":
			return true
		case "##other":
			if namespace != "" && namespace != targetNamespace {
				return true
			}
		case "##local":
			if namespace == "" {
				return true
			}
		case "##targetNamespace":
			if namespace == targetNamespace {
				return true
			}
		default:
			if namespace == constraint {
				return true
			}
		}
	}
	return false
}

func validateWildcard(wildcard *xsd.Wildcard) error {
	if wildcard == nil {
		return nil
	}
	if wildcard.ProcessContents != xsd.ProcessStrict &&
		wildcard.ProcessContents != xsd.ProcessLax &&
		wildcard.ProcessContents != xsd.ProcessSkip {
		return fmt.Errorf(
			"%w: wildcard has invalid processContents %q",
			ErrInvalidComponent,
			wildcard.ProcessContents,
		)
	}
	if len(wildcard.Namespaces) == 0 {
		return fmt.Errorf("%w: wildcard has no namespace constraint", ErrInvalidComponent)
	}
	seen := make(map[string]struct{}, len(wildcard.Namespaces))
	for _, namespace := range wildcard.Namespaces {
		if strings.HasPrefix(namespace, "##") && namespace != "##any" &&
			namespace != "##other" && namespace != "##local" &&
			namespace != "##targetNamespace" {
			return fmt.Errorf(
				"%w: wildcard has invalid namespace token %q",
				ErrInvalidComponent,
				namespace,
			)
		}
		if _, duplicate := seen[namespace]; duplicate {
			return fmt.Errorf("%w: wildcard namespace %q is duplicated", ErrInvalidComponent, namespace)
		}
		seen[namespace] = struct{}{}
	}
	if len(wildcard.Namespaces) > 1 {
		if _, any := seen["##any"]; any {
			return fmt.Errorf("%w: ##any must be the only wildcard namespace", ErrInvalidComponent)
		}
		if _, other := seen["##other"]; other {
			return fmt.Errorf("%w: ##other must be the only wildcard namespace", ErrInvalidComponent)
		}
	}
	return nil
}

func (s *compileState) validateAttributeUse(
	attribute xsd.AttributeUse,
	typeNamespace string,
) error {
	_ = typeNamespace
	if (attribute.Name == "") == (attribute.Ref.Local == "") {
		return fmt.Errorf(
			"%w: attribute use must have exactly one of name or ref",
			ErrInvalidComponent,
		)
	}
	if attribute.Ref.Local != "" &&
		(attribute.Type.Local != "" || attribute.InlineSimpleType != nil) {
		return fmt.Errorf(
			"%w: referenced attribute use cannot define a type",
			ErrInvalidComponent,
		)
	}
	if attributeDefaultSet(attribute) && attributeFixedSet(attribute) {
		return fmt.Errorf(
			"%w: attribute use default and fixed are mutually exclusive",
			ErrInvalidComponent,
		)
	}
	if attributeDefaultSet(attribute) && attribute.Use != xsd.AttributeOptional {
		return fmt.Errorf(
			"%w: attribute default requires optional use",
			ErrInvalidComponent,
		)
	}
	if attribute.Ref.Local != "" {
		if _, ok := s.attributes[attribute.Ref]; !ok {
			return unresolvedComponent("attribute", attribute.Ref)
		}
		return s.validateAttributeUseValueConstraint(attribute)
	}
	if attribute.Type.Local != "" && attribute.InlineSimpleType != nil {
		return fmt.Errorf(
			"%w: attribute use has more than one type definition",
			ErrInvalidComponent,
		)
	}
	if attribute.InlineSimpleType != nil {
		if err := s.validateSimpleTypeDefinition(*attribute.InlineSimpleType); err != nil {
			return err
		}
	}
	if attribute.Type.Local != "" && !s.typeExists(attribute.Type, "simple") {
		return unresolvedComponent("simple type", attribute.Type)
	}
	if directNotationType(attribute.Type) {
		return fmt.Errorf("%w: NOTATION cannot be used directly", ErrInvalidComponent)
	}
	return s.validateAttributeUseValueConstraint(attribute)
}

func directNotationType(name xsd.QName) bool {
	return name.Namespace == xsd.Namespace && name.Local == "NOTATION"
}

func (s *compileState) typeExists(name xsd.QName, requiredKind string) bool {
	kind := ""
	if name.Namespace == xsd.Namespace {
		kind = builtInTypes[name.Local]
	} else if stored, ok := s.typeKinds[name]; ok {
		if strings.HasPrefix(stored, "simple") {
			kind = "simple"
		} else {
			kind = "complex"
		}
	}
	return kind != "" && (requiredKind == "" || kind == requiredKind)
}

func invalidComponent(kind string, name xsd.QName, reason string) error {
	return fmt.Errorf(
		"%w: %s {%s}%s: %s",
		ErrInvalidComponent,
		kind,
		name.Namespace,
		name.Local,
		reason,
	)
}

func unresolvedComponent(kind string, name xsd.QName) error {
	return fmt.Errorf(
		"%w: %s {%s}%s",
		ErrUnresolvedComponent,
		kind,
		name.Namespace,
		name.Local,
	)
}

type resourceDocument struct {
	document *xsd.Document
}

type instanceKey struct {
	uri       string
	namespace string
}

type compileState struct {
	compiler          *Compiler
	resources         map[string]resourceDocument
	instances         map[instanceKey]*Document
	bytes             int64
	references        int
	components        int
	elements          map[xsd.QName]xsd.Element
	attributes        map[xsd.QName]xsd.Attribute
	simpleTypes       map[xsd.QName]xsd.SimpleType
	complexTypes      map[xsd.QName]xsd.ComplexType
	modelGroups       map[xsd.QName]xsd.ModelGroupDefinition
	attributeGroups   map[xsd.QName]xsd.AttributeGroup
	notations         map[xsd.QName]xsd.Notation
	substitutionHeads map[xsd.QName]xsd.QName
	typeKinds         map[xsd.QName]string
}

func (s *compileState) compileDocument(
	ctx context.Context,
	identity string,
	effectiveNamespace string,
	depth int,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > s.compiler.limits.MaxDepth {
		return fmt.Errorf("%w: schema depth exceeds %d", ErrLimitExceeded, s.compiler.limits.MaxDepth)
	}
	resource := s.resources[identity]
	namespace := resource.document.TargetNamespace
	chameleon := namespace == "" && effectiveNamespace != ""
	if chameleon {
		namespace = effectiveNamespace
	}
	key := instanceKey{uri: identity, namespace: namespace}
	if _, ok := s.instances[key]; ok {
		return nil
	}
	if len(s.instances) >= s.compiler.limits.MaxSchemas {
		return fmt.Errorf("%w: schema count exceeds %d", ErrLimitExceeded, s.compiler.limits.MaxSchemas)
	}
	compiled := &Document{URI: identity, Namespace: namespace, Chameleon: chameleon}
	s.instances[key] = compiled
	if err := s.indexComponents(resource.document, namespace, chameleon); err != nil {
		return err
	}

	for _, reference := range resource.document.References {
		if reference.URI == "" && reference.Kind != xsd.ReferenceImport {
			continue
		}
		s.references++
		if s.references > s.compiler.limits.MaxReferences {
			return fmt.Errorf(
				"%w: reference count exceeds %d",
				ErrLimitExceeded,
				s.compiler.limits.MaxReferences,
			)
		}
		referenced, resolvedIdentity, err := s.load(ctx, reference)
		if err != nil {
			if reference.URI == "" &&
				(errors.Is(err, resolve.ErrNotFound) || errors.Is(err, resolve.ErrAccessDenied)) {
				continue
			}
			return err
		}
		resolvedReference := reference
		resolvedReference.URI = resolvedIdentity
		compiled.Dependencies = append(compiled.Dependencies, resolvedIdentity)
		childNamespace, err := requiredNamespace(
			resolvedReference,
			namespace,
			referenced.TargetNamespace,
		)
		if err != nil {
			return err
		}
		if err := s.compileDocument(ctx, resolvedIdentity, childNamespace, depth+1); err != nil {
			return err
		}
		if reference.Kind == xsd.ReferenceRedefine {
			for _, redefinition := range resource.document.Redefinitions {
				if redefinition.Reference.URI == reference.URI {
					if err := s.applyRedefinition(
						redefinition,
						resource.document,
						namespace,
						chameleon,
					); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (s *compileState) applyRedefinition(
	redefinition xsd.Redefinition,
	document *xsd.Document,
	namespace string,
	chameleon bool,
) error {
	for _, definition := range redefinition.SimpleTypes {
		name := xsd.QName{Namespace: namespace, Local: definition.Name}
		original, ok := s.simpleTypes[name]
		if !ok {
			return unresolvedComponent("redefined simple type", name)
		}
		definition = normalizeInlineSimpleType(definition, namespace, chameleon)
		if definition.Variety != xsd.SimpleRestriction || definition.Base != name {
			return invalidComponent("redefined simple type", name, "must restrict itself")
		}
		definition.Base = original.Base
		definition.InlineBase = original.InlineBase
		definition.Facets = append(append([]xsd.Facet(nil), original.Facets...), definition.Facets...)
		s.simpleTypes[name] = definition
	}
	for _, definition := range redefinition.ModelGroups {
		name := xsd.QName{Namespace: namespace, Local: definition.Name}
		original, ok := s.modelGroups[name]
		if !ok {
			return unresolvedComponent("redefined model group", name)
		}
		definition.Content = cloneModelGroup(definition.Content)
		normalizeModelGroup(
			definition.Content,
			document.ElementFormDefault,
			document.AttributeFormDefault,
			namespace,
			chameleon,
		)
		replaceRedefinedGroupRefs(definition.Content, name, original.Content)
		s.modelGroups[name] = definition
	}
	for _, definition := range redefinition.AttributeGroups {
		name := xsd.QName{Namespace: namespace, Local: definition.Name}
		original, ok := s.attributeGroups[name]
		if !ok {
			return unresolvedComponent("redefined attribute group", name)
		}
		definition = normalizeAttributeGroup(
			definition,
			document.AttributeFormDefault,
			namespace,
			chameleon,
		)
		foundSelf := false
		refs := make([]xsd.QName, 0, len(definition.References))
		for _, reference := range definition.References {
			if reference == name {
				foundSelf = true
				definition.Attributes = append(
					append([]xsd.AttributeUse(nil), original.Attributes...),
					definition.Attributes...,
				)
				if definition.Wildcard == nil {
					definition.Wildcard = cloneWildcard(original.Wildcard)
				}
				continue
			}
			refs = append(refs, reference)
		}
		if !foundSelf {
			return invalidComponent("redefined attribute group", name, "must reference itself")
		}
		definition.References = refs
		s.attributeGroups[name] = definition
	}
	for _, definition := range redefinition.ComplexTypes {
		name := xsd.QName{Namespace: namespace, Local: definition.Name}
		original, ok := s.complexTypes[name]
		if !ok {
			return unresolvedComponent("redefined complex type", name)
		}
		definition = normalizeComplexType(
			definition,
			document.ElementFormDefault,
			document.AttributeFormDefault,
			namespace,
			chameleon,
		)
		if definition.Base != name ||
			(definition.Derivation != xsd.DerivationExtension &&
				definition.Derivation != xsd.DerivationRestriction) {
			return invalidComponent("redefined complex type", name, "must derive from itself")
		}
		if definition.Derivation == xsd.DerivationExtension {
			definition.Content = extendContent(original.Content, definition.Content)
			definition.Attributes = append(
				append([]xsd.AttributeUse(nil), original.Attributes...),
				definition.Attributes...,
			)
			definition.AttributeGroupRefs = append(
				append([]xsd.QName(nil), original.AttributeGroupRefs...),
				definition.AttributeGroupRefs...,
			)
			if definition.AttributeWildcard == nil {
				definition.AttributeWildcard = cloneWildcard(original.AttributeWildcard)
			}
		} else if err := s.validateComplexRestriction(definition, original); err != nil {
			return invalidComponent("redefined complex type", name, err.Error())
		}
		definition.Base = original.Base
		definition.Derivation = original.Derivation
		s.complexTypes[name] = definition
	}
	return nil
}

func replaceRedefinedGroupRefs(
	group *xsd.ModelGroup,
	name xsd.QName,
	original *xsd.ModelGroup,
) {
	if group == nil {
		return
	}
	for index := range group.Particles {
		particle := &group.Particles[index]
		if particle.GroupRef == name {
			particle.GroupRef = xsd.QName{}
			particle.Group = cloneModelGroup(original)
			continue
		}
		replaceRedefinedGroupRefs(particle.Group, name, original)
	}
}

func (s *compileState) indexComponents(
	document *xsd.Document,
	namespace string,
	chameleon bool,
) error {
	for _, notation := range document.Notations {
		name, err := s.componentName(namespace, notation.Name, "notation")
		if err != nil {
			return err
		}
		if _, exists := s.notations[name]; exists {
			return duplicateComponent("notation", name)
		}
		notation.Annotation = cloneAnnotation(notation.Annotation)
		s.notations[name] = notation
	}
	for _, element := range document.Elements {
		element = cloneElement(element)
		if element.Block.String() == "" {
			element.Block = document.BlockDefault
		}
		if element.Final.String() == "" {
			element.Final = document.FinalDefault
		}
		element = normalizeElementTypes(
			element,
			document.ElementFormDefault,
			document.AttributeFormDefault,
			namespace,
			chameleon,
		)
		name, err := s.componentName(namespace, element.Name, "element")
		if err != nil {
			return err
		}
		if _, exists := s.elements[name]; exists {
			return duplicateComponent("element", name)
		}
		if chameleon {
			element.Type = adoptNamespace(element.Type, namespace)
			element.Ref = adoptNamespace(element.Ref, namespace)
			element.SubstitutionGroup = adoptNamespace(
				element.SubstitutionGroup,
				namespace,
			)
			for index := range element.IdentityConstraints {
				element.IdentityConstraints[index].Refer = adoptNamespace(
					element.IdentityConstraints[index].Refer,
					namespace,
				)
			}
		}
		s.elements[name] = element
	}
	for _, attribute := range document.Attributes {
		attribute = cloneAttribute(attribute)
		name, err := s.componentName(namespace, attribute.Name, "attribute")
		if err != nil {
			return err
		}
		if _, exists := s.attributes[name]; exists {
			return duplicateComponent("attribute", name)
		}
		if chameleon {
			attribute.Type = adoptNamespace(attribute.Type, namespace)
		}
		if attribute.InlineSimpleType != nil {
			typeDefinition := normalizeInlineSimpleType(
				*attribute.InlineSimpleType,
				namespace,
				chameleon,
			)
			attribute.InlineSimpleType = &typeDefinition
		}
		s.attributes[name] = attribute
	}
	for _, simpleType := range document.SimpleTypes {
		simpleType = cloneSimpleType(simpleType)
		name, err := s.componentName(namespace, simpleType.Name, "type")
		if err != nil {
			return err
		}
		if kind, exists := s.typeKinds[name]; exists {
			return duplicateComponent(kind+" and simple type", name)
		}
		if chameleon {
			simpleType.Base = adoptNamespace(simpleType.Base, namespace)
			simpleType.ItemType = adoptNamespace(simpleType.ItemType, namespace)
			for index := range simpleType.MemberTypes {
				simpleType.MemberTypes[index] = adoptNamespace(
					simpleType.MemberTypes[index],
					namespace,
				)
			}
		}
		s.typeKinds[name] = "simple type"
		s.simpleTypes[name] = cloneSimpleType(simpleType)
	}
	for _, complexType := range document.ComplexTypes {
		name, err := s.componentName(namespace, complexType.Name, "type")
		if err != nil {
			return err
		}
		if kind, exists := s.typeKinds[name]; exists {
			return duplicateComponent(kind+" and complex type", name)
		}
		if complexType.Block.String() == "" {
			complexType.Block = document.BlockDefault
		}
		if complexType.Final.String() == "" {
			complexType.Final = document.FinalDefault
		}
		complexType = normalizeComplexType(
			complexType,
			document.ElementFormDefault,
			document.AttributeFormDefault,
			namespace,
			chameleon,
		)
		s.typeKinds[name] = "complex type"
		s.complexTypes[name] = complexType
	}
	for _, group := range document.ModelGroups {
		name, err := s.componentName(namespace, group.Name, "model group")
		if err != nil {
			return err
		}
		if _, exists := s.modelGroups[name]; exists {
			return duplicateComponent("model group", name)
		}
		group.Content = cloneModelGroup(group.Content)
		normalizeModelGroup(
			group.Content,
			document.ElementFormDefault,
			document.AttributeFormDefault,
			namespace,
			chameleon,
		)
		s.modelGroups[name] = group
	}
	for _, group := range document.AttributeGroups {
		name, err := s.componentName(namespace, group.Name, "attribute group")
		if err != nil {
			return err
		}
		if _, exists := s.attributeGroups[name]; exists {
			return duplicateComponent("attribute group", name)
		}
		group = normalizeAttributeGroup(
			group,
			document.AttributeFormDefault,
			namespace,
			chameleon,
		)
		s.attributeGroups[name] = group
	}
	return nil
}

func normalizeComplexType(
	complexType xsd.ComplexType,
	elementDefault xsd.Form,
	attributeDefault xsd.Form,
	namespace string,
	chameleon bool,
) xsd.ComplexType {
	complexType = cloneComplexType(complexType)
	if complexType.InlineSimpleType != nil {
		typeDefinition := normalizeInlineSimpleType(
			*complexType.InlineSimpleType,
			namespace,
			chameleon,
		)
		complexType.InlineSimpleType = &typeDefinition
	}
	normalizeModelGroup(
		complexType.Content,
		elementDefault,
		attributeDefault,
		namespace,
		chameleon,
	)
	complexType.Attributes = normalizeAttributeUses(
		complexType.Attributes,
		attributeDefault,
		namespace,
		chameleon,
	)
	if chameleon {
		for index := range complexType.AttributeGroupRefs {
			complexType.AttributeGroupRefs[index] = adoptNamespace(
				complexType.AttributeGroupRefs[index],
				namespace,
			)
		}
		complexType.Base = adoptNamespace(complexType.Base, namespace)
	}
	return complexType
}

func normalizeElementTypes(
	element xsd.Element,
	elementDefault xsd.Form,
	attributeDefault xsd.Form,
	namespace string,
	chameleon bool,
) xsd.Element {
	element.TargetNamespace = namespace
	for index := range element.IdentityConstraints {
		element.IdentityConstraints[index].TargetNamespace = namespace
		if chameleon {
			element.IdentityConstraints[index].Refer = adoptNamespace(
				element.IdentityConstraints[index].Refer,
				namespace,
			)
		}
	}
	if element.InlineSimpleType != nil {
		typeDefinition := normalizeInlineSimpleType(
			*element.InlineSimpleType,
			namespace,
			chameleon,
		)
		element.InlineSimpleType = &typeDefinition
	}
	if element.InlineComplexType != nil {
		typeDefinition := normalizeComplexType(
			*element.InlineComplexType,
			elementDefault,
			attributeDefault,
			namespace,
			chameleon,
		)
		element.InlineComplexType = &typeDefinition
	}
	return element
}

func normalizeInlineSimpleType(
	typeDefinition xsd.SimpleType,
	namespace string,
	chameleon bool,
) xsd.SimpleType {
	typeDefinition = cloneSimpleType(typeDefinition)
	if typeDefinition.InlineBase != nil {
		base := normalizeInlineSimpleType(*typeDefinition.InlineBase, namespace, chameleon)
		typeDefinition.InlineBase = &base
	}
	if typeDefinition.InlineItem != nil {
		item := normalizeInlineSimpleType(*typeDefinition.InlineItem, namespace, chameleon)
		typeDefinition.InlineItem = &item
	}
	for index := range typeDefinition.InlineMembers {
		typeDefinition.InlineMembers[index] = normalizeInlineSimpleType(
			typeDefinition.InlineMembers[index], namespace, chameleon,
		)
	}
	if chameleon {
		typeDefinition.Base = adoptNamespace(typeDefinition.Base, namespace)
		typeDefinition.ItemType = adoptNamespace(typeDefinition.ItemType, namespace)
		for index := range typeDefinition.MemberTypes {
			typeDefinition.MemberTypes[index] = adoptNamespace(
				typeDefinition.MemberTypes[index],
				namespace,
			)
		}
	}
	return typeDefinition
}

func normalizeAttributeUses(
	attributes []xsd.AttributeUse,
	attributeDefault xsd.Form,
	namespace string,
	chameleon bool,
) []xsd.AttributeUse {
	attributes = cloneAttributeUses(attributes)
	for index := range attributes {
		attribute := &attributes[index]
		if attribute.Form == "" {
			attribute.Form = attributeDefault
		}
		if attribute.Form == xsd.FormQualified {
			attribute.Namespace = namespace
		} else {
			attribute.Namespace = ""
		}
		if chameleon {
			attribute.Ref = adoptNamespace(attribute.Ref, namespace)
			attribute.Type = adoptNamespace(attribute.Type, namespace)
		}
		if attribute.InlineSimpleType != nil {
			typeDefinition := normalizeInlineSimpleType(
				*attribute.InlineSimpleType,
				namespace,
				chameleon,
			)
			attribute.InlineSimpleType = &typeDefinition
		}
	}
	return attributes
}

func normalizeAttributeGroup(
	group xsd.AttributeGroup,
	attributeDefault xsd.Form,
	namespace string,
	chameleon bool,
) xsd.AttributeGroup {
	group = cloneAttributeGroup(group)
	group.Attributes = normalizeAttributeUses(
		group.Attributes,
		attributeDefault,
		namespace,
		chameleon,
	)
	if chameleon {
		for index := range group.References {
			group.References[index] = adoptNamespace(group.References[index], namespace)
		}
	}
	return group
}

func normalizeModelGroup(
	group *xsd.ModelGroup,
	elementDefault xsd.Form,
	attributeDefault xsd.Form,
	namespace string,
	chameleon bool,
) {
	if group == nil {
		return
	}
	for index := range group.Particles {
		particle := &group.Particles[index]
		if particle.Element != nil {
			if particle.Element.Ref.Local != "" {
				particle.Element.Namespace = ""
			} else {
				if particle.Element.Form == "" {
					particle.Element.Form = elementDefault
				}
				if particle.Element.Form == xsd.FormQualified {
					particle.Element.Namespace = namespace
				} else {
					particle.Element.Namespace = ""
				}
			}
			if chameleon {
				particle.Element.Ref = adoptNamespace(particle.Element.Ref, namespace)
				particle.Element.Type = adoptNamespace(particle.Element.Type, namespace)
				for index := range particle.Element.IdentityConstraints {
					constraint := &particle.Element.IdentityConstraints[index]
					constraint.Refer = adoptNamespace(constraint.Refer, namespace)
				}
			}
			*particle.Element = normalizeElementTypes(
				*particle.Element,
				elementDefault,
				attributeDefault,
				namespace,
				chameleon,
			)
		}
		if chameleon {
			particle.GroupRef = adoptNamespace(particle.GroupRef, namespace)
		}
		normalizeModelGroup(
			particle.Group,
			elementDefault,
			attributeDefault,
			namespace,
			chameleon,
		)
	}
}

func (s *compileState) componentName(
	namespace string,
	local string,
	kind string,
) (xsd.QName, error) {
	if local == "" {
		return xsd.QName{}, fmt.Errorf("%w: global %s has no name", ErrInvalidComponent, kind)
	}
	s.components++
	if s.components > s.compiler.limits.MaxComponents {
		return xsd.QName{}, fmt.Errorf(
			"%w: component count exceeds %d",
			ErrLimitExceeded,
			s.compiler.limits.MaxComponents,
		)
	}
	return xsd.QName{Namespace: namespace, Local: local}, nil
}

func duplicateComponent(kind string, name xsd.QName) error {
	return fmt.Errorf(
		"%w: %s {%s}%s",
		ErrDuplicateComponent,
		kind,
		name.Namespace,
		name.Local,
	)
}

func adoptNamespace(name xsd.QName, namespace string) xsd.QName {
	if name.Local != "" && name.Namespace == "" {
		name.Namespace = namespace
	}
	return name
}

func (s *compileState) load(
	ctx context.Context,
	reference xsd.SchemaReference,
) (*xsd.Document, string, error) {
	if cached, ok := s.resources[reference.URI]; ok {
		return cached.document, reference.URI, nil
	}
	resource, err := s.compiler.resolver.Resolve(ctx, resolve.Request{
		URI:       reference.URI,
		Namespace: reference.Namespace,
		Kind:      resolveKind(reference.Kind),
	})
	if err != nil {
		return nil, "", err
	}
	if reference.URI != "" && resource.URI != reference.URI {
		return nil, "", fmt.Errorf(
			"%w: requested %q, received %q",
			ErrResourceIdentity,
			reference.URI,
			resource.URI,
		)
	}
	if reference.URI == "" {
		if err := validateIdentity(resource.URI); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrResourceIdentity, err)
		}
		if cached, ok := s.resources[resource.URI]; ok {
			return cached.document, resource.URI, nil
		}
	}
	if s.bytes+int64(len(resource.Content)) > s.compiler.limits.MaxBytes {
		return nil, "", fmt.Errorf("%w: schema bytes exceed %d", ErrLimitExceeded, s.compiler.limits.MaxBytes)
	}
	s.bytes += int64(len(resource.Content))
	document, err := xsd.Parse(ctx, resource.Content, xsd.ParseOptions{
		SystemID:         resource.URI,
		MaxDocumentBytes: s.compiler.limits.MaxBytes,
	})
	if err != nil {
		return nil, "", err
	}
	s.resources[resource.URI] = resourceDocument{document: document}
	return document, resource.URI, nil
}

func requiredNamespace(
	reference xsd.SchemaReference,
	parentNamespace string,
	actualNamespace string,
) (string, error) {
	switch reference.Kind {
	case xsd.ReferenceInclude, xsd.ReferenceRedefine:
		if actualNamespace != "" && actualNamespace != parentNamespace {
			return "", fmt.Errorf(
				"%w: %s has %q, expected %q",
				ErrNamespace,
				reference.URI,
				actualNamespace,
				parentNamespace,
			)
		}
		return parentNamespace, nil
	case xsd.ReferenceImport:
		if actualNamespace != reference.Namespace {
			return "", fmt.Errorf(
				"%w: %s has %q, import requested %q",
				ErrNamespace,
				reference.URI,
				actualNamespace,
				reference.Namespace,
			)
		}
		return actualNamespace, nil
	default:
		return "", fmt.Errorf("xsd compile: unknown reference kind %q", reference.Kind)
	}
}

func resolveKind(kind xsd.ReferenceKind) resolve.Kind {
	switch kind {
	case xsd.ReferenceInclude:
		return resolve.KindInclude
	case xsd.ReferenceImport:
		return resolve.KindImport
	case xsd.ReferenceRedefine:
		return resolve.KindRedefine
	default:
		return ""
	}
}

func validateIdentity(identity string) error {
	uri, err := url.Parse(identity)
	if err != nil || !uri.IsAbs() || uri.Fragment != "" {
		return fmt.Errorf("xsd compile: invalid resource URI %q", identity)
	}
	return nil
}
