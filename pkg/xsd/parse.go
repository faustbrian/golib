package xsd

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

const (
	defaultMaxDocumentBytes int64 = 8 << 20
	defaultMaxParseDepth          = 256
	defaultMaxElements            = 1000000
)

// ParseOptions configures a single document parse. A zero byte limit uses a
// conservative default; a negative value is invalid.
type ParseOptions struct {
	SystemID         string
	MaxDocumentBytes int64
	MaxDepth         int
	MaxElements      int
}

// Parse decodes one XML Schema document without resolving external resources.
func Parse(ctx context.Context, source []byte, options ParseOptions) (*Document, error) {
	limit := options.MaxDocumentBytes
	if limit == 0 {
		limit = defaultMaxDocumentBytes
	}
	if options.MaxDepth == 0 {
		options.MaxDepth = defaultMaxParseDepth
	}
	if options.MaxElements == 0 {
		options.MaxElements = defaultMaxElements
	}
	if limit < 0 || options.MaxDepth < 0 || options.MaxElements < 0 {
		return nil, fmt.Errorf("xsd: parse limits must not be negative")
	}
	if int64(len(source)) > limit {
		return nil, fmt.Errorf("%w: document bytes exceed %d", ErrLimitExceeded, limit)
	}
	if err := validateAnnotationPlacement(ctx, source, options); err != nil {
		return nil, err
	}
	return parseValidated(ctx, source, options)
}

func parseValidated(ctx context.Context, source []byte, options ParseOptions) (*Document, error) {
	decoder := xml.NewDecoder(&contextReader{ctx: ctx, reader: bytes.NewReader(source)})
	decoder.Strict = true
	decoder.Entity = map[string]string{}

	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, located(decoder, options.SystemID, ErrNotSchema)
			}
			return nil, located(decoder, options.SystemID, err)
		}

		switch value := token.(type) {
		case xml.Directive:
			return nil, located(decoder, options.SystemID, ErrDTDForbidden)
		case xml.StartElement:
			if value.Name.Space != Namespace || value.Name.Local != "schema" {
				return nil, located(decoder, options.SystemID, ErrNotSchema)
			}
			return parseDocument(decoder, value, options.SystemID)
		}
	}
}

type annotationFrame struct {
	name            xml.Name
	elements        int
	annotations     int
	opaqueExtension bool
	notationBase    bool
}

func validateAnnotationPlacement(
	ctx context.Context,
	source []byte,
	options ParseOptions,
) error {
	systemID := options.SystemID
	decoder := xml.NewDecoder(&contextReader{ctx: ctx, reader: bytes.NewReader(source)})
	decoder.Strict = true
	decoder.Entity = map[string]string{}
	stack := make([]annotationFrame, 0, 16)
	elements := 0
	notations := make(map[string]struct{})
	notationReferences := make([]string, 0)
	identifiers := make(map[string]struct{})
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			for _, reference := range notationReferences {
				if _, declared := notations[reference]; !declared {
					return located(
						decoder,
						systemID,
						fmt.Errorf("xsd: NOTATION value %q is not declared", reference),
					)
				}
			}
			return nil
		}
		if err != nil {
			return located(decoder, systemID, err)
		}
		switch value := token.(type) {
		case xml.Directive:
			return located(decoder, systemID, ErrDTDForbidden)
		case xml.StartElement:
			elements++
			if elements > options.MaxElements {
				return located(decoder, systemID, fmt.Errorf(
					"%w: element count exceeds %d",
					ErrLimitExceeded,
					options.MaxElements,
				))
			}
			if len(stack)+1 > options.MaxDepth {
				return located(decoder, systemID, fmt.Errorf(
					"%w: element depth exceeds %d",
					ErrLimitExceeded,
					options.MaxDepth,
				))
			}
			opaque := false
			notationBase := false
			if len(stack) > 0 {
				parent := &stack[len(stack)-1]
				opaque = parent.opaqueExtension
				if !opaque && value.Name == (xml.Name{Space: Namespace, Local: "attribute"}) &&
					(parent.name == (xml.Name{Space: Namespace, Local: "schema"}) ||
						parent.name == (xml.Name{Space: Namespace, Local: "redefine"})) {
					for _, attribute := range value.Attr {
						if attribute.Name.Space == "" && attribute.Name.Local == "use" {
							return located(decoder, systemID, fmt.Errorf(
								"xsd: global attribute declaration cannot specify use",
							))
						}
					}
				}
				if !opaque && value.Name == (xml.Name{Space: Namespace, Local: "annotation"}) &&
					parent.name != (xml.Name{Space: Namespace, Local: "schema"}) {
					if parent.annotations > 0 || parent.elements > 0 {
						return located(
							decoder,
							systemID,
							fmt.Errorf("xsd: annotation must be the first and only annotation child"),
						)
					}
					parent.annotations++
				}
				parent.elements++
				if !opaque && value.Name == (xml.Name{Space: Namespace, Local: "enumeration"}) &&
					parent.notationBase {
					for _, attribute := range value.Attr {
						if attribute.Name.Space == "" && attribute.Name.Local == "value" &&
							!strings.Contains(attribute.Value, ":") {
							notationReferences = append(notationReferences, attribute.Value)
						}
					}
				}
			}
			if !opaque && value.Name.Space == Namespace {
				for _, attribute := range value.Attr {
					if attribute.Name.Space != "" || attribute.Name.Local != "id" {
						continue
					}
					if err := datatype.ValidateBuiltInLexical("ID", attribute.Value); err != nil {
						return located(
							decoder,
							systemID,
							fmt.Errorf("xsd: invalid ID %q", attribute.Value),
						)
					}
					if _, duplicate := identifiers[attribute.Value]; duplicate {
						return located(
							decoder,
							systemID,
							fmt.Errorf("xsd: duplicate ID %q", attribute.Value),
						)
					}
					identifiers[attribute.Value] = struct{}{}
				}
			}
			if !opaque && value.Name == (xml.Name{Space: Namespace, Local: "notation"}) {
				name := ""
				identifier := false
				for _, attribute := range value.Attr {
					if attribute.Name.Space != "" {
						continue
					}
					switch attribute.Name.Local {
					case "name":
						name = attribute.Value
					case "public", "system":
						identifier = true
					}
				}
				if name == "" || !identifier {
					return located(decoder, systemID, fmt.Errorf(
						"xsd: notation requires a name and public or system identifier",
					))
				}
				notations[name] = struct{}{}
			}
			if !opaque && value.Name == (xml.Name{Space: Namespace, Local: "restriction"}) {
				for _, attribute := range value.Attr {
					if attribute.Name.Space == "" && attribute.Name.Local == "base" {
						base := strings.TrimSpace(attribute.Value)
						notationBase = base == "NOTATION" || strings.HasSuffix(base, ":NOTATION")
					}
				}
			}
			if value.Name.Space == Namespace &&
				(value.Name.Local == "appinfo" || value.Name.Local == "documentation") {
				opaque = true
			}
			stack = append(stack, annotationFrame{
				name: value.Name, opaqueExtension: opaque, notationBase: notationBase,
			})
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
}

