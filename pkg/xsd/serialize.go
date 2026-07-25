package xsd

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Marshal serializes a schema document deterministically as UTF-8 XML using
// conservative resource limits.
func Marshal(document *Document) ([]byte, error) {
	return MarshalWithOptions(document, MarshalOptions{})
}

// MarshalWithOptions serializes a schema document deterministically as UTF-8
// XML while enforcing explicit output, nesting, and component-work limits.
func MarshalWithOptions(document *Document, options MarshalOptions) ([]byte, error) {
	if document == nil {
		return nil, fmt.Errorf("xsd: document is nil")
	}
	limits, err := normalizeMarshalOptions(options)
	if err != nil {
		return nil, err
	}
	if err := checkMarshalDocument(document, limits); err != nil {
		return nil, err
	}
	serializer := newLimitedSerializer(document, limits.MaxOutputBytes)
	return marshalWithSerializer(document, serializer)
}

func marshalWithSerializer(document *Document, serializer *serializer) ([]byte, error) {
	if err := serializer.schema(document); err != nil {
		return nil, err
	}
	if err := serializer.encoder.Flush(); err != nil {
		return nil, err
	}
	return serializer.buffer.Bytes(), nil
}

type serializer struct {
	buffer     bytes.Buffer
	encoder    tokenEncoder
	prefixes   map[string]string
	namespaces map[string]string
}

type tokenEncoder interface {
	EncodeToken(xml.Token) error
	Flush() error
}

func newSerializer(document *Document) *serializer {
	return newSerializerTo(document, nil)
}

func newLimitedSerializer(document *Document, maximum int64) *serializer {
	serializer := newSerializerTo(document, &maximum)
	return serializer
}

func newSerializerTo(document *Document, maximum *int64) *serializer {
	serializer := &serializer{
		prefixes:   make(map[string]string),
		namespaces: make(map[string]string),
	}
	var output io.Writer = &serializer.buffer
	if maximum != nil {
		output = &marshalLimitWriter{writer: output, remaining: *maximum, maximum: *maximum}
	}
	encoder := xml.NewEncoder(output)
	encoder.Indent("", "  ")
	serializer.encoder = encoder
	for prefix, namespace := range document.Namespaces {
		if prefix != "" && prefix != "xml" && namespace != "" {
			serializer.namespaces[prefix] = namespace
		}
	}
	serializer.namespaces["xs"] = Namespace
	if document.TargetNamespace != "" && !containsNamespace(
		serializer.namespaces,
		document.TargetNamespace,
	) {
		serializer.namespaces["tns"] = document.TargetNamespace
	}
	missing := make([]string, 0)
	for namespace := range documentQNameNamespaces(document) {
		if namespace != "" && !containsNamespace(serializer.namespaces, namespace) {
			missing = append(missing, namespace)
		}
	}
	sort.Strings(missing)
	for _, namespace := range missing {
		for index := 1; ; index++ {
			prefix := fmt.Sprintf("ns%d", index)
			if _, exists := serializer.namespaces[prefix]; !exists {
				serializer.namespaces[prefix] = namespace
				break
			}
		}
	}
	for prefix, namespace := range serializer.namespaces {
		current, exists := serializer.prefixes[namespace]
		if !exists || prefix < current {
			serializer.prefixes[namespace] = prefix
		}
	}
	return serializer
}

