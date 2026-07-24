// Package builder provides checked helpers for constructing schema documents.
package builder

import (
	"context"
	"fmt"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

const validationURI = "urn:xsd:builder"

// Schema accumulates declarations and rejects duplicate or malformed names.
// Build performs complete schema compilation before returning a document.
type Schema struct {
	document *xsd.Document
	names    map[string]string
}

// New creates an empty schema with qualified local elements.
func New(targetNamespace string) *Schema {
	return &Schema{
		document: &xsd.Document{
			TargetNamespace:    targetNamespace,
			ElementFormDefault: xsd.FormQualified,
			Namespaces:         map[string]string{"xs": xsd.Namespace},
		},
		names: make(map[string]string),
	}
}

// SetFormDefaults controls local element and attribute qualification.
func (s *Schema) SetFormDefaults(elements xsd.Form, attributes xsd.Form) error {
	if !validForm(elements) || !validForm(attributes) {
		return fmt.Errorf("xsd builder: invalid form defaults %q and %q", elements, attributes)
	}
	s.document.ElementFormDefault = elements
	s.document.AttributeFormDefault = attributes
	return nil
}

// AddSimpleRestriction adds a named restriction of an existing simple type.
func (s *Schema) AddSimpleRestriction(
	name string,
	base xsd.QName,
	facets ...xsd.Facet,
) error {
	if err := s.reserve("type", name); err != nil {
		return err
	}
	if base.Local == "" {
		delete(s.names, "type\x00"+name)
		return fmt.Errorf("xsd builder: restriction %q has no base", name)
	}
	s.document.SimpleTypes = append(s.document.SimpleTypes, xsd.SimpleType{
		Name:    name,
		Variety: xsd.SimpleRestriction,
		Base:    base,
		Facets:  append([]xsd.Facet(nil), facets...),
	})
	return nil
}

// AddComplexType adds a named complex type with copied content and attributes.
func (s *Schema) AddComplexType(
	name string,
	content *xsd.ModelGroup,
	attributes ...xsd.AttributeUse,
) error {
	if err := s.reserve("type", name); err != nil {
		return err
	}
	s.document.ComplexTypes = append(s.document.ComplexTypes, xsd.ComplexType{
		Name:       name,
		Content:    cloneModelGroup(content),
		Attributes: cloneAttributeUses(attributes),
	})
	return nil
}

// AddElement adds a named global element with a named type.
func (s *Schema) AddElement(name string, typeName xsd.QName) error {
	if err := s.reserve("element", name); err != nil {
		return err
	}
	if typeName.Local == "" {
		delete(s.names, "element\x00"+name)
		return fmt.Errorf("xsd builder: element %q has no type", name)
	}
	s.document.Elements = append(s.document.Elements, xsd.Element{
		Name: name,
		Type: typeName,
	})
	return nil
}

// AddAttribute adds a named global attribute with a named simple type.
func (s *Schema) AddAttribute(name string, typeName xsd.QName) error {
	if err := s.reserve("attribute", name); err != nil {
		return err
	}
	if typeName.Local == "" {
		delete(s.names, "attribute\x00"+name)
		return fmt.Errorf("xsd builder: attribute %q has no type", name)
	}
	s.document.Attributes = append(s.document.Attributes, xsd.Attribute{
		Name: name,
		Type: typeName,
	})
	return nil
}

// Build compiles the generated schema and returns an isolated parsed document.
func (s *Schema) Build(ctx context.Context) (*xsd.Document, error) {
	encoded, err := xsd.Marshal(s.document)
	if err != nil {
		return nil, err
	}
	compiler, _ := compile.New(compile.Options{})
	if _, err := compiler.Compile(ctx, compile.Source{
		URI: validationURI, Content: encoded,
	}); err != nil {
		return nil, fmt.Errorf("xsd builder: schema is invalid: %w", err)
	}
	return xsd.Parse(ctx, encoded, xsd.ParseOptions{SystemID: validationURI})
}

func (s *Schema) reserve(kind string, name string) error {
	if datatype.ValidateBuiltInLexical("NCName", name) != nil {
		return fmt.Errorf("xsd builder: %s name %q is not an NCName", kind, name)
	}
	key := kind + "\x00" + name
	if previous, duplicate := s.names[key]; duplicate {
		return fmt.Errorf("xsd builder: duplicate %s %q after %s", kind, name, previous)
	}
	s.names[key] = kind
	return nil
}

func validForm(form xsd.Form) bool {
	return form == "" || form == xsd.FormQualified || form == xsd.FormUnqualified
}

func cloneModelGroup(group *xsd.ModelGroup) *xsd.ModelGroup {
	if group == nil {
		return nil
	}
	clone := *group
	clone.Annotation = cloneAnnotation(group.Annotation)
	clone.Particles = make([]xsd.Particle, len(group.Particles))
	for index, particle := range group.Particles {
		clone.Particles[index] = particle
		clone.Particles[index].Annotation = cloneAnnotation(particle.Annotation)
		if particle.Element != nil {
			element := cloneElement(*particle.Element)
			clone.Particles[index].Element = &element
		}
		clone.Particles[index].Group = cloneModelGroup(particle.Group)
		if particle.Wildcard != nil {
			wildcard := *particle.Wildcard
			wildcard.Namespaces = append([]string(nil), particle.Wildcard.Namespaces...)
			wildcard.Annotation = cloneAnnotation(particle.Wildcard.Annotation)
			clone.Particles[index].Wildcard = &wildcard
		}
	}
	return &clone
}

func cloneElement(element xsd.Element) xsd.Element {
	element.Annotation = cloneAnnotation(element.Annotation)
	element.ValueNamespaces = cloneStringMap(element.ValueNamespaces)
	constraints := make([]xsd.IdentityConstraint, len(element.IdentityConstraints))
	for index, constraint := range element.IdentityConstraints {
		constraint.Fields = append([]string(nil), constraint.Fields...)
		constraint.FieldIDs = append([]string(nil), constraint.FieldIDs...)
		constraint.Namespaces = make(map[string]string, len(constraint.Namespaces))
		for prefix, namespace := range element.IdentityConstraints[index].Namespaces {
			constraint.Namespaces[prefix] = namespace
		}
		constraint.Annotation = cloneAnnotation(constraint.Annotation)
		constraint.SelectorAnnotation = cloneAnnotation(constraint.SelectorAnnotation)
		fieldAnnotations := constraint.FieldAnnotations
		constraint.FieldAnnotations = make(
			[]*xsd.Annotation,
			len(fieldAnnotations),
		)
		for fieldIndex, annotation := range fieldAnnotations {
			constraint.FieldAnnotations[fieldIndex] = cloneAnnotation(annotation)
		}
		constraints[index] = constraint
	}
	element.IdentityConstraints = constraints
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

func cloneStringMap(values map[string]string) map[string]string {
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func cloneSimpleType(typeDefinition xsd.SimpleType) xsd.SimpleType {
	return cloneSimpleTypeWithSeen(
		typeDefinition,
		make(map[*xsd.SimpleType]*xsd.SimpleType),
	)
}

func cloneSimpleTypeWithSeen(
	typeDefinition xsd.SimpleType,
	seen map[*xsd.SimpleType]*xsd.SimpleType,
) xsd.SimpleType {
	typeDefinition.Annotation = cloneAnnotation(typeDefinition.Annotation)
	typeDefinition.VarietyAnnotation = cloneAnnotation(typeDefinition.VarietyAnnotation)
	typeDefinition.Facets = append([]xsd.Facet(nil), typeDefinition.Facets...)
	for index := range typeDefinition.Facets {
		typeDefinition.Facets[index].Namespaces = cloneStringMap(
			typeDefinition.Facets[index].Namespaces,
		)
		typeDefinition.Facets[index].Annotation = cloneAnnotation(
			typeDefinition.Facets[index].Annotation,
		)
	}
	typeDefinition.MemberTypes = append([]xsd.QName(nil), typeDefinition.MemberTypes...)
	typeDefinition.InlineBase = cloneSimpleTypePointer(typeDefinition.InlineBase, seen)
	typeDefinition.InlineItem = cloneSimpleTypePointer(typeDefinition.InlineItem, seen)
	typeDefinition.InlineMembers = append(
		[]xsd.SimpleType(nil),
		typeDefinition.InlineMembers...,
	)
	for index := range typeDefinition.InlineMembers {
		typeDefinition.InlineMembers[index] = cloneSimpleTypeWithSeen(
			typeDefinition.InlineMembers[index],
			seen,
		)
	}
	return typeDefinition
}

func cloneSimpleTypePointer(
	typeDefinition *xsd.SimpleType,
	seen map[*xsd.SimpleType]*xsd.SimpleType,
) *xsd.SimpleType {
	if typeDefinition == nil {
		return nil
	}
	if clone, ok := seen[typeDefinition]; ok {
		return clone
	}
	clone := new(xsd.SimpleType)
	seen[typeDefinition] = clone
	*clone = cloneSimpleTypeWithSeen(*typeDefinition, seen)
	return clone
}

func cloneComplexType(typeDefinition xsd.ComplexType) xsd.ComplexType {
	typeDefinition.Annotation = cloneAnnotation(typeDefinition.Annotation)
	typeDefinition.ContentAnnotation = cloneAnnotation(typeDefinition.ContentAnnotation)
	typeDefinition.DerivationAnnotation = cloneAnnotation(typeDefinition.DerivationAnnotation)
	if typeDefinition.InlineSimpleType != nil {
		simpleType := cloneSimpleType(*typeDefinition.InlineSimpleType)
		typeDefinition.InlineSimpleType = &simpleType
	}
	typeDefinition.SimpleFacets = append([]xsd.Facet(nil), typeDefinition.SimpleFacets...)
	for index := range typeDefinition.SimpleFacets {
		typeDefinition.SimpleFacets[index].Namespaces = cloneStringMap(
			typeDefinition.SimpleFacets[index].Namespaces,
		)
		typeDefinition.SimpleFacets[index].Annotation = cloneAnnotation(
			typeDefinition.SimpleFacets[index].Annotation,
		)
	}
	typeDefinition.Content = cloneModelGroup(typeDefinition.Content)
	typeDefinition.Attributes = cloneAttributeUses(typeDefinition.Attributes)
	typeDefinition.AttributeGroupRefs = append(
		[]xsd.QName(nil),
		typeDefinition.AttributeGroupRefs...,
	)
	typeDefinition.AttributeGroupReferences = append(
		[]xsd.AttributeGroupReference(nil),
		typeDefinition.AttributeGroupReferences...,
	)
	for index := range typeDefinition.AttributeGroupReferences {
		typeDefinition.AttributeGroupReferences[index].Annotation = cloneAnnotation(
			typeDefinition.AttributeGroupReferences[index].Annotation,
		)
	}
	if typeDefinition.AttributeWildcard != nil {
		wildcard := *typeDefinition.AttributeWildcard
		wildcard.Namespaces = append(
			[]string(nil),
			typeDefinition.AttributeWildcard.Namespaces...,
		)
		wildcard.Annotation = cloneAnnotation(typeDefinition.AttributeWildcard.Annotation)
		typeDefinition.AttributeWildcard = &wildcard
	}
	return typeDefinition
}

func cloneAttributeUses(attributes []xsd.AttributeUse) []xsd.AttributeUse {
	clone := make([]xsd.AttributeUse, len(attributes))
	copy(clone, attributes)
	for index, attribute := range clone {
		clone[index].Annotation = cloneAnnotation(attribute.Annotation)
		clone[index].ValueNamespaces = cloneStringMap(attribute.ValueNamespaces)
		if attribute.InlineSimpleType != nil {
			typeDefinition := cloneSimpleType(*attribute.InlineSimpleType)
			clone[index].InlineSimpleType = &typeDefinition
		}
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