func parseDocument(decoder *xml.Decoder, root xml.StartElement, systemID string) (*Document, error) {
	if err := validateSchemaAttributes(
		root,
		"attributeFormDefault",
		"blockDefault",
		"elementFormDefault",
		"finalDefault",
		"id",
		"targetNamespace",
		"version",
	); err != nil {
		return nil, located(decoder, systemID, err)
	}
	document := &Document{
		SystemID:             systemID,
		BaseURI:              systemID,
		Namespaces:           make(map[string]string),
		ElementFormDefault:   FormUnqualified,
		AttributeFormDefault: FormUnqualified,
	}

	for _, attribute := range root.Attr {
		if attribute.Name.Space == "" && attribute.Name.Local == "xmlns" {
			document.Namespaces[""] = attribute.Value
			continue
		}
		if attribute.Name.Space == "xmlns" {
			document.Namespaces[attribute.Name.Local] = attribute.Value
			continue
		}
		if attribute.Name.Space == "http://www.w3.org/XML/1998/namespace" && attribute.Name.Local == "base" {
			baseURI, err := resolveURI(systemID, attribute.Value)
			if err != nil {
				return nil, located(decoder, systemID, err)
			}
			document.BaseURI = baseURI
			continue
		}
		if attribute.Name.Space == "" && attribute.Name.Local == "targetNamespace" {
			document.TargetNamespace = attribute.Value
			continue
		}
		if attribute.Name.Space == "" && attribute.Name.Local == "id" {
			document.ID = attribute.Value
			continue
		}
		if attribute.Name.Space == "" && attribute.Name.Local == "elementFormDefault" {
			form, err := parseForm(attribute.Value)
			if err != nil {
				return nil, located(decoder, systemID, err)
			}
			document.ElementFormDefault = form
			continue
		}
		if attribute.Name.Space == "" && attribute.Name.Local == "attributeFormDefault" {
			form, err := parseForm(attribute.Value)
			if err != nil {
				return nil, located(decoder, systemID, err)
			}
			document.AttributeFormDefault = form
			continue
		}
		if attribute.Name.Space == "" && attribute.Name.Local == "blockDefault" {
			set, err := parseDerivationSet(attribute.Value)
			if err != nil {
				return nil, located(decoder, systemID, err)
			}
			document.BlockDefault = set
			continue
		}
		if attribute.Name.Space == "" && attribute.Name.Local == "finalDefault" {
			set, err := parseDerivationSet(attribute.Value)
			if err != nil {
				return nil, located(decoder, systemID, err)
			}
			document.FinalDefault = set
			continue
		}
		if attribute.Name.Space == "" && attribute.Name.Local == "version" {
			document.Version = attribute.Value
			continue
		}
		if attribute.Name.Space == "http://www.w3.org/XML/1998/namespace" && attribute.Name.Local == "lang" {
			document.Language = attribute.Value
		}
	}

	componentsStarted := false
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, located(decoder, systemID, err)
		}
		switch value := token.(type) {
		case xml.Directive:
			return nil, located(decoder, systemID, ErrDTDForbidden)
		case xml.StartElement:
			if value.Name.Space == Namespace && value.Name.Local == "annotation" {
				parsedAnnotation, parseErr := parseAnnotation(decoder, value)
				if parseErr != nil {
					return nil, located(decoder, systemID, parseErr)
				}
				document.Annotations = append(document.Annotations, parsedAnnotation)
				continue
			}
			if value.Name.Space == Namespace {
				kind, ok := referenceKind(value.Name.Local)
				if ok {
					if componentsStarted {
						return nil, located(
							decoder,
							systemID,
							fmt.Errorf("xsd: %s must precede schema components", kind),
						)
					}
					reference, parseErr := parseSchemaReference(kind, document.BaseURI, value)
					if parseErr != nil {
						return nil, located(decoder, systemID, parseErr)
					}
					document.References = append(document.References, reference)
					if kind == ReferenceRedefine {
						redefinition, redefineErr := parseRedefinition(
							decoder,
							value,
							reference,
							document.Namespaces,
						)
						if redefineErr != nil {
							return nil, located(decoder, systemID, redefineErr)
						}
						document.Redefinitions = append(document.Redefinitions, redefinition)
						document.References[len(document.References)-1] = redefinition.Reference
						continue
					}
					annotations, parseErr := parseAnnotationChildren(decoder, value)
					if parseErr != nil {
						return nil, located(decoder, systemID, parseErr)
					}
					document.References[len(document.References)-1].Annotation = annotations
					continue
				}
				componentsStarted = true
				switch value.Name.Local {
				case "notation":
					notation, parseErr := parseNotation(decoder, value)
					if parseErr != nil {
						return nil, located(decoder, systemID, parseErr)
					}
					document.Notations = append(document.Notations, notation)
					continue
				case "element":
					element, parseErr := parseElement(value, document.Namespaces)
					if parseErr != nil {
						return nil, located(decoder, systemID, parseErr)
					}
					document.Elements = append(document.Elements, element)
					if err := parseElementBody(
						decoder,
						value,
						&document.Elements[len(document.Elements)-1],
						document.Namespaces,
					); err != nil {
						return nil, located(decoder, systemID, err)
					}
					continue
				case "attribute":
					attribute, parseErr := parseAttribute(value, document.Namespaces)
					if parseErr != nil {
						return nil, located(decoder, systemID, parseErr)
					}
					document.Attributes = append(document.Attributes, attribute)
					inline, annotations, err := parseAttributeBody(decoder, value, document.Namespaces)
					if err != nil {
						return nil, located(decoder, systemID, err)
					}
					document.Attributes[len(document.Attributes)-1].InlineSimpleType = inline
					document.Attributes[len(document.Attributes)-1].Annotation = annotations
					continue
				case "simpleType":
					simpleType, parseErr := parseSimpleType(decoder, value, document.Namespaces)
					if parseErr != nil {
						return nil, located(decoder, systemID, parseErr)
					}
					document.SimpleTypes = append(document.SimpleTypes, simpleType)
					continue
				case "complexType":
					complexType, parseErr := parseComplexType(decoder, value, document.Namespaces)
					if parseErr != nil {
						return nil, located(decoder, systemID, parseErr)
					}
					document.ComplexTypes = append(document.ComplexTypes, complexType)
					continue
				case "group":
					group, parseErr := parseModelGroupDefinition(
						decoder,
						value,
						document.Namespaces,
					)
					if parseErr != nil {
						return nil, located(decoder, systemID, parseErr)
					}
					document.ModelGroups = append(document.ModelGroups, group)
					continue
				case "attributeGroup":
					group, parseErr := parseAttributeGroupDefinition(
						decoder,
						value,
						document.Namespaces,
					)
					if parseErr != nil {
						return nil, located(decoder, systemID, parseErr)
					}
					document.AttributeGroups = append(document.AttributeGroups, group)
					continue
				}
				return nil, located(
					decoder,
					systemID,
					fmt.Errorf("xsd: unknown schema component %q", value.Name.Local),
				)
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return nil, located(decoder, systemID, err)
			}
		case xml.EndElement:
			if value.Name == root.Name {
				return document, nil
			}
		}
	}
}

func parseNotation(decoder *xml.Decoder, start xml.StartElement) (Notation, error) {
	if err := validateSchemaAttributes(start, "id", "name", "public", "system"); err != nil {
		return Notation{}, err
	}
	notation := Notation{}
	for _, attribute := range start.Attr {
		if attribute.Name.Space != "" {
			continue
		}
		switch attribute.Name.Local {
		case "id":
			notation.ID = attribute.Value
		case "name":
			notation.Name = attribute.Value
		case "public":
			notation.Public = attribute.Value
		case "system":
			notation.System = attribute.Value
		}
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return Notation{}, err
		}
		switch value := token.(type) {
		case xml.Directive:
			return Notation{}, ErrDTDForbidden
		case xml.StartElement:
			if value.Name == (xml.Name{Space: Namespace, Local: "annotation"}) {
				parsedAnnotation, parseErr := parseAnnotation(decoder, value)
				if parseErr != nil {
					return Notation{}, parseErr
				}
				notation.Annotation = &parsedAnnotation
				continue
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return Notation{}, err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return notation, nil
			}
		}
	}
}

func parseRedefinition(
	decoder *xml.Decoder,
	start xml.StartElement,
	reference SchemaReference,
	namespaces map[string]string,
) (Redefinition, error) {
	if err := validateSchemaAttributes(start, "id", "schemaLocation"); err != nil {
		return Redefinition{}, err
	}
	namespaces = namespaceScope(namespaces, start)
	redefinition := Redefinition{Reference: reference}
	for {
		token, err := decoder.Token()
		if err != nil {
			return Redefinition{}, err
		}
		switch value := token.(type) {
		case xml.Directive:
			return Redefinition{}, ErrDTDForbidden
		case xml.StartElement:
			if value.Name.Space != Namespace {
				return Redefinition{}, skipUnsupportedElement(decoder, value)
			}
			if value.Name.Local == "annotation" {
				annotation, parseErr := parseAnnotation(decoder, value)
				if parseErr != nil {
					return Redefinition{}, parseErr
				}
				redefinition.Reference.Annotation = &annotation
				continue
			}
			switch value.Name.Local {
			case "simpleType":
				definition, parseErr := parseSimpleType(decoder, value, namespaces)
				if parseErr != nil {
					return Redefinition{}, parseErr
				}
				redefinition.SimpleTypes = append(redefinition.SimpleTypes, definition)
			case "complexType":
				definition, parseErr := parseComplexType(decoder, value, namespaces)
				if parseErr != nil {
					return Redefinition{}, parseErr
				}
				redefinition.ComplexTypes = append(redefinition.ComplexTypes, definition)
			case "group":
				definition, parseErr := parseModelGroupDefinition(decoder, value, namespaces)
				if parseErr != nil {
					return Redefinition{}, parseErr
				}
				redefinition.ModelGroups = append(redefinition.ModelGroups, definition)
			case "attributeGroup":
				definition, parseErr := parseAttributeGroupDefinition(decoder, value, namespaces)
				if parseErr != nil {
					return Redefinition{}, parseErr
				}
				redefinition.AttributeGroups = append(redefinition.AttributeGroups, definition)
			default:
				if err := skipUnsupportedElement(decoder, value); err != nil {
					return Redefinition{}, err
				}
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return redefinition, nil
			}
		}
	}
}