func documentQNameNamespaces(document *Document) map[string]struct{} {
	result := make(map[string]struct{})
	add := func(name QName) {
		if name.Namespace != "" {
			result[name.Namespace] = struct{}{}
		}
	}
	simple := func(typeDefinition SimpleType) {
		add(typeDefinition.Base)
		add(typeDefinition.ItemType)
		for _, member := range typeDefinition.MemberTypes {
			add(member)
		}
	}
	var element func(Element)
	var complex func(ComplexType)
	var group func(*ModelGroup)
	element = func(declaration Element) {
		add(declaration.Type)
		add(declaration.Ref)
		add(declaration.SubstitutionGroup)
		for _, constraint := range declaration.IdentityConstraints {
			add(constraint.Refer)
		}
		if declaration.InlineSimpleType != nil {
			simple(*declaration.InlineSimpleType)
		}
		if declaration.InlineComplexType != nil {
			complex(*declaration.InlineComplexType)
		}
	}
	group = func(model *ModelGroup) {
		if model == nil {
			return
		}
		for _, particle := range model.Particles {
			add(particle.GroupRef)
			if particle.Element != nil {
				element(*particle.Element)
			}
			group(particle.Group)
		}
	}
	complex = func(typeDefinition ComplexType) {
		add(typeDefinition.Base)
		group(typeDefinition.Content)
		for _, attribute := range typeDefinition.Attributes {
			add(attribute.Type)
			add(attribute.Ref)
			if attribute.InlineSimpleType != nil {
				simple(*attribute.InlineSimpleType)
			}
		}
		for _, reference := range typeDefinition.AttributeGroupRefs {
			add(reference)
		}
	}
	for _, typeDefinition := range document.SimpleTypes {
		simple(typeDefinition)
	}
	for _, typeDefinition := range document.ComplexTypes {
		complex(typeDefinition)
	}
	for _, definition := range document.ModelGroups {
		group(definition.Content)
	}
	for _, definition := range document.AttributeGroups {
		for _, attribute := range definition.Attributes {
			add(attribute.Type)
			add(attribute.Ref)
			if attribute.InlineSimpleType != nil {
				simple(*attribute.InlineSimpleType)
			}
		}
		for _, reference := range definition.References {
			add(reference)
		}
	}
	for _, attribute := range document.Attributes {
		add(attribute.Type)
		if attribute.InlineSimpleType != nil {
			simple(*attribute.InlineSimpleType)
		}
	}
	for _, declaration := range document.Elements {
		element(declaration)
	}
	for _, redefinition := range document.Redefinitions {
		for _, typeDefinition := range redefinition.SimpleTypes {
			simple(typeDefinition)
		}
		for _, typeDefinition := range redefinition.ComplexTypes {
			complex(typeDefinition)
		}
		for _, definition := range redefinition.ModelGroups {
			group(definition.Content)
		}
		for _, definition := range redefinition.AttributeGroups {
			for _, attribute := range definition.Attributes {
				add(attribute.Type)
				add(attribute.Ref)
				if attribute.InlineSimpleType != nil {
					simple(*attribute.InlineSimpleType)
				}
			}
			for _, reference := range definition.References {
				add(reference)
			}
		}
	}
	return result
}

func containsNamespace(namespaces map[string]string, expected string) bool {
	for _, namespace := range namespaces {
		if namespace == expected {
			return true
		}
	}
	return false
}