func parseElement(start xml.StartElement, namespaces map[string]string) (Element, error) {
	if err := validateSchemaAttributes(
		start,
		"abstract",
		"block",
		"default",
		"final",
		"fixed",
		"form",
		"id",
		"maxOccurs",
		"minOccurs",
		"name",
		"nillable",
		"ref",
		"substitutionGroup",
		"type",
	); err != nil {
		return Element{}, err
	}
	namespaces = namespaceScope(namespaces, start)
	var element Element
	for _, attribute := range start.Attr {
		if attribute.Name.Space != "" {
			continue
		}
		switch attribute.Name.Local {
		case "id":
			element.ID = attribute.Value
		case "name":
			element.Name = attribute.Value
		case "type":
			name, err := parseQName(attribute.Value, namespaces)
			if err != nil {
				return Element{}, err
			}
			element.Type = name
		case "ref":
			name, err := parseQName(attribute.Value, namespaces)
			if err != nil {
				return Element{}, err
			}
			element.Ref = name
		case "substitutionGroup":
			name, err := parseQName(attribute.Value, namespaces)
			if err != nil {
				return Element{}, err
			}
			element.SubstitutionGroup = name
		case "form":
			form, err := parseForm(attribute.Value)
			if err != nil {
				return Element{}, err
			}
			element.Form = form
		case "abstract":
			value, err := parseBoolean(attribute.Value)
			if err != nil {
				return Element{}, err
			}
			element.Abstract = value
		case "nillable":
			value, err := parseBoolean(attribute.Value)
			if err != nil {
				return Element{}, err
			}
			element.Nillable = value
		case "default":
			element.Default = attribute.Value
			element.DefaultSet = true
		case "fixed":
			element.Fixed = attribute.Value
			element.FixedSet = true
		case "block":
			set, err := parseDerivationSet(attribute.Value)
			if err != nil {
				return Element{}, err
			}
			element.Block = set
		case "final":
			set, err := parseDerivationSet(attribute.Value)
			if err != nil {
				return Element{}, err
			}
			element.Final = set
		}
	}
	if element.DefaultSet || element.FixedSet {
		element.ValueNamespaces = cloneNamespaces(namespaces)
	}
	return element, nil
}

func parseElementBody(
	decoder *xml.Decoder,
	start xml.StartElement,
	element *Element,
	namespaces map[string]string,
) error {
	namespaces = namespaceScope(namespaces, start)
	identityConstraintsStarted := false
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.Directive:
			return ErrDTDForbidden
		case xml.StartElement:
			if value.Name.Space == Namespace {
				if value.Name.Local == "annotation" {
					annotation, parseErr := parseAnnotation(decoder, value)
					if parseErr != nil {
						return parseErr
					}
					element.Annotation = &annotation
					continue
				}
				if value.Name.Local == "simpleType" {
					if identityConstraintsStarted {
						return fmt.Errorf("xsd: element type must precede identity constraints")
					}
					if element.InlineSimpleType != nil || element.InlineComplexType != nil {
						return fmt.Errorf("xsd: element has multiple inline types")
					}
					typeDefinition, parseErr := parseSimpleType(decoder, value, namespaces)
					if parseErr != nil {
						return parseErr
					}
					element.InlineSimpleType = &typeDefinition
					continue
				}
				if value.Name.Local == "complexType" {
					if identityConstraintsStarted {
						return fmt.Errorf("xsd: element type must precede identity constraints")
					}
					if element.InlineSimpleType != nil || element.InlineComplexType != nil {
						return fmt.Errorf("xsd: element has multiple inline types")
					}
					typeDefinition, parseErr := parseComplexType(decoder, value, namespaces)
					if parseErr != nil {
						return parseErr
					}
					element.InlineComplexType = &typeDefinition
					continue
				}
				kind := IdentityKind(value.Name.Local)
				if kind == IdentityUnique || kind == IdentityKey || kind == IdentityKeyRef {
					identityConstraintsStarted = true
					constraint, parseErr := parseIdentityConstraint(
						decoder,
						value,
						kind,
						namespaces,
					)
					if parseErr != nil {
						return parseErr
					}
					element.IdentityConstraints = append(
						element.IdentityConstraints,
						constraint,
					)
					continue
				}
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return nil
			}
		}
	}
}

func parseIdentityConstraint(
	decoder *xml.Decoder,
	start xml.StartElement,
	kind IdentityKind,
	namespaces map[string]string,
) (IdentityConstraint, error) {
	if err := validateSchemaAttributes(start, "id", "name", "refer"); err != nil {
		return IdentityConstraint{}, err
	}
	namespaces = namespaceScope(namespaces, start)
	constraint := IdentityConstraint{
		Kind:       kind,
		Namespaces: cloneNamespaces(namespaces),
	}
	for _, attribute := range start.Attr {
		if attribute.Name.Space != "" {
			continue
		}
		switch attribute.Name.Local {
		case "id":
			constraint.ID = attribute.Value
		case "name":
			constraint.Name = attribute.Value
		case "refer":
			reference, err := parseQName(attribute.Value, namespaces)
			if err != nil {
				return IdentityConstraint{}, err
			}
			constraint.Refer = reference
		}
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return IdentityConstraint{}, err
		}
		switch value := token.(type) {
		case xml.Directive:
			return IdentityConstraint{}, ErrDTDForbidden
		case xml.StartElement:
			if value.Name == (xml.Name{Space: Namespace, Local: "annotation"}) {
				annotation, parseErr := parseAnnotation(decoder, value)
				if parseErr != nil {
					return IdentityConstraint{}, parseErr
				}
				constraint.Annotation = &annotation
				continue
			}
			if value.Name.Space == Namespace &&
				(value.Name.Local == "selector" || value.Name.Local == "field") {
				if attributeErr := validateSchemaAttributes(value, "id", "xpath"); attributeErr != nil {
					return IdentityConstraint{}, attributeErr
				}
				xpath := ""
				id := ""
				for _, attribute := range value.Attr {
					if attribute.Name.Space == "" {
						switch attribute.Name.Local {
						case "id":
							id = attribute.Value
						case "xpath":
							xpath = attribute.Value
						}
					}
				}
				if value.Name.Local == "selector" {
					if constraint.Selector != "" {
						return IdentityConstraint{}, fmt.Errorf(
							"xsd: identity constraint has multiple selectors",
						)
					}
					if len(constraint.Fields) > 0 {
						return IdentityConstraint{}, fmt.Errorf(
							"xsd: identity selector must precede fields",
						)
					}
					constraint.Selector = xpath
					constraint.SelectorID = id
				} else {
					constraint.Fields = append(constraint.Fields, xpath)
					constraint.FieldIDs = append(constraint.FieldIDs, id)
				}
				annotation, parseErr := parseAnnotationChildren(decoder, value)
				if parseErr != nil {
					return IdentityConstraint{}, parseErr
				}
				if value.Name.Local == "selector" {
					constraint.SelectorAnnotation = annotation
				} else {
					constraint.FieldAnnotations = append(
						constraint.FieldAnnotations,
						annotation,
					)
				}
				continue
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return IdentityConstraint{}, err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return constraint, nil
			}
		}
	}
}

func cloneNamespaces(namespaces map[string]string) map[string]string {
	clone := make(map[string]string, len(namespaces))
	for prefix, namespace := range namespaces {
		clone[prefix] = namespace
	}
	return clone
}

func skipUnsupportedElement(_ *xml.Decoder, start xml.StartElement) error {
	return fmt.Errorf(
		"xsd: unexpected element {%s}%s in schema grammar",
		start.Name.Space,
		start.Name.Local,
	)
}

func validateSchemaAttributes(start xml.StartElement, allowed ...string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}
	for _, attribute := range start.Attr {
		if attribute.Name.Space != "" || attribute.Name.Local == "xmlns" {
			continue
		}
		if _, ok := allowedSet[attribute.Name.Local]; !ok {
			return fmt.Errorf(
				"xsd: unexpected attribute %q on %s",
				attribute.Name.Local,
				start.Name.Local,
			)
		}
	}
	return nil
}

func attributeValue(start xml.StartElement, name string) string {
	for _, attribute := range start.Attr {
		if attribute.Name.Space == "" && attribute.Name.Local == name {
			return attribute.Value
		}
	}
	return ""
}

func namespaceScope(parent map[string]string, start xml.StartElement) map[string]string {
	result := parent
	cloned := false
	for _, attribute := range start.Attr {
		prefix := ""
		if attribute.Name.Space == "" && attribute.Name.Local == "xmlns" {
			// The default namespace uses the empty prefix.
		} else if attribute.Name.Space == "xmlns" {
			prefix = attribute.Name.Local
		} else {
			continue
		}
		if !cloned {
			result = cloneNamespaces(parent)
			cloned = true
		}
		result[prefix] = attribute.Value
	}
	return result
}

func parseAttribute(start xml.StartElement, namespaces map[string]string) (Attribute, error) {
	if err := validateSchemaAttributes(start, "default", "fixed", "id", "name", "type"); err != nil {
		return Attribute{}, err
	}
	namespaces = namespaceScope(namespaces, start)
	var declaration Attribute
	for _, attribute := range start.Attr {
		if attribute.Name.Space != "" {
			continue
		}
		switch attribute.Name.Local {
		case "id":
			declaration.ID = attribute.Value
		case "name":
			declaration.Name = attribute.Value
		case "type":
			name, err := parseQName(attribute.Value, namespaces)
			if err != nil {
				return Attribute{}, err
			}
			declaration.Type = name
		case "default":
			declaration.Default = attribute.Value
			declaration.DefaultSet = true
		case "fixed":
			declaration.Fixed = attribute.Value
			declaration.FixedSet = true
		}
	}
	if declaration.DefaultSet || declaration.FixedSet {
		declaration.ValueNamespaces = cloneNamespaces(namespaces)
	}
	return declaration, nil
}

func parseAttributeBody(
	decoder *xml.Decoder,
	start xml.StartElement,
	namespaces map[string]string,
) (*SimpleType, *Annotation, error) {
	namespaces = namespaceScope(namespaces, start)
	var inline *SimpleType
	var annotation *Annotation
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, nil, err
		}
		switch value := token.(type) {
		case xml.Directive:
			return nil, nil, ErrDTDForbidden
		case xml.StartElement:
			if value.Name == (xml.Name{Space: Namespace, Local: "annotation"}) {
				parsedAnnotation, parseErr := parseAnnotation(decoder, value)
				if parseErr != nil {
					return nil, nil, parseErr
				}
				annotation = &parsedAnnotation
				continue
			}
			if value.Name.Space == Namespace && value.Name.Local == "simpleType" {
				if inline != nil {
					return nil, nil, fmt.Errorf("xsd: attribute has multiple inline types")
				}
				typeDefinition, parseErr := parseSimpleType(decoder, value, namespaces)
				if parseErr != nil {
					return nil, nil, parseErr
				}
				inline = &typeDefinition
				continue
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return nil, nil, err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return inline, annotation, nil
			}
		}
	}
}

func parseSimpleType(
	decoder *xml.Decoder,
	start xml.StartElement,
	namespaces map[string]string,
) (SimpleType, error) {
	if err := validateSchemaAttributes(start, "final", "id", "name"); err != nil {
		return SimpleType{}, err
	}
	namespaces = namespaceScope(namespaces, start)
	var simpleType SimpleType
	for _, attribute := range start.Attr {
		if attribute.Name.Space != "" {
			continue
		}
		switch attribute.Name.Local {
		case "id":
			simpleType.ID = attribute.Value
		case "name":
			simpleType.Name = attribute.Value
		case "final":
			set, err := parseDerivationSet(attribute.Value)
			if err != nil {
				return SimpleType{}, err
			}
			simpleType.Final = set
		}
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return SimpleType{}, err
		}
		switch value := token.(type) {
		case xml.Directive:
			return SimpleType{}, ErrDTDForbidden
		case xml.StartElement:
			if value.Name.Space == Namespace {
				childNamespaces := namespaceScope(namespaces, value)
				switch value.Name.Local {
				case "annotation":
					annotation, parseErr := parseAnnotation(decoder, value)
					if parseErr != nil {
						return SimpleType{}, parseErr
					}
					simpleType.Annotation = &annotation
					continue
				case "restriction":
					if simpleType.Variety != "" {
						return SimpleType{}, fmt.Errorf("xsd: simple type has multiple varieties")
					}
					if attributeErr := validateSchemaAttributes(value, "base", "id"); attributeErr != nil {
						return SimpleType{}, attributeErr
					}
					simpleType.Variety = SimpleRestriction
					simpleType.VarietyID = attributeValue(value, "id")
					for _, attribute := range value.Attr {
						if attribute.Name.Space == "" && attribute.Name.Local == "base" {
							base, parseErr := parseQName(attribute.Value, childNamespaces)
							if parseErr != nil {
								return SimpleType{}, parseErr
							}
							simpleType.Base = base
						}
					}
					if parseErr := parseRestrictionFacets(
						decoder,
						value,
						&simpleType,
						childNamespaces,
					); parseErr != nil {
						return SimpleType{}, parseErr
					}
					continue
				case "list":
					if simpleType.Variety != "" {
						return SimpleType{}, fmt.Errorf("xsd: simple type has multiple varieties")
					}
					if attributeErr := validateSchemaAttributes(value, "id", "itemType"); attributeErr != nil {
						return SimpleType{}, attributeErr
					}
					simpleType.Variety = SimpleList
					simpleType.VarietyID = attributeValue(value, "id")
					for _, attribute := range value.Attr {
						if attribute.Name.Space == "" && attribute.Name.Local == "itemType" {
							itemType, parseErr := parseQName(attribute.Value, childNamespaces)
							if parseErr != nil {
								return SimpleType{}, parseErr
							}
							simpleType.ItemType = itemType
						}
					}
					children, annotation, parseErr := parseInlineSimpleTypes(decoder, value, childNamespaces)
					if parseErr != nil {
						return SimpleType{}, parseErr
					}
					if len(children) > 0 {
						simpleType.InlineItem = &children[0]
						if len(children) > 1 {
							return SimpleType{}, fmt.Errorf("xsd: list has multiple inline item types")
						}
					}
					simpleType.VarietyAnnotation = annotation
					continue
				case "union":
					if simpleType.Variety != "" {
						return SimpleType{}, fmt.Errorf("xsd: simple type has multiple varieties")
					}
					if attributeErr := validateSchemaAttributes(value, "id", "memberTypes"); attributeErr != nil {
						return SimpleType{}, attributeErr
					}
					simpleType.Variety = SimpleUnion
					simpleType.VarietyID = attributeValue(value, "id")
					for _, attribute := range value.Attr {
						if attribute.Name.Space == "" && attribute.Name.Local == "memberTypes" {
							for _, member := range strings.Fields(attribute.Value) {
								memberType, parseErr := parseQName(member, childNamespaces)
								if parseErr != nil {
									return SimpleType{}, parseErr
								}
								simpleType.MemberTypes = append(simpleType.MemberTypes, memberType)
							}
						}
					}
					children, annotation, parseErr := parseInlineSimpleTypes(decoder, value, childNamespaces)
					if parseErr != nil {
						return SimpleType{}, parseErr
					}
					simpleType.InlineMembers = append(simpleType.InlineMembers, children...)
					simpleType.VarietyAnnotation = annotation
					continue
				}
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return SimpleType{}, err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				if simpleType.Variety == "" {
					return SimpleType{}, fmt.Errorf(
						"xsd: simple type must contain a restriction, list, or union",
					)
				}
				return simpleType, nil
			}
		}
	}
}

func parseInlineSimpleTypes(
	decoder *xml.Decoder,
	start xml.StartElement,
	namespaces map[string]string,
) ([]SimpleType, *Annotation, error) {
	namespaces = namespaceScope(namespaces, start)
	var result []SimpleType
	var annotation *Annotation
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, nil, err
		}
		switch value := token.(type) {
		case xml.Directive:
			return nil, nil, ErrDTDForbidden
		case xml.StartElement:
			if value.Name == (xml.Name{Space: Namespace, Local: "annotation"}) {
				parsedAnnotation, parseErr := parseAnnotation(decoder, value)
				if parseErr != nil {
					return nil, nil, parseErr
				}
				annotation = &parsedAnnotation
				continue
			}
			if value.Name == (xml.Name{Space: Namespace, Local: "simpleType"}) {
				typeDefinition, parseErr := parseSimpleType(decoder, value, namespaces)
				if parseErr != nil {
					return nil, nil, parseErr
				}
				result = append(result, typeDefinition)
				continue
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return nil, nil, err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return result, annotation, nil
			}
		}
	}
}

func parseRestrictionFacets(
	decoder *xml.Decoder,
	start xml.StartElement,
	simpleType *SimpleType,
	namespaces map[string]string,
) error {
	namespaces = namespaceScope(namespaces, start)
	facetsStarted := false
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.Directive:
			return ErrDTDForbidden
		case xml.StartElement:
			if value.Name.Space == Namespace {
				if value.Name.Local == "annotation" {
					annotation, parseErr := parseAnnotation(decoder, value)
					if parseErr != nil {
						return parseErr
					}
					simpleType.VarietyAnnotation = &annotation
					continue
				}
				if value.Name.Local == "simpleType" {
					if simpleType.InlineBase != nil {
						return fmt.Errorf("xsd: restriction has multiple inline base types")
					}
					if facetsStarted {
						return fmt.Errorf("xsd: restriction base type must precede facets")
					}
					base, parseErr := parseSimpleType(decoder, value, namespaces)
					if parseErr != nil {
						return parseErr
					}
					simpleType.InlineBase = &base
					continue
				}
				if _, ok := parseFacetKind(value.Name.Local); ok {
					facetsStarted = true
					facet, parseErr := parseFacet(decoder, value, namespaces)
					if parseErr != nil {
						return parseErr
					}
					simpleType.Facets = append(simpleType.Facets, facet)
					continue
				}
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return nil
			}
		}
	}
}

func parseFacet(
	decoder *xml.Decoder,
	start xml.StartElement,
	namespaces map[string]string,
) (Facet, error) {
	if err := validateSchemaAttributes(start, "fixed", "id", "value"); err != nil {
		return Facet{}, err
	}
	kind, ok := parseFacetKind(start.Name.Local)
	if !ok {
		return Facet{}, fmt.Errorf("xsd: unknown facet %q", start.Name.Local)
	}
	facet := Facet{
		Kind:       kind,
		Namespaces: cloneNamespaces(namespaceScope(namespaces, start)),
	}
	for _, attribute := range start.Attr {
		if attribute.Name.Space != "" {
			continue
		}
		switch attribute.Name.Local {
		case "id":
			facet.ID = attribute.Value
		case "value":
			facet.Value = attribute.Value
		case "fixed":
			fixed, err := parseBoolean(attribute.Value)
			if err != nil {
				return Facet{}, err
			}
			facet.Fixed = fixed
		}
	}
	annotation, err := parseAnnotationChildren(decoder, start)
	if err != nil {
		return Facet{}, err
	}
	facet.Annotation = annotation
	return facet, nil
}