func (s *serializer) schema(document *Document) error {
	attributes := make([]xml.Attr, 0, len(s.namespaces)+8)
	prefixes := make([]string, 0, len(s.namespaces))
	for prefix := range s.namespaces {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	for _, prefix := range prefixes {
		attributes = append(attributes, attr("xmlns:"+prefix, s.namespaces[prefix]))
	}
	attributes = appendString(attributes, "id", document.ID)
	attributes = appendString(attributes, "targetNamespace", document.TargetNamespace)
	attributes = appendString(attributes, "elementFormDefault", string(document.ElementFormDefault))
	attributes = appendString(attributes, "attributeFormDefault", string(document.AttributeFormDefault))
	attributes = appendString(attributes, "blockDefault", document.BlockDefault.String())
	attributes = appendString(attributes, "finalDefault", document.FinalDefault.String())
	attributes = appendString(attributes, "version", document.Version)
	attributes = appendString(attributes, "xml:lang", document.Language)
	if document.BaseURI != "" && document.BaseURI != document.SystemID {
		attributes = appendString(attributes, "xml:base", document.BaseURI)
	}
	start := startElement("xs:schema", attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	for _, annotation := range document.Annotations {
		if err := s.annotation(annotation); err != nil {
			return err
		}
	}
	redefinitionIndex := 0
	for _, reference := range document.References {
		if reference.Kind == ReferenceRedefine && redefinitionIndex < len(document.Redefinitions) {
			if err := s.redefinition(document.Redefinitions[redefinitionIndex]); err != nil {
				return err
			}
			redefinitionIndex++
			continue
		}
		if err := s.reference(reference); err != nil {
			return err
		}
	}
	for _, simpleType := range document.SimpleTypes {
		if err := s.simpleType(simpleType); err != nil {
			return err
		}
	}
	for _, complexType := range document.ComplexTypes {
		if err := s.complexType(complexType); err != nil {
			return err
		}
	}
	for _, group := range document.ModelGroups {
		if err := s.modelGroupDefinition(group); err != nil {
			return err
		}
	}
	for _, group := range document.AttributeGroups {
		if err := s.attributeGroup(group); err != nil {
			return err
		}
	}
	for _, notation := range document.Notations {
		if err := s.notation(notation); err != nil {
			return err
		}
	}
	for _, attribute := range document.Attributes {
		if err := s.attribute(attribute); err != nil {
			return err
		}
	}
	for _, element := range document.Elements {
		if err := s.element(element); err != nil {
			return err
		}
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) notation(notation Notation) error {
	attributes := appendString(nil, "id", notation.ID)
	attributes = appendString(attributes, "name", notation.Name)
	attributes = appendString(attributes, "public", notation.Public)
	attributes = appendString(attributes, "system", notation.System)
	start := startElement("xs:notation", attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	if err := s.componentAnnotation(notation.Annotation); err != nil {
		return err
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) annotation(annotation Annotation) error {
	attributes := appendString(nil, "id", annotation.ID)
	start := startElement("xs:annotation", attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	for _, documentation := range annotation.Documentation {
		attributes := appendString(nil, "id", documentation.ID)
		attributes = appendString(attributes, "source", documentation.Source)
		attributes = appendString(attributes, "xml:lang", documentation.Language)
		documentationStart := startElement("xs:documentation", attributes)
		if err := s.encoder.EncodeToken(documentationStart); err != nil {
			return err
		}
		if documentation.Markup != "" {
			if err := validateXMLFragment(documentation.Markup); err != nil {
				return fmt.Errorf("xsd: invalid documentation markup: %w", err)
			}
			if err := s.encoder.Flush(); err != nil {
				return err
			}
			s.buffer.WriteString(documentation.Markup)
		} else {
			if err := s.encoder.EncodeToken(xml.CharData(documentation.Content)); err != nil {
				return err
			}
		}
		if err := s.encoder.EncodeToken(documentationStart.End()); err != nil {
			return err
		}
	}
	for _, appInfo := range annotation.AppInformation {
		if err := validateXMLFragment(appInfo.Content); err != nil {
			return fmt.Errorf("xsd: invalid appinfo content: %w", err)
		}
		appInfoStart := startElement(
			"xs:appinfo",
			appendString(
				appendString(nil, "id", appInfo.ID),
				"source",
				appInfo.Source,
			),
		)
		if err := s.encoder.EncodeToken(appInfoStart); err != nil {
			return err
		}
		if err := s.encoder.Flush(); err != nil {
			return err
		}
		s.buffer.WriteString(appInfo.Content)
		if err := s.encoder.EncodeToken(appInfoStart.End()); err != nil {
			return err
		}
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) componentAnnotation(annotation *Annotation) error {
	if annotation == nil {
		return nil
	}
	return s.annotation(*annotation)
}

func validateXMLFragment(content string) error {
	decoder := xml.NewDecoder(strings.NewReader("<root>" + content + "</root>"))
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if _, directive := token.(xml.Directive); directive {
			return ErrDTDForbidden
		}
	}
}

func (s *serializer) reference(reference SchemaReference) error {
	attributes := appendString(nil, "id", reference.ID)
	attributes = appendString(attributes, "namespace", reference.Namespace)
	attributes = appendString(attributes, "schemaLocation", reference.Location)
	start := startElement("xs:"+string(reference.Kind), attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	if err := s.componentAnnotation(reference.Annotation); err != nil {
		return err
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) redefinition(redefinition Redefinition) error {
	attributes := appendString(nil, "id", redefinition.Reference.ID)
	attributes = appendString(attributes, "schemaLocation", redefinition.Reference.Location)
	start := startElement(
		"xs:redefine",
		attributes,
	)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	if err := s.componentAnnotation(redefinition.Reference.Annotation); err != nil {
		return err
	}
	for _, typeDefinition := range redefinition.SimpleTypes {
		if err := s.simpleType(typeDefinition); err != nil {
			return err
		}
	}
	for _, typeDefinition := range redefinition.ComplexTypes {
		if err := s.complexType(typeDefinition); err != nil {
			return err
		}
	}
	for _, group := range redefinition.ModelGroups {
		if err := s.modelGroupDefinition(group); err != nil {
			return err
		}
	}
	for _, group := range redefinition.AttributeGroups {
		if err := s.attributeGroup(group); err != nil {
			return err
		}
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) simpleType(typeDefinition SimpleType) error {
	attributes := appendString(nil, "id", typeDefinition.ID)
	attributes = appendString(attributes, "name", typeDefinition.Name)
	attributes = appendString(attributes, "final", typeDefinition.Final.String())
	start := startElement("xs:simpleType", attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	if err := s.componentAnnotation(typeDefinition.Annotation); err != nil {
		return err
	}
	var err error
	switch typeDefinition.Variety {
	case SimpleRestriction:
		attributes, attrErr := s.qNameAttribute("base", typeDefinition.Base)
		if attrErr != nil {
			return attrErr
		}
		attributes = appendString(attributes, "id", typeDefinition.VarietyID)
		restriction := startElement("xs:restriction", attributes)
		if err = s.encoder.EncodeToken(restriction); err == nil {
			err = s.componentAnnotation(typeDefinition.VarietyAnnotation)
		}
		if err == nil {
			if typeDefinition.InlineBase != nil {
				err = s.simpleType(*typeDefinition.InlineBase)
			}
		}
		if err == nil {
			for _, facet := range typeDefinition.Facets {
				attributes := appendString(nil, "id", facet.ID)
				attributes = appendString(attributes, "value", facet.Value)
				attributes = appendValueNamespaces(attributes, facet.Namespaces, facet.Value)
				if facet.Fixed {
					attributes = append(attributes, attr("fixed", "true"))
				}
				if err = s.annotatedElement(
					"xs:"+string(facet.Kind),
					attributes,
					facet.Annotation,
				); err != nil {
					break
				}
			}
		}
		if err == nil {
			err = s.encoder.EncodeToken(restriction.End())
		}
	case SimpleList:
		attributes, attrErr := s.qNameAttribute("itemType", typeDefinition.ItemType)
		if attrErr != nil {
			return attrErr
		}
		attributes = appendString(attributes, "id", typeDefinition.VarietyID)
		list := startElement("xs:list", attributes)
		if err = s.encoder.EncodeToken(list); err == nil {
			err = s.componentAnnotation(typeDefinition.VarietyAnnotation)
		}
		if err == nil && typeDefinition.InlineItem != nil {
			err = s.simpleType(*typeDefinition.InlineItem)
		}
		if err == nil {
			err = s.encoder.EncodeToken(list.End())
		}
	case SimpleUnion:
		members := make([]string, 0, len(typeDefinition.MemberTypes))
		for _, member := range typeDefinition.MemberTypes {
			lexical, qNameErr := s.qName(member)
			if qNameErr != nil {
				return qNameErr
			}
			members = append(members, lexical)
		}
		unionAttributes := appendString(nil, "id", typeDefinition.VarietyID)
		unionAttributes = appendString(unionAttributes, "memberTypes", strings.Join(members, " "))
		union := startElement(
			"xs:union",
			unionAttributes,
		)
		if err = s.encoder.EncodeToken(union); err == nil {
			err = s.componentAnnotation(typeDefinition.VarietyAnnotation)
		}
		if err == nil {
			for _, member := range typeDefinition.InlineMembers {
				if err = s.simpleType(member); err != nil {
					break
				}
			}
		}
		if err == nil {
			err = s.encoder.EncodeToken(union.End())
		}
	default:
		return fmt.Errorf("xsd: simple type %q has invalid variety %q", typeDefinition.Name, typeDefinition.Variety)
	}
	if err != nil {
		return err
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) complexType(typeDefinition ComplexType) error {
	attributes := appendString(nil, "id", typeDefinition.ID)
	attributes = appendString(attributes, "name", typeDefinition.Name)
	attributes = appendBool(attributes, "abstract", typeDefinition.Abstract)
	if typeDefinition.MixedSet || typeDefinition.Mixed {
		attributes = append(attributes, attr("mixed", fmt.Sprint(typeDefinition.Mixed)))
	}
	attributes = appendString(attributes, "block", typeDefinition.Block.String())
	attributes = appendString(attributes, "final", typeDefinition.Final.String())
	start := startElement("xs:complexType", attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	if err := s.componentAnnotation(typeDefinition.Annotation); err != nil {
		return err
	}
	if typeDefinition.Derivation != "" {
		contentName := "xs:complexContent"
		if typeDefinition.SimpleContent {
			contentName = "xs:simpleContent"
		}
		content := startElement(
			contentName,
			appendString(nil, "id", typeDefinition.ContentID),
		)
		if err := s.encoder.EncodeToken(content); err != nil {
			return err
		}
		if err := s.componentAnnotation(typeDefinition.ContentAnnotation); err != nil {
			return err
		}
		attributes, err := s.qNameAttribute("base", typeDefinition.Base)
		if err != nil {
			return err
		}
		attributes = appendString(attributes, "id", typeDefinition.DerivationID)
		derivation := startElement("xs:"+string(typeDefinition.Derivation), attributes)
		if err := s.encoder.EncodeToken(derivation); err != nil {
			return err
		}
		if err := s.componentAnnotation(typeDefinition.DerivationAnnotation); err != nil {
			return err
		}
		if typeDefinition.SimpleContent &&
			typeDefinition.Derivation == DerivationRestriction {
			if typeDefinition.InlineSimpleType != nil {
				if err := s.simpleType(*typeDefinition.InlineSimpleType); err != nil {
					return err
				}
			}
			for _, facet := range typeDefinition.SimpleFacets {
				facetAttributes := appendString(nil, "id", facet.ID)
				facetAttributes = appendString(facetAttributes, "value", facet.Value)
				facetAttributes = appendValueNamespaces(
					facetAttributes,
					facet.Namespaces,
					facet.Value,
				)
				if facet.Fixed {
					facetAttributes = append(facetAttributes, attr("fixed", "true"))
				}
				if err := s.annotatedElement(
					"xs:"+string(facet.Kind),
					facetAttributes,
					facet.Annotation,
				); err != nil {
					return err
				}
			}
		}
		if err := s.complexBody(typeDefinition); err != nil {
			return err
		}
		if err := s.encoder.EncodeToken(derivation.End()); err != nil {
			return err
		}
		if err := s.encoder.EncodeToken(content.End()); err != nil {
			return err
		}
	} else if err := s.complexBody(typeDefinition); err != nil {
		return err
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) complexBody(typeDefinition ComplexType) error {
	if typeDefinition.Content != nil {
		if err := s.modelGroup(*typeDefinition.Content); err != nil {
			return err
		}
	}
	for _, attribute := range typeDefinition.Attributes {
		if err := s.attributeUse(attribute); err != nil {
			return err
		}
	}
	if err := s.attributeGroupReferences(
		typeDefinition.AttributeGroupRefs,
		typeDefinition.AttributeGroupReferences,
	); err != nil {
		return err
	}
	if typeDefinition.AttributeWildcard != nil {
		return s.wildcard("xs:anyAttribute", *typeDefinition.AttributeWildcard, nil)
	}
	return nil
}

func (s *serializer) modelGroupDefinition(definition ModelGroupDefinition) error {
	attributes := appendString(nil, "id", definition.ID)
	attributes = appendString(attributes, "name", definition.Name)
	start := startElement("xs:group", attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	if err := s.componentAnnotation(definition.Annotation); err != nil {
		return err
	}
	if definition.Content != nil {
		if err := s.modelGroup(*definition.Content); err != nil {
			return err
		}
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) modelGroup(group ModelGroup) error {
	attributes := appendString(nil, "id", group.ID)
	if group.OccursSet {
		attributes = append(
			attributes,
			occurrenceValues(group.MinOccurs, group.MaxOccurs, group.Unbounded)...,
		)
	}
	start := startElement("xs:"+string(group.Compositor), attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	if err := s.componentAnnotation(group.Annotation); err != nil {
		return err
	}
	if err := s.modelGroupChildren(group); err != nil {
		return err
	}
	return s.encoder.EncodeToken(start.End())
}

func occurrenceValues(minOccurs, maxOccurs uint64, unbounded bool) []xml.Attr {
	return occurrenceAttributes(Particle{
		MinOccurs: minOccurs,
		MaxOccurs: maxOccurs,
		Unbounded: unbounded,
	})
}

func (s *serializer) modelGroupChildren(group ModelGroup) error {
	for _, particle := range group.Particles {
		attributes := occurrenceAttributes(particle)
		if particle.Element != nil {
			if err := s.elementWithAttributes(*particle.Element, attributes); err != nil {
				return err
			}
			continue
		}
		if particle.Group != nil {
			attributes = appendString(attributes, "id", particle.Group.ID)
			start := startElement("xs:"+string(particle.Group.Compositor), attributes)
			if err := s.encoder.EncodeToken(start); err != nil {
				return err
			}
			if err := s.componentAnnotation(particle.Group.Annotation); err != nil {
				return err
			}
			if err := s.modelGroupChildren(*particle.Group); err != nil {
				return err
			}
			if err := s.encoder.EncodeToken(start.End()); err != nil {
				return err
			}
			continue
		}
		if particle.GroupRef.Local != "" {
			qNameAttributes, err := s.qNameAttribute("ref", particle.GroupRef)
			if err != nil {
				return err
			}
			attributes = appendString(attributes, "id", particle.ID)
			if err := s.annotatedElement(
				"xs:group",
				append(attributes, qNameAttributes...),
				particle.Annotation,
			); err != nil {
				return err
			}
			continue
		}
		if particle.Wildcard != nil {
			if err := s.wildcard("xs:any", *particle.Wildcard, attributes); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *serializer) attributeGroup(group AttributeGroup) error {
	attributes := appendString(nil, "id", group.ID)
	attributes = appendString(attributes, "name", group.Name)
	start := startElement("xs:attributeGroup", attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	if err := s.componentAnnotation(group.Annotation); err != nil {
		return err
	}
	for _, attribute := range group.Attributes {
		if err := s.attributeUse(attribute); err != nil {
			return err
		}
	}
	if err := s.attributeGroupReferences(
		group.References,
		group.AttributeGroupReferences,
	); err != nil {
		return err
	}
	if group.Wildcard != nil {
		if err := s.wildcard("xs:anyAttribute", *group.Wildcard, nil); err != nil {
			return err
		}
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) element(element Element) error {
	return s.elementWithAttributes(element, nil)
}

func (s *serializer) elementWithAttributes(element Element, attributes []xml.Attr) error {
	attributes = appendString(attributes, "id", element.ID)
	attributes = appendString(attributes, "name", element.Name)
	var err error
	attributes, err = s.appendQName(attributes, "type", element.Type)
	if err != nil {
		return err
	}
	attributes, err = s.appendQName(attributes, "ref", element.Ref)
	if err != nil {
		return err
	}
	attributes, err = s.appendQName(attributes, "substitutionGroup", element.SubstitutionGroup)
	if err != nil {
		return err
	}
	attributes = appendString(attributes, "form", string(element.Form))
	attributes = appendBool(attributes, "abstract", element.Abstract)
	attributes = appendBool(attributes, "nillable", element.Nillable)
	attributes = appendOptionalString(attributes, "default", element.Default, element.DefaultSet)
	attributes = appendOptionalString(attributes, "fixed", element.Fixed, element.FixedSet)
	attributes = appendValueNamespaces(
		attributes,
		element.ValueNamespaces,
		element.Default,
		element.Fixed,
	)
	attributes = appendString(attributes, "block", element.Block.String())
	attributes = appendString(attributes, "final", element.Final.String())
	start := startElement("xs:element", attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	if err := s.componentAnnotation(element.Annotation); err != nil {
		return err
	}
	if element.InlineSimpleType != nil {
		if err := s.simpleType(*element.InlineSimpleType); err != nil {
			return err
		}
	}
	if element.InlineComplexType != nil {
		if err := s.complexType(*element.InlineComplexType); err != nil {
			return err
		}
	}
	for _, constraint := range element.IdentityConstraints {
		if err := s.identityConstraint(constraint); err != nil {
			return err
		}
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) identityConstraint(constraint IdentityConstraint) error {
	attributes := appendString(nil, "id", constraint.ID)
	attributes = appendString(attributes, "name", constraint.Name)
	var err error
	attributes, err = s.appendQName(attributes, "refer", constraint.Refer)
	if err != nil {
		return err
	}
	start := startElement("xs:"+string(constraint.Kind), attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	if err := s.componentAnnotation(constraint.Annotation); err != nil {
		return err
	}
	selectorAttributes := appendString(nil, "id", constraint.SelectorID)
	selectorAttributes = append(selectorAttributes, attr("xpath", constraint.Selector))
	if err := s.annotatedElement(
		"xs:selector",
		selectorAttributes,
		constraint.SelectorAnnotation,
	); err != nil {
		return err
	}
	for index, field := range constraint.Fields {
		var annotation *Annotation
		id := ""
		if index < len(constraint.FieldAnnotations) {
			annotation = constraint.FieldAnnotations[index]
		}
		if index < len(constraint.FieldIDs) {
			id = constraint.FieldIDs[index]
		}
		fieldAttributes := appendString(nil, "id", id)
		fieldAttributes = append(fieldAttributes, attr("xpath", field))
		if err := s.annotatedElement(
			"xs:field",
			fieldAttributes,
			annotation,
		); err != nil {
			return err
		}
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) attribute(attribute Attribute) error {
	use := AttributeUse{
		ID:               attribute.ID,
		Name:             attribute.Name,
		Type:             attribute.Type,
		Default:          attribute.Default,
		Fixed:            attribute.Fixed,
		DefaultSet:       attribute.DefaultSet,
		FixedSet:         attribute.FixedSet,
		ValueNamespaces:  attribute.ValueNamespaces,
		InlineSimpleType: attribute.InlineSimpleType,
		Annotation:       attribute.Annotation,
	}
	return s.attributeUse(use)
}

func (s *serializer) attributeUse(attribute AttributeUse) error {
	attributes := appendString(nil, "id", attribute.ID)
	attributes = appendString(attributes, "name", attribute.Name)
	var err error
	attributes, err = s.appendQName(attributes, "type", attribute.Type)
	if err != nil {
		return err
	}
	attributes, err = s.appendQName(attributes, "ref", attribute.Ref)
	if err != nil {
		return err
	}
	attributes = appendString(attributes, "form", string(attribute.Form))
	if attribute.Use != "" && attribute.Use != AttributeOptional {
		attributes = append(attributes, attr("use", string(attribute.Use)))
	}
	attributes = appendOptionalString(attributes, "default", attribute.Default, attribute.DefaultSet)
	attributes = appendOptionalString(attributes, "fixed", attribute.Fixed, attribute.FixedSet)
	attributes = appendValueNamespaces(
		attributes,
		attribute.ValueNamespaces,
		attribute.Default,
		attribute.Fixed,
	)
	start := startElement("xs:attribute", attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	if err := s.componentAnnotation(attribute.Annotation); err != nil {
		return err
	}
	if attribute.InlineSimpleType != nil {
		if err := s.simpleType(*attribute.InlineSimpleType); err != nil {
			return err
		}
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) wildcard(name string, wildcard Wildcard, attributes []xml.Attr) error {
	attributes = appendString(attributes, "id", wildcard.ID)
	attributes = appendString(attributes, "namespace", strings.Join(wildcard.Namespaces, " "))
	if wildcard.ProcessContents != "" && wildcard.ProcessContents != ProcessStrict {
		attributes = append(attributes, attr("processContents", string(wildcard.ProcessContents)))
	}
	return s.annotatedElement(name, attributes, wildcard.Annotation)
}

func (s *serializer) annotatedElement(
	name string,
	attributes []xml.Attr,
	annotation *Annotation,
) error {
	start := startElement(name, attributes)
	if err := s.encoder.EncodeToken(start); err != nil {
		return err
	}
	if err := s.componentAnnotation(annotation); err != nil {
		return err
	}
	return s.encoder.EncodeToken(start.End())
}

func (s *serializer) attributeGroupReferences(
	legacy []QName,
	references []AttributeGroupReference,
) error {
	if len(references) == 0 {
		references = make([]AttributeGroupReference, len(legacy))
		for index, reference := range legacy {
			references[index].Ref = reference
		}
	}
	for _, reference := range references {
		attributes, err := s.qNameAttribute("ref", reference.Ref)
		if err != nil {
			return err
		}
		attributes = appendString(attributes, "id", reference.ID)
		if err := s.annotatedElement(
			"xs:attributeGroup",
			attributes,
			reference.Annotation,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *serializer) qNameAttribute(name string, value QName) ([]xml.Attr, error) {
	return s.appendQName(nil, name, value)
}

func (s *serializer) appendQName(attributes []xml.Attr, name string, value QName) ([]xml.Attr, error) {
	if value.Local == "" {
		return attributes, nil
	}
	lexical, err := s.qName(value)
	if err != nil {
		return nil, err
	}
	return append(attributes, attr(name, lexical)), nil
}

func (s *serializer) qName(value QName) (string, error) {
	if value.Local == "" {
		return "", nil
	}
	if value.Namespace == "" {
		return value.Local, nil
	}
	prefix, ok := s.prefixes[value.Namespace]
	if !ok {
		return "", fmt.Errorf(
			"xsd: no namespace prefix for {%s}%s",
			value.Namespace,
			value.Local,
		)
	}
	return prefix + ":" + value.Local, nil
}

func occurrenceAttributes(particle Particle) []xml.Attr {
	attributes := make([]xml.Attr, 0, 2)
	if particle.MinOccurs != 1 {
		attributes = append(attributes, attr("minOccurs", fmt.Sprint(particle.MinOccurs)))
	}
	if particle.Unbounded {
		attributes = append(attributes, attr("maxOccurs", "unbounded"))
	} else if particle.MaxOccurs != 1 {
		attributes = append(attributes, attr("maxOccurs", fmt.Sprint(particle.MaxOccurs)))
	}
	return attributes
}

func startElement(name string, attributes []xml.Attr) xml.StartElement {
	return xml.StartElement{Name: xml.Name{Local: name}, Attr: attributes}
}

func attr(name string, value string) xml.Attr {
	return xml.Attr{Name: xml.Name{Local: name}, Value: value}
}

func appendValueNamespaces(
	attributes []xml.Attr,
	namespaces map[string]string,
	values ...string,
) []xml.Attr {
	prefixes := make(map[string]struct{})
	for _, value := range values {
		prefix := ""
		if index := strings.IndexByte(value, ':'); index >= 0 {
			prefix = value[:index]
		}
		if _, ok := namespaces[prefix]; ok && prefix != "xml" {
			prefixes[prefix] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(prefixes))
	for prefix := range prefixes {
		ordered = append(ordered, prefix)
	}
	sort.Strings(ordered)
	for _, prefix := range ordered {
		name := "xmlns"
		if prefix != "" {
			name += ":" + prefix
		}
		attributes = append(attributes, attr(name, namespaces[prefix]))
	}
	return attributes
}

func appendString(attributes []xml.Attr, name string, value string) []xml.Attr {
	if value == "" {
		return attributes
	}
	return append(attributes, attr(name, value))
}

func appendOptionalString(attributes []xml.Attr, name string, value string, set bool) []xml.Attr {
	if value == "" && !set {
		return attributes
	}
	return append(attributes, attr(name, value))
}

func appendBool(attributes []xml.Attr, name string, value bool) []xml.Attr {
	if !value {
		return attributes
	}
	return append(attributes, attr(name, "true"))
}