func parseFacetKind(local string) (FacetKind, bool) {
	switch FacetKind(local) {
	case FacetLength, FacetMinLength, FacetMaxLength, FacetPattern,
		FacetEnumeration, FacetWhiteSpace, FacetMaxInclusive,
		FacetMaxExclusive, FacetMinInclusive, FacetMinExclusive,
		FacetTotalDigits, FacetFractionDigits:
		return FacetKind(local), true
	default:
		return "", false
	}
}

func parseComplexType(
	decoder *xml.Decoder,
	start xml.StartElement,
	namespaces map[string]string,
) (ComplexType, error) {
	if err := validateSchemaAttributes(
		start,
		"abstract",
		"block",
		"final",
		"id",
		"mixed",
		"name",
	); err != nil {
		return ComplexType{}, err
	}
	namespaces = namespaceScope(namespaces, start)
	var complexType ComplexType
	attributesStarted := false
	for _, attribute := range start.Attr {
		if attribute.Name.Space != "" {
			continue
		}
		switch attribute.Name.Local {
		case "id":
			complexType.ID = attribute.Value
		case "name":
			complexType.Name = attribute.Value
		case "abstract":
			value, err := parseBoolean(attribute.Value)
			if err != nil {
				return ComplexType{}, err
			}
			complexType.Abstract = value
		case "mixed":
			value, err := parseBoolean(attribute.Value)
			if err != nil {
				return ComplexType{}, err
			}
			complexType.Mixed = value
			complexType.MixedSet = true
		case "block":
			set, err := parseDerivationSet(attribute.Value)
			if err != nil {
				return ComplexType{}, err
			}
			complexType.Block = set
		case "final":
			set, err := parseDerivationSet(attribute.Value)
			if err != nil {
				return ComplexType{}, err
			}
			complexType.Final = set
		}
	}

	for {
		token, err := decoder.Token()
		if err != nil {
			return ComplexType{}, err
		}
		switch value := token.(type) {
		case xml.Directive:
			return ComplexType{}, ErrDTDForbidden
		case xml.StartElement:
			if value.Name.Space == Namespace {
				if value.Name.Local == "annotation" {
					annotation, parseErr := parseAnnotation(decoder, value)
					if parseErr != nil {
						return ComplexType{}, parseErr
					}
					complexType.Annotation = &annotation
					continue
				}
				if value.Name.Local == "group" {
					if attributesStarted || complexType.AttributeWildcard != nil {
						return ComplexType{}, fmt.Errorf("xsd: complex type content must precede attributes")
					}
					if complexType.Derivation != "" || complexType.Content != nil {
						return ComplexType{}, fmt.Errorf("xsd: complex type has multiple content models")
					}
					particle, parseErr := parseGroupReferenceParticle(value, namespaces)
					if parseErr != nil {
						return ComplexType{}, parseErr
					}
					annotation, parseErr := parseAnnotationChildren(decoder, value)
					if parseErr != nil {
						return ComplexType{}, parseErr
					}
					particle.Annotation = annotation
					complexType.Content = &ModelGroup{
						Compositor: Sequence,
						Particles:  []Particle{particle},
					}
					continue
				}
				if value.Name.Local == "anyAttribute" {
					if complexType.AttributeWildcard != nil {
						return ComplexType{}, fmt.Errorf("xsd: complex type has multiple attribute wildcards")
					}
					wildcard, parseErr := parseWildcard(value)
					if parseErr != nil {
						return ComplexType{}, parseErr
					}
					annotation, parseErr := parseAnnotationChildren(decoder, value)
					if parseErr != nil {
						return ComplexType{}, parseErr
					}
					wildcard.Annotation = annotation
					complexType.AttributeWildcard = &wildcard
					continue
				}
				if value.Name.Local == "complexContent" || value.Name.Local == "simpleContent" {
					if attributesStarted || complexType.AttributeWildcard != nil {
						return ComplexType{}, fmt.Errorf("xsd: complex type content must precede attributes")
					}
					if complexType.Derivation != "" || complexType.Content != nil {
						return ComplexType{}, fmt.Errorf("xsd: complex type has multiple content models")
					}
					complexType.SimpleContent = value.Name.Local == "simpleContent"
					if parseErr := parseContentDerivation(
						decoder,
						value,
						&complexType,
						namespaces,
					); parseErr != nil {
						return ComplexType{}, parseErr
					}
					continue
				}
				if compositor, ok := parseCompositor(value.Name.Local); ok {
					if attributesStarted || complexType.AttributeWildcard != nil {
						return ComplexType{}, fmt.Errorf("xsd: complex type content must precede attributes")
					}
					if complexType.Derivation != "" || complexType.Content != nil {
						return ComplexType{}, fmt.Errorf("xsd: complex type has multiple content models")
					}
					group, parseErr := parseModelGroup(decoder, value, compositor, namespaces)
					if parseErr != nil {
						return ComplexType{}, parseErr
					}
					if parseErr := setModelGroupOccurrence(&group, value); parseErr != nil {
						return ComplexType{}, parseErr
					}
					complexType.Content = &group
					continue
				}
				if value.Name.Local == "attribute" {
					if complexType.AttributeWildcard != nil {
						return ComplexType{}, fmt.Errorf("xsd: attribute must precede anyAttribute")
					}
					attributesStarted = true
					attribute, parseErr := parseAttributeUse(value, namespaces)
					if parseErr != nil {
						return ComplexType{}, parseErr
					}
					complexType.Attributes = append(complexType.Attributes, attribute)
					inline, annotations, err := parseAttributeBody(decoder, value, namespaces)
					if err != nil {
						return ComplexType{}, err
					}
					complexType.Attributes[len(complexType.Attributes)-1].InlineSimpleType = inline
					complexType.Attributes[len(complexType.Attributes)-1].Annotation = annotations
					continue
				}
				if value.Name.Local == "attributeGroup" {
					if complexType.AttributeWildcard != nil {
						return ComplexType{}, fmt.Errorf("xsd: attributeGroup must precede anyAttribute")
					}
					attributesStarted = true
					reference, parseErr := parseAttributeGroupReference(value, namespaces)
					if parseErr != nil {
						return ComplexType{}, parseErr
					}
					complexType.AttributeGroupRefs = append(
						complexType.AttributeGroupRefs,
						reference,
					)
					annotation, parseErr := parseAnnotationChildren(decoder, value)
					if parseErr != nil {
						return ComplexType{}, parseErr
					}
					complexType.AttributeGroupReferences = append(
						complexType.AttributeGroupReferences,
						AttributeGroupReference{
							ID: attributeValue(value, "id"), Ref: reference, Annotation: annotation,
						},
					)
					continue
				}
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return ComplexType{}, err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return complexType, nil
			}
		}
	}
}

func parseContentDerivation(
	decoder *xml.Decoder,
	start xml.StartElement,
	complexType *ComplexType,
	namespaces map[string]string,
) error {
	allowed := []string{"id"}
	if start.Name.Local == "complexContent" {
		allowed = append(allowed, "mixed")
	}
	if err := validateSchemaAttributes(start, allowed...); err != nil {
		return err
	}
	complexType.ContentID = attributeValue(start, "id")
	namespaces = namespaceScope(namespaces, start)
	for _, attribute := range start.Attr {
		if attribute.Name.Space == "" && attribute.Name.Local == "mixed" {
			mixed, err := parseBoolean(attribute.Value)
			if err != nil {
				return err
			}
			complexType.Mixed = mixed
			complexType.MixedSet = true
		}
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.Directive:
			return ErrDTDForbidden
		case xml.StartElement:
			if value.Name == (xml.Name{Space: Namespace, Local: "annotation"}) {
				annotation, parseErr := parseAnnotation(decoder, value)
				if parseErr != nil {
					return parseErr
				}
				complexType.ContentAnnotation = &annotation
				continue
			}
			if value.Name.Space == Namespace &&
				(value.Name.Local == "extension" || value.Name.Local == "restriction") {
				if complexType.Derivation != "" {
					return fmt.Errorf("xsd: content has multiple derivations")
				}
				childNamespaces := namespaceScope(namespaces, value)
				complexType.Derivation = Derivation(value.Name.Local)
				for _, attribute := range value.Attr {
					if attribute.Name.Space == "" && attribute.Name.Local == "base" {
						base, parseErr := parseQName(attribute.Value, childNamespaces)
						if parseErr != nil {
							return parseErr
						}
						complexType.Base = base
					}
				}
				if err := parseDerivationBody(decoder, value, complexType, childNamespaces); err != nil {
					return err
				}
				continue
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return nil
			}
		}
	}
}

func parseDerivationBody(
	decoder *xml.Decoder,
	start xml.StartElement,
	complexType *ComplexType,
	namespaces map[string]string,
) error {
	if err := validateSchemaAttributes(start, "base", "id"); err != nil {
		return err
	}
	complexType.DerivationID = attributeValue(start, "id")
	namespaces = namespaceScope(namespaces, start)
	attributesStarted := false
	facetsStarted := false
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.Directive:
			return ErrDTDForbidden
		case xml.StartElement:
			if value.Name.Space == Namespace {
				if complexType.SimpleContent &&
					complexType.Derivation == DerivationRestriction {
					if value.Name.Local == "simpleType" {
						if complexType.InlineSimpleType != nil {
							return fmt.Errorf("xsd: restriction has multiple inline simple types")
						}
						if facetsStarted || attributesStarted || complexType.AttributeWildcard != nil {
							return fmt.Errorf("xsd: restriction type must precede facets and attributes")
						}
						typeDefinition, parseErr := parseSimpleType(
							decoder,
							value,
							namespaces,
						)
						if parseErr != nil {
							return parseErr
						}
						complexType.InlineSimpleType = &typeDefinition
						continue
					}
					if _, ok := parseFacetKind(value.Name.Local); ok {
						if attributesStarted || complexType.AttributeWildcard != nil {
							return fmt.Errorf("xsd: restriction facets must precede attributes")
						}
						facetsStarted = true
						facet, parseErr := parseFacet(decoder, value, namespaces)
						if parseErr != nil {
							return parseErr
						}
						complexType.SimpleFacets = append(complexType.SimpleFacets, facet)
						continue
					}
				}
				if value.Name.Local == "annotation" {
					annotation, parseErr := parseAnnotation(decoder, value)
					if parseErr != nil {
						return parseErr
					}
					complexType.DerivationAnnotation = &annotation
					continue
				}
				if value.Name.Local == "group" {
					if attributesStarted || complexType.AttributeWildcard != nil {
						return fmt.Errorf("xsd: derivation content must precede attributes")
					}
					if complexType.Content != nil {
						return fmt.Errorf("xsd: derivation has multiple content models")
					}
					particle, parseErr := parseGroupReferenceParticle(value, namespaces)
					if parseErr != nil {
						return parseErr
					}
					annotation, parseErr := parseAnnotationChildren(decoder, value)
					if parseErr != nil {
						return parseErr
					}
					particle.Annotation = annotation
					complexType.Content = &ModelGroup{
						Compositor: Sequence,
						Particles:  []Particle{particle},
					}
					continue
				}
				if value.Name.Local == "anyAttribute" {
					if complexType.AttributeWildcard != nil {
						return fmt.Errorf("xsd: derivation has multiple attribute wildcards")
					}
					wildcard, parseErr := parseWildcard(value)
					if parseErr != nil {
						return parseErr
					}
					annotation, parseErr := parseAnnotationChildren(decoder, value)
					if parseErr != nil {
						return parseErr
					}
					wildcard.Annotation = annotation
					complexType.AttributeWildcard = &wildcard
					continue
				}
				if compositor, ok := parseCompositor(value.Name.Local); ok {
					if attributesStarted || complexType.AttributeWildcard != nil {
						return fmt.Errorf("xsd: derivation content must precede attributes")
					}
					if complexType.Content != nil {
						return fmt.Errorf("xsd: derivation has multiple content models")
					}
					group, parseErr := parseModelGroup(decoder, value, compositor, namespaces)
					if parseErr != nil {
						return parseErr
					}
					if parseErr := setModelGroupOccurrence(&group, value); parseErr != nil {
						return parseErr
					}
					complexType.Content = &group
					continue
				}
				if value.Name.Local == "attribute" {
					if complexType.AttributeWildcard != nil {
						return fmt.Errorf("xsd: attribute must precede anyAttribute")
					}
					attributesStarted = true
					attribute, parseErr := parseAttributeUse(value, namespaces)
					if parseErr != nil {
						return parseErr
					}
					complexType.Attributes = append(complexType.Attributes, attribute)
					inline, annotations, err := parseAttributeBody(decoder, value, namespaces)
					if err != nil {
						return err
					}
					complexType.Attributes[len(complexType.Attributes)-1].InlineSimpleType = inline
					complexType.Attributes[len(complexType.Attributes)-1].Annotation = annotations
					continue
				}
				if value.Name.Local == "attributeGroup" {
					if complexType.AttributeWildcard != nil {
						return fmt.Errorf("xsd: attributeGroup must precede anyAttribute")
					}
					attributesStarted = true
					reference, parseErr := parseAttributeGroupReference(value, namespaces)
					if parseErr != nil {
						return parseErr
					}
					complexType.AttributeGroupRefs = append(
						complexType.AttributeGroupRefs,
						reference,
					)
					annotation, parseErr := parseAnnotationChildren(decoder, value)
					if parseErr != nil {
						return parseErr
					}
					complexType.AttributeGroupReferences = append(
						complexType.AttributeGroupReferences,
						AttributeGroupReference{
							ID: attributeValue(value, "id"), Ref: reference, Annotation: annotation,
						},
					)
					continue
				}
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return nil
			}
		}
	}
}

func parseCompositor(local string) (Compositor, bool) {
	switch local {
	case "sequence":
		return Sequence, true
	case "choice":
		return Choice, true
	case "all":
		return All, true
	default:
		return "", false
	}
}

func parseModelGroup(
	decoder *xml.Decoder,
	start xml.StartElement,
	compositor Compositor,
	namespaces map[string]string,
) (ModelGroup, error) {
	if err := validateSchemaAttributes(start, "id", "maxOccurs", "minOccurs"); err != nil {
		return ModelGroup{}, err
	}
	namespaces = namespaceScope(namespaces, start)
	group := ModelGroup{ID: attributeValue(start, "id"), Compositor: compositor}
	for {
		token, err := decoder.Token()
		if err != nil {
			return ModelGroup{}, err
		}
		switch value := token.(type) {
		case xml.Directive:
			return ModelGroup{}, ErrDTDForbidden
		case xml.StartElement:
			if value.Name == (xml.Name{Space: Namespace, Local: "annotation"}) {
				parsedAnnotation, parseErr := parseAnnotation(decoder, value)
				if parseErr != nil {
					return ModelGroup{}, parseErr
				}
				group.Annotation = &parsedAnnotation
				continue
			}
			if value.Name.Space == Namespace && value.Name.Local == "element" {
				element, parseErr := parseElement(value, namespaces)
				if parseErr != nil {
					return ModelGroup{}, parseErr
				}
				particle, parseErr := parseOccurrence(value)
				if parseErr != nil {
					return ModelGroup{}, parseErr
				}
				particle.Element = &element
				group.Particles = append(group.Particles, particle)
				if err := parseElementBody(
					decoder,
					value,
					group.Particles[len(group.Particles)-1].Element,
					namespaces,
				); err != nil {
					return ModelGroup{}, err
				}
				continue
			}
			if value.Name.Space == Namespace {
				if value.Name.Local == "any" {
					particle, parseErr := parseOccurrence(value)
					if parseErr != nil {
						return ModelGroup{}, parseErr
					}
					wildcard, parseErr := parseWildcard(value)
					if parseErr != nil {
						return ModelGroup{}, parseErr
					}
					annotation, parseErr := parseAnnotationChildren(decoder, value)
					if parseErr != nil {
						return ModelGroup{}, parseErr
					}
					wildcard.Annotation = annotation
					particle.Wildcard = &wildcard
					group.Particles = append(group.Particles, particle)
					continue
				}
				if value.Name.Local == "group" {
					particle, parseErr := parseGroupReferenceParticle(value, namespaces)
					if parseErr != nil {
						return ModelGroup{}, parseErr
					}
					annotation, parseErr := parseAnnotationChildren(decoder, value)
					if parseErr != nil {
						return ModelGroup{}, parseErr
					}
					particle.Annotation = annotation
					group.Particles = append(group.Particles, particle)
					continue
				}
				if childCompositor, ok := parseCompositor(value.Name.Local); ok {
					particle, parseErr := parseOccurrence(value)
					if parseErr != nil {
						return ModelGroup{}, parseErr
					}
					child, parseErr := parseModelGroup(decoder, value, childCompositor, namespaces)
					if parseErr != nil {
						return ModelGroup{}, parseErr
					}
					particle.Group = &child
					group.Particles = append(group.Particles, particle)
					continue
				}
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return ModelGroup{}, err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return group, nil
			}
		}
	}
}

func setModelGroupOccurrence(group *ModelGroup, start xml.StartElement) error {
	occurrence, err := parseOccurrence(start)
	if err != nil {
		return err
	}
	group.MinOccurs = occurrence.MinOccurs
	group.MaxOccurs = occurrence.MaxOccurs
	group.Unbounded = occurrence.Unbounded
	group.OccursSet = true
	return nil
}

func parseGroupReferenceParticle(
	start xml.StartElement,
	namespaces map[string]string,
) (Particle, error) {
	if err := validateSchemaAttributes(start, "id", "maxOccurs", "minOccurs", "ref"); err != nil {
		return Particle{}, err
	}
	namespaces = namespaceScope(namespaces, start)
	particle, err := parseOccurrence(start)
	if err != nil {
		return Particle{}, err
	}
	particle.ID = attributeValue(start, "id")
	for _, attribute := range start.Attr {
		if attribute.Name.Space == "" && attribute.Name.Local == "ref" {
			reference, parseErr := parseQName(attribute.Value, namespaces)
			if parseErr != nil {
				return Particle{}, parseErr
			}
			particle.GroupRef = reference
		}
	}
	if particle.GroupRef.Local == "" {
		return Particle{}, fmt.Errorf("xsd: group reference has no ref")
	}
	return particle, nil
}

func parseModelGroupDefinition(
	decoder *xml.Decoder,
	start xml.StartElement,
	namespaces map[string]string,
) (ModelGroupDefinition, error) {
	if err := validateSchemaAttributes(start, "id", "name"); err != nil {
		return ModelGroupDefinition{}, err
	}
	namespaces = namespaceScope(namespaces, start)
	definition := ModelGroupDefinition{ID: attributeValue(start, "id")}
	for _, attribute := range start.Attr {
		if attribute.Name.Space == "" && attribute.Name.Local == "name" {
			definition.Name = attribute.Value
		}
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return ModelGroupDefinition{}, err
		}
		switch value := token.(type) {
		case xml.Directive:
			return ModelGroupDefinition{}, ErrDTDForbidden
		case xml.StartElement:
			if value.Name.Space == Namespace {
				if value.Name.Local == "annotation" {
					annotation, parseErr := parseAnnotation(decoder, value)
					if parseErr != nil {
						return ModelGroupDefinition{}, parseErr
					}
					definition.Annotation = &annotation
					continue
				}
				if compositor, ok := parseCompositor(value.Name.Local); ok {
					if definition.Content != nil {
						return ModelGroupDefinition{}, fmt.Errorf(
							"xsd: model group definition has multiple compositors",
						)
					}
					group, parseErr := parseModelGroup(decoder, value, compositor, namespaces)
					if parseErr != nil {
						return ModelGroupDefinition{}, parseErr
					}
					definition.Content = &group
					continue
				}
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return ModelGroupDefinition{}, err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return definition, nil
			}
		}
	}
}

func parseAttributeGroupDefinition(
	decoder *xml.Decoder,
	start xml.StartElement,
	namespaces map[string]string,
) (AttributeGroup, error) {
	if err := validateSchemaAttributes(start, "id", "name"); err != nil {
		return AttributeGroup{}, err
	}
	namespaces = namespaceScope(namespaces, start)
	definition := AttributeGroup{ID: attributeValue(start, "id")}
	for _, attribute := range start.Attr {
		if attribute.Name.Space == "" && attribute.Name.Local == "name" {
			definition.Name = attribute.Value
		}
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return AttributeGroup{}, err
		}
		switch value := token.(type) {
		case xml.Directive:
			return AttributeGroup{}, ErrDTDForbidden
		case xml.StartElement:
			if value.Name == (xml.Name{Space: Namespace, Local: "annotation"}) {
				parsedAnnotation, parseErr := parseAnnotation(decoder, value)
				if parseErr != nil {
					return AttributeGroup{}, parseErr
				}
				definition.Annotation = &parsedAnnotation
				continue
			}
			if value.Name.Space == Namespace && value.Name.Local == "anyAttribute" {
				if definition.Wildcard != nil {
					return AttributeGroup{}, fmt.Errorf(
						"xsd: attribute group has multiple attribute wildcards",
					)
				}
				wildcard, parseErr := parseWildcard(value)
				if parseErr != nil {
					return AttributeGroup{}, parseErr
				}
				annotation, parseErr := parseAnnotationChildren(decoder, value)
				if parseErr != nil {
					return AttributeGroup{}, parseErr
				}
				wildcard.Annotation = annotation
				definition.Wildcard = &wildcard
				continue
			}
			if value.Name.Space == Namespace && value.Name.Local == "attribute" {
				if definition.Wildcard != nil {
					return AttributeGroup{}, fmt.Errorf("xsd: attribute must precede anyAttribute")
				}
				attribute, parseErr := parseAttributeUse(value, namespaces)
				if parseErr != nil {
					return AttributeGroup{}, parseErr
				}
				definition.Attributes = append(definition.Attributes, attribute)
				inline, annotations, err := parseAttributeBody(decoder, value, namespaces)
				if err != nil {
					return AttributeGroup{}, err
				}
				definition.Attributes[len(definition.Attributes)-1].InlineSimpleType = inline
				definition.Attributes[len(definition.Attributes)-1].Annotation = annotations
				continue
			}
			if value.Name.Space == Namespace && value.Name.Local == "attributeGroup" {
				if definition.Wildcard != nil {
					return AttributeGroup{}, fmt.Errorf("xsd: attributeGroup must precede anyAttribute")
				}
				reference, parseErr := parseAttributeGroupReference(value, namespaces)
				if parseErr != nil {
					return AttributeGroup{}, parseErr
				}
				definition.References = append(definition.References, reference)
				annotation, parseErr := parseAnnotationChildren(decoder, value)
				if parseErr != nil {
					return AttributeGroup{}, parseErr
				}
				definition.AttributeGroupReferences = append(
					definition.AttributeGroupReferences,
					AttributeGroupReference{
						ID: attributeValue(value, "id"), Ref: reference, Annotation: annotation,
					},
				)
				continue
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return AttributeGroup{}, err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return definition, nil
			}
		}
	}
}

func parseWildcard(start xml.StartElement) (Wildcard, error) {
	if err := validateSchemaAttributes(
		start,
		"id",
		"maxOccurs",
		"minOccurs",
		"namespace",
		"processContents",
	); err != nil {
		return Wildcard{}, err
	}
	wildcard := Wildcard{
		ID:              attributeValue(start, "id"),
		Namespaces:      []string{"##any"},
		ProcessContents: ProcessStrict,
	}
	for _, attribute := range start.Attr {
		if attribute.Name.Space != "" {
			continue
		}
		switch attribute.Name.Local {
		case "namespace":
			wildcard.Namespaces = strings.Fields(attribute.Value)
			if len(wildcard.Namespaces) == 0 {
				return Wildcard{}, fmt.Errorf("xsd: wildcard namespace is empty")
			}
		case "processContents":
			wildcard.ProcessContents = ProcessContents(attribute.Value)
			if wildcard.ProcessContents != ProcessStrict &&
				wildcard.ProcessContents != ProcessLax &&
				wildcard.ProcessContents != ProcessSkip {
				return Wildcard{}, fmt.Errorf(
					"xsd: invalid wildcard processContents %q",
					attribute.Value,
				)
			}
		}
	}
	return wildcard, nil
}

func parseAttributeGroupReference(
	start xml.StartElement,
	namespaces map[string]string,
) (QName, error) {
	if err := validateSchemaAttributes(start, "id", "ref"); err != nil {
		return QName{}, err
	}
	namespaces = namespaceScope(namespaces, start)
	for _, attribute := range start.Attr {
		if attribute.Name.Space == "" && attribute.Name.Local == "ref" {
			return parseQName(attribute.Value, namespaces)
		}
	}
	return QName{}, fmt.Errorf("xsd: attributeGroup reference has no ref")
}

func parseOccurrence(start xml.StartElement) (Particle, error) {
	particle := Particle{MinOccurs: 1, MaxOccurs: 1}
	for _, attribute := range start.Attr {
		if attribute.Name.Space != "" {
			continue
		}
		switch attribute.Name.Local {
		case "minOccurs":
			value, err := strconv.ParseUint(attribute.Value, 10, 64)
			if err != nil {
				return Particle{}, fmt.Errorf("xsd: invalid minOccurs %q", attribute.Value)
			}
			particle.MinOccurs = value
		case "maxOccurs":
			if attribute.Value == "unbounded" {
				particle.Unbounded = true
				particle.MaxOccurs = 0
				continue
			}
			value, err := strconv.ParseUint(attribute.Value, 10, 64)
			if err != nil {
				return Particle{}, fmt.Errorf("xsd: invalid maxOccurs %q", attribute.Value)
			}
			particle.MaxOccurs = value
		}
	}
	if !particle.Unbounded && particle.MinOccurs > particle.MaxOccurs {
		return Particle{}, fmt.Errorf(
			"xsd: minOccurs %d exceeds maxOccurs %d",
			particle.MinOccurs,
			particle.MaxOccurs,
		)
	}
	return particle, nil
}

func parseAttributeUse(
	start xml.StartElement,
	namespaces map[string]string,
) (AttributeUse, error) {
	if err := validateSchemaAttributes(
		start,
		"default",
		"fixed",
		"form",
		"id",
		"name",
		"ref",
		"type",
		"use",
	); err != nil {
		return AttributeUse{}, err
	}
	namespaces = namespaceScope(namespaces, start)
	use := AttributeUse{ID: attributeValue(start, "id"), Use: AttributeOptional}
	for _, attribute := range start.Attr {
		if attribute.Name.Space != "" {
			continue
		}
		switch attribute.Name.Local {
		case "name":
			use.Name = attribute.Value
		case "ref":
			name, err := parseQName(attribute.Value, namespaces)
			if err != nil {
				return AttributeUse{}, err
			}
			use.Ref = name
		case "type":
			name, err := parseQName(attribute.Value, namespaces)
			if err != nil {
				return AttributeUse{}, err
			}
			use.Type = name
		case "form":
			form, err := parseForm(attribute.Value)
			if err != nil {
				return AttributeUse{}, err
			}
			use.Form = form
		case "use":
			use.Use = AttributeUseKind(attribute.Value)
			if use.Use != AttributeOptional && use.Use != AttributeRequired &&
				use.Use != AttributeProhibited {
				return AttributeUse{}, fmt.Errorf("xsd: invalid attribute use %q", attribute.Value)
			}
		case "default":
			use.Default = attribute.Value
			use.DefaultSet = true
		case "fixed":
			use.Fixed = attribute.Value
			use.FixedSet = true
		}
	}
	if use.DefaultSet || use.FixedSet {
		use.ValueNamespaces = cloneNamespaces(namespaces)
	}
	return use, nil
}

func parseQName(value string, namespaces map[string]string) (QName, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return QName{}, fmt.Errorf("xsd: invalid QName %q", value)
	}
	prefix := ""
	local := value
	if colon := strings.IndexByte(value, ':'); colon >= 0 {
		if colon == 0 || colon == len(value)-1 || strings.IndexByte(value[colon+1:], ':') >= 0 {
			return QName{}, fmt.Errorf("xsd: invalid QName %q", value)
		}
		prefix = value[:colon]
		local = value[colon+1:]
	}
	namespace, ok := namespaces[prefix]
	if prefix != "" && !ok {
		return QName{}, fmt.Errorf("xsd: undeclared QName prefix %q", prefix)
	}
	return QName{Namespace: namespace, Local: local}, nil
}

func parseBoolean(value string) (bool, error) {
	switch value {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("xsd: invalid boolean %q", value)
	}
}

func referenceKind(local string) (ReferenceKind, bool) {
	switch local {
	case "include":
		return ReferenceInclude, true
	case "import":
		return ReferenceImport, true
	case "redefine":
		return ReferenceRedefine, true
	default:
		return "", false
	}
}

func parseSchemaReference(
	kind ReferenceKind,
	documentBase string,
	start xml.StartElement,
) (SchemaReference, error) {
	if err := validateSchemaAttributes(start, "id", "namespace", "schemaLocation"); err != nil {
		return SchemaReference{}, err
	}
	reference := SchemaReference{ID: attributeValue(start, "id"), Kind: kind}
	baseURI := documentBase
	for _, attribute := range start.Attr {
		if attribute.Name.Space == "" && attribute.Name.Local == "namespace" {
			reference.Namespace = attribute.Value
			continue
		}
		if attribute.Name.Space == "" && attribute.Name.Local == "schemaLocation" {
			reference.Location = attribute.Value
			continue
		}
		if attribute.Name.Space == "http://www.w3.org/XML/1998/namespace" && attribute.Name.Local == "base" {
			resolved, err := resolveURI(documentBase, attribute.Value)
			if err != nil {
				return SchemaReference{}, err
			}
			baseURI = resolved
		}
	}
	if (kind == ReferenceInclude || kind == ReferenceRedefine) && reference.Location == "" {
		return SchemaReference{}, fmt.Errorf("xsd: %s requires schemaLocation", kind)
	}

	if reference.Location != "" {
		resolved, err := resolveURI(baseURI, reference.Location)
		if err != nil {
			return SchemaReference{}, err
		}
		reference.URI = resolved
	}

	return reference, nil
}

func resolveURI(base, reference string) (string, error) {
	referenceURI, err := url.Parse(reference)
	if err != nil {
		return "", fmt.Errorf("xsd: invalid URI reference %q: %w", reference, err)
	}
	if base == "" {
		return referenceURI.String(), nil
	}
	baseURI, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("xsd: invalid base URI %q: %w", base, err)
	}

	return baseURI.ResolveReference(referenceURI).String(), nil
}

func parseAnnotationChildren(
	decoder *xml.Decoder,
	start xml.StartElement,
) (*Annotation, error) {
	var annotation *Annotation
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.Directive:
			return nil, ErrDTDForbidden
		case xml.StartElement:
			if value.Name == (xml.Name{Space: Namespace, Local: "annotation"}) {
				parsedAnnotation, parseErr := parseAnnotation(decoder, value)
				if parseErr != nil {
					return nil, parseErr
				}
				annotation = &parsedAnnotation
				continue
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return nil, err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return annotation, nil
			}
		}
	}
}

func parseAnnotation(decoder *xml.Decoder, start xml.StartElement) (Annotation, error) {
	if err := validateSchemaAttributes(start, "id"); err != nil {
		return Annotation{}, err
	}
	annotation := Annotation{}
	for _, attribute := range start.Attr {
		if attribute.Name.Space == "" && attribute.Name.Local == "id" {
			annotation.ID = attribute.Value
		}
	}

	for {
		token, err := decoder.Token()
		if err != nil {
			return Annotation{}, err
		}
		switch value := token.(type) {
		case xml.Directive:
			return Annotation{}, ErrDTDForbidden
		case xml.StartElement:
			if value.Name.Space == Namespace && value.Name.Local == "documentation" {
				documentation, decodeErr := decodeDocumentation(decoder, value)
				if decodeErr != nil {
					return Annotation{}, decodeErr
				}
				annotation.Documentation = append(annotation.Documentation, documentation)
				continue
			}
			if value.Name.Space == Namespace && value.Name.Local == "appinfo" {
				appInfo, decodeErr := decodeAppInfo(decoder, value)
				if decodeErr != nil {
					return Annotation{}, decodeErr
				}
				annotation.AppInformation = append(annotation.AppInformation, appInfo)
				continue
			}
			if err := skipUnsupportedElement(decoder, value); err != nil {
				return Annotation{}, err
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return annotation, nil
			}
		}
	}
}

func decodeAppInfo(decoder *xml.Decoder, start xml.StartElement) (AppInfo, error) {
	if err := validateSchemaAttributes(start, "id", "source"); err != nil {
		return AppInfo{}, err
	}
	var content struct {
		ID     string `xml:"id,attr"`
		Source string `xml:"source,attr"`
		Inner  string `xml:",innerxml"`
	}
	if err := decoder.DecodeElement(&content, &start); err != nil {
		return AppInfo{}, err
	}
	return AppInfo{ID: content.ID, Source: content.Source, Content: content.Inner}, nil
}

func decodeDocumentation(decoder *xml.Decoder, start xml.StartElement) (Documentation, error) {
	if err := validateSchemaAttributes(start, "id", "source"); err != nil {
		return Documentation{}, err
	}
	var documentation Documentation
	for _, attribute := range start.Attr {
		if attribute.Name.Space == "" && attribute.Name.Local == "id" {
			documentation.ID = attribute.Value
			continue
		}
		if attribute.Name.Space == "" && attribute.Name.Local == "source" {
			documentation.Source = attribute.Value
			continue
		}
		if attribute.Name.Space == "http://www.w3.org/XML/1998/namespace" && attribute.Name.Local == "lang" {
			documentation.Language = attribute.Value
		}
	}

	var content struct {
		Inner string `xml:",innerxml"`
	}
	if err := decoder.DecodeElement(&content, &start); err != nil {
		return Documentation{}, err
	}
	documentation.Markup = content.Inner
	// DecodeElement has already proven that Inner is a well-formed fragment.
	text, _ := documentationText(content.Inner)
	documentation.Content = text

	return documentation, nil
}

func documentationText(markup string) (string, error) {
	decoder := xml.NewDecoder(strings.NewReader("<root>" + markup + "</root>"))
	var content strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return strings.TrimSpace(content.String()), nil
			}
			return "", err
		}
		switch value := token.(type) {
		case xml.Directive:
			return "", ErrDTDForbidden
		case xml.CharData:
			content.Write(value)
		}
	}
}

func parseForm(value string) (Form, error) {
	switch Form(value) {
	case FormQualified:
		return FormQualified, nil
	case FormUnqualified:
		return FormUnqualified, nil
	default:
		return "", fmt.Errorf("xsd: invalid form value %q", value)
	}
}

func parseDerivationSet(value string) (DerivationSet, error) {
	if value == "#all" {
		return DerivationSet{all: true}, nil
	}

	set := DerivationSet{values: make(map[Derivation]struct{})}
	for _, field := range strings.Fields(value) {
		derivation := Derivation(field)
		switch derivation {
		case DerivationExtension, DerivationRestriction, DerivationSubstitution,
			DerivationList, DerivationUnion:
			set.values[derivation] = struct{}{}
		default:
			return DerivationSet{}, fmt.Errorf("xsd: invalid derivation %q", field)
		}
	}

	return set, nil
}

func located(decoder *xml.Decoder, systemID string, err error) error {
	line, column := decoder.InputPos()
	return &ParseError{
		Location: Location{
			SystemID: systemID,
			Line:     line,
			Column:   column,
			Offset:   decoder.InputOffset(),
		},
		Err: err,
	}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
