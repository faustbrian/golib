package xsd_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestParseErrorFormatsLocationsAndUnwraps(t *testing.T) {
	t.Parallel()

	cause := errors.New("broken")
	withoutSystemID := &xsd.ParseError{
		Location: xsd.Location{Line: 2, Column: 3},
		Err:      cause,
	}
	withSystemID := &xsd.ParseError{
		Location: xsd.Location{SystemID: "schema.xsd", Line: 4, Column: 5},
		Err:      cause,
	}
	if withoutSystemID.Error() != "xsd: line 2, column 3: broken" ||
		withSystemID.Error() != "xsd: schema.xsd:4:5: broken" ||
		!errors.Is(withSystemID, cause) {
		t.Fatalf("ParseError values = %q, %q", withoutSystemID, withSystemID)
	}
}

func TestParseRejectsDTDWithoutResolvingEntities(t *testing.T) {
	t.Parallel()

	const schema = `<!DOCTYPE schema SYSTEM "https://attacker.invalid/schema.dtd">
<schema xmlns="http://www.w3.org/2001/XMLSchema"/>`

	_, err := xsd.Parse(context.Background(), []byte(schema), xsd.ParseOptions{})
	if !errors.Is(err, xsd.ErrDTDForbidden) {
		t.Fatalf("Parse() error = %v, want ErrDTDForbidden", err)
	}
}

func TestParseCapturesSchemaDocumentPolicies(t *testing.T) {
	t.Parallel()

	const schema = `<schema xmlns="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:example:orders" elementFormDefault="qualified"
 attributeFormDefault="unqualified" blockDefault="extension restriction"
 finalDefault="#all" version="1.0" xml:lang="en">
 <annotation><documentation>Order schema</documentation></annotation>
</schema>`

	document, err := xsd.Parse(
		context.Background(),
		[]byte(schema),
		xsd.ParseOptions{SystemID: "https://example.test/orders.xsd"},
	)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if document.TargetNamespace != "urn:example:orders" {
		t.Fatalf("TargetNamespace = %q", document.TargetNamespace)
	}
	if document.ElementFormDefault != xsd.FormQualified {
		t.Fatalf("ElementFormDefault = %q", document.ElementFormDefault)
	}
	if document.AttributeFormDefault != xsd.FormUnqualified {
		t.Fatalf("AttributeFormDefault = %q", document.AttributeFormDefault)
	}
	if !document.BlockDefault.Contains(xsd.DerivationExtension) ||
		!document.BlockDefault.Contains(xsd.DerivationRestriction) {
		t.Fatalf("BlockDefault = %v", document.BlockDefault)
	}
	if !document.FinalDefault.All() {
		t.Fatalf("FinalDefault = %v, want #all", document.FinalDefault)
	}
	if document.Version != "1.0" || document.Language != "en" {
		t.Fatalf("version/language = %q/%q", document.Version, document.Language)
	}
	if document.SystemID != "https://example.test/orders.xsd" {
		t.Fatalf("SystemID = %q", document.SystemID)
	}
	if len(document.Annotations) != 1 ||
		len(document.Annotations[0].Documentation) != 1 ||
		document.Annotations[0].Documentation[0].Content != "Order schema" {
		t.Fatalf("Annotations = %#v", document.Annotations)
	}
}

func TestParseRejectsNonSchemaRoot(t *testing.T) {
	t.Parallel()

	_, err := xsd.Parse(
		context.Background(),
		[]byte(`<schema xmlns="urn:not-xsd"/>`),
		xsd.ParseOptions{},
	)
	if !errors.Is(err, xsd.ErrNotSchema) {
		t.Fatalf("Parse() error = %v, want ErrNotSchema", err)
	}
}

func TestParseRejectsUnknownSchemaComponents(t *testing.T) {
	t.Parallel()

	for _, child := range []string{
		`<xs:unknown/>`,
		`<extension xmlns="urn:foreign"/>`,
	} {
		_, err := xsd.Parse(
			context.Background(),
			[]byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">`+
				child+`</xs:schema>`),
			xsd.ParseOptions{},
		)
		if err == nil {
			t.Fatalf("Parse() accepted unsupported child %s", child)
		}
	}
}

func TestParseRejectsDeclarationsInsideRedefine(t *testing.T) {
	t.Parallel()

	for _, declaration := range []string{
		`<xs:element name="value"/>`,
		`<xs:attribute name="value"/>`,
	} {
		_, err := xsd.Parse(
			context.Background(),
			[]byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">`+
				`<xs:redefine schemaLocation="base.xsd">`+declaration+
				`</xs:redefine></xs:schema>`),
			xsd.ParseOptions{},
		)
		if err == nil {
			t.Fatalf("Parse() accepted %s inside redefine", declaration)
		}
	}
}

func TestParseEnforcesSchemaCompositionGrammar(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">` +
			`<xs:element name="value"/><xs:include schemaLocation="base.xsd"/>` +
			`</xs:schema>`,
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:include/></xs:schema>`,
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:redefine/></xs:schema>`,
	} {
		if _, err := xsd.Parse(
			context.Background(),
			[]byte(source),
			xsd.ParseOptions{},
		); err == nil {
			t.Fatalf("Parse() accepted invalid composition grammar in %s", source)
		}
	}
}

func TestParseRejectsDuplicateExclusiveChildren(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`<xs:simpleType name="Value"><xs:restriction base="xs:string"/><xs:list itemType="xs:string"/></xs:simpleType>`,
		`<xs:simpleType name="Value"><xs:list itemType="xs:string"/><xs:restriction base="xs:string"/></xs:simpleType>`,
		`<xs:simpleType name="Value"><xs:list itemType="xs:string"/><xs:union memberTypes="xs:string"/></xs:simpleType>`,
		`<xs:attribute name="value"><xs:simpleType/><xs:simpleType/></xs:attribute>`,
		`<xs:element name="value"><xs:simpleType/><xs:simpleType/></xs:element>`,
		`<xs:element name="value"><xs:simpleType/><xs:complexType/></xs:element>`,
		`<xs:element name="value"><xs:key name="key"><xs:selector xpath="."/><xs:selector xpath="."/><xs:field xpath="."/></xs:key></xs:element>`,
		`<xs:complexType name="Value"><xs:sequence/><xs:choice/></xs:complexType>`,
		`<xs:complexType name="Value"><xs:sequence/><xs:group ref="Items"/></xs:complexType>`,
		`<xs:complexType name="Value"><xs:sequence/><xs:complexContent><xs:extension base="xs:anyType"/></xs:complexContent></xs:complexType>`,
		`<xs:complexType name="Value"><xs:complexContent><xs:extension base="xs:anyType"/><xs:restriction base="xs:anyType"/></xs:complexContent></xs:complexType>`,
		`<xs:complexType name="Value"><xs:complexContent><xs:extension base="xs:anyType"><xs:sequence/><xs:choice/></xs:extension></xs:complexContent></xs:complexType>`,
		`<xs:complexType name="Value"><xs:complexContent><xs:extension base="xs:anyType"><xs:sequence/><xs:group ref="Items"/></xs:extension></xs:complexContent></xs:complexType>`,
		`<xs:complexType name="Value"><xs:complexContent><xs:extension base="xs:anyType"><xs:anyAttribute/><xs:anyAttribute/></xs:extension></xs:complexContent></xs:complexType>`,
		`<xs:complexType name="Value"><xs:simpleContent><xs:restriction base="xs:string"><xs:simpleType/><xs:simpleType/></xs:restriction></xs:simpleContent></xs:complexType>`,
		`<xs:complexType name="Value"><xs:anyAttribute/><xs:anyAttribute/></xs:complexType>`,
		`<xs:attributeGroup name="Values"><xs:anyAttribute/><xs:anyAttribute/></xs:attributeGroup>`,
	} {
		source := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">` +
			body + `</xs:schema>`
		if _, err := xsd.Parse(
			context.Background(),
			[]byte(source),
			xsd.ParseOptions{},
		); err == nil {
			t.Fatalf("Parse() accepted duplicate exclusive children in %s", body)
		}
	}
}

func TestParseRejectsOutOfOrderChildren(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`<xs:element name="value"><xs:key name="key"><xs:selector xpath="."/><xs:field xpath="."/></xs:key><xs:simpleType/></xs:element>`,
		`<xs:element name="value"><xs:key name="key"><xs:selector xpath="."/><xs:field xpath="."/></xs:key><xs:complexType/></xs:element>`,
		`<xs:element name="value"><xs:key name="key"><xs:field xpath="."/><xs:selector xpath="."/></xs:key></xs:element>`,
		`<xs:simpleType name="Value"><xs:restriction base="xs:string"><xs:length value="1"/><xs:simpleType/></xs:restriction></xs:simpleType>`,
		`<xs:simpleType name="Value"><xs:restriction><xs:simpleType/><xs:simpleType/></xs:restriction></xs:simpleType>`,
		`<xs:complexType name="Value"><xs:attribute name="code"/><xs:sequence/></xs:complexType>`,
		`<xs:complexType name="Value"><xs:attribute name="code"/><xs:group ref="Items"/></xs:complexType>`,
		`<xs:complexType name="Value"><xs:attribute name="code"/><xs:complexContent><xs:extension base="xs:anyType"/></xs:complexContent></xs:complexType>`,
		`<xs:complexType name="Value"><xs:anyAttribute/><xs:attribute name="code"/></xs:complexType>`,
		`<xs:complexType name="Value"><xs:anyAttribute/><xs:attributeGroup ref="Attributes"/></xs:complexType>`,
		`<xs:complexType name="Value"><xs:complexContent><xs:extension base="xs:anyType"><xs:attribute name="code"/><xs:sequence/></xs:extension></xs:complexContent></xs:complexType>`,
		`<xs:complexType name="Value"><xs:complexContent><xs:extension base="xs:anyType"><xs:attribute name="code"/><xs:group ref="Items"/></xs:extension></xs:complexContent></xs:complexType>`,
		`<xs:complexType name="Value"><xs:complexContent><xs:extension base="xs:anyType"><xs:anyAttribute/><xs:attribute name="code"/></xs:extension></xs:complexContent></xs:complexType>`,
		`<xs:complexType name="Value"><xs:complexContent><xs:extension base="xs:anyType"><xs:anyAttribute/><xs:attributeGroup ref="Attributes"/></xs:extension></xs:complexContent></xs:complexType>`,
		`<xs:complexType name="Value"><xs:simpleContent><xs:restriction base="xs:string"><xs:length value="1"/><xs:simpleType/></xs:restriction></xs:simpleContent></xs:complexType>`,
		`<xs:complexType name="Value"><xs:simpleContent><xs:restriction base="xs:string"><xs:attribute name="code"/><xs:length value="1"/></xs:restriction></xs:simpleContent></xs:complexType>`,
		`<xs:attributeGroup name="Values"><xs:anyAttribute/><xs:attribute name="code"/></xs:attributeGroup>`,
		`<xs:attributeGroup name="Values"><xs:anyAttribute/><xs:attributeGroup ref="Attributes"/></xs:attributeGroup>`,
	} {
		source := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">` +
			body + `</xs:schema>`
		if _, err := xsd.Parse(
			context.Background(),
			[]byte(source),
			xsd.ParseOptions{},
		); err == nil {
			t.Fatalf("Parse() accepted out-of-order children in %s", body)
		}
	}
}

func TestParseRejectsUnknownSchemaAttributes(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" unknown="x"/>`,
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">` +
			`<xs:element name="value" unknown="x"/></xs:schema>`,
	} {
		if _, err := xsd.Parse(
			context.Background(),
			[]byte(source),
			xsd.ParseOptions{},
		); err == nil {
			t.Fatalf("Parse() accepted unknown attribute in %s", source)
		}
	}
}

func TestParseRejectsInvalidAndDuplicateSchemaIDs(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" id="not valid"/>`,
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">` +
			`<xs:element id="same" name="one"/><xs:element id="same" name="two"/>` +
			`</xs:schema>`,
	} {
		if _, err := xsd.Parse(
			context.Background(),
			[]byte(source),
			xsd.ParseOptions{},
		); err == nil {
			t.Fatalf("Parse() accepted invalid schema IDs in %s", source)
		}
	}
}

func TestParseAndMarshalPreserveAppInfo(t *testing.T) {
	t.Parallel()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tool="urn:tool">
 <xs:annotation id="metadata"><xs:appinfo source="urn:source"><tool:binding enabled="true"/></xs:appinfo></xs:annotation>
</xs:schema>`), xsd.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Annotations) != 1 || len(document.Annotations[0].AppInformation) != 1 ||
		document.Annotations[0].AppInformation[0].Source != "urn:source" {
		t.Fatalf("Annotations = %#v", document.Annotations)
	}
	encoded, err := xsd.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := xsd.Parse(context.Background(), encoded, xsd.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v\n%s", err, encoded)
	}
	if len(roundTrip.Annotations[0].AppInformation) != 1 ||
		roundTrip.Annotations[0].AppInformation[0].Content == "" {
		t.Fatalf("AppInformation = %#v\n%s", roundTrip.Annotations[0].AppInformation, encoded)
	}
}

func TestParseAndMarshalPreserveDocumentationMarkup(t *testing.T) {
	t.Parallel()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:doc="urn:doc">
 <xs:annotation><xs:documentation>Read <doc:em>carefully</doc:em>.</xs:documentation></xs:annotation>
</xs:schema>`), xsd.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	documentation := document.Annotations[0].Documentation[0]
	if documentation.Content != "Read carefully." ||
		!strings.Contains(documentation.Markup, "<doc:em>carefully</doc:em>") {
		t.Fatalf("Documentation = %#v", documentation)
	}
	encoded, err := xsd.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := xsd.Parse(context.Background(), encoded, xsd.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v\n%s", err, encoded)
	}
	if roundTrip.Annotations[0].Documentation[0].Markup != documentation.Markup {
		t.Fatalf("Documentation.Markup = %q, want %q", roundTrip.Annotations[0].Documentation[0].Markup, documentation.Markup)
	}
}

func TestMarshalRejectsUnsafeAppInfoContent(t *testing.T) {
	t.Parallel()

	for _, content := range []string{`<broken>`, `<!DOCTYPE unsafe>`} {
		_, err := xsd.Marshal(&xsd.Document{Annotations: []xsd.Annotation{{
			AppInformation: []xsd.AppInfo{{Content: content}},
		}}})
		if err == nil {
			t.Fatalf("Marshal(%q) succeeded", content)
		}
	}
}

func TestMarshalRejectsUnsafeDocumentationContent(t *testing.T) {
	t.Parallel()

	for _, content := range []string{`<broken>`, `<!DOCTYPE unsafe>`} {
		_, err := xsd.Marshal(&xsd.Document{Annotations: []xsd.Annotation{{
			Documentation: []xsd.Documentation{{Markup: content}},
		}}})
		if err == nil {
			t.Fatalf("Marshal(%q) succeeded", content)
		}
	}
}

func TestParseRejectsMisplacedOrRepeatedComponentAnnotations(t *testing.T) {
	t.Parallel()

	for _, schema := range []string{
		`<schema xmlns="http://www.w3.org/2001/XMLSchema">
 <attributeGroup name="G"><annotation/><annotation/></attributeGroup>
</schema>`,
		`<schema xmlns="http://www.w3.org/2001/XMLSchema">
 <attributeGroup name="G"><attribute name="a"/><annotation/></attributeGroup>
</schema>`,
	} {
		if _, err := xsd.Parse(context.Background(), []byte(schema), xsd.ParseOptions{}); err == nil {
			t.Fatalf("Parse(%s) error = nil, want invalid annotation placement", schema)
		}
	}
}

func TestParseRejectsMultipleModelGroupCompositors(t *testing.T) {
	t.Parallel()

	_, err := xsd.Parse(context.Background(), []byte(
		`<schema xmlns="http://www.w3.org/2001/XMLSchema">
 <group name="G"><sequence/><choice/></group>
</schema>`), xsd.ParseOptions{})
	if err == nil {
		t.Fatal("Parse() error = nil, want multiple compositor rejection")
	}
}

func TestParseValidatesNotationDeclarationsAndReferences(t *testing.T) {
	t.Parallel()

	for _, schema := range []string{
		`<schema xmlns="http://www.w3.org/2001/XMLSchema"><notation name="png"/></schema>`,
		`<schema xmlns="http://www.w3.org/2001/XMLSchema"><notation public="image/png"/></schema>`,
		`<schema xmlns="http://www.w3.org/2001/XMLSchema">
 <notation name="png" public="image/png"/>
 <simpleType name="Image"><restriction base="NOTATION"><enumeration value="jpeg"/></restriction></simpleType>
</schema>`,
	} {
		if _, err := xsd.Parse(context.Background(), []byte(schema), xsd.ParseOptions{}); err == nil {
			t.Fatalf("Parse(%s) error = nil, want invalid notation", schema)
		}
	}
}

func TestParseAndMarshalPreserveNotationDeclarations(t *testing.T) {
	t.Parallel()

	source := []byte(`<schema xmlns="http://www.w3.org/2001/XMLSchema">
 <notation id="png-id" name="png" public="image/png" system="png.dat">
  <annotation><documentation>Portable Network Graphics</documentation></annotation>
 </notation>
</schema>`)
	document, err := xsd.Parse(context.Background(), source, xsd.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Notations) != 1 || document.Notations[0].Name != "png" ||
		document.Notations[0].ID != "png-id" || document.Notations[0].Public != "image/png" ||
		document.Notations[0].System != "png.dat" || document.Notations[0].Annotation == nil {
		t.Fatalf("Notations = %#v", document.Notations)
	}
	encoded, err := xsd.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := xsd.Parse(context.Background(), encoded, xsd.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v\n%s", err, encoded)
	}
	if len(roundTrip.Notations) != 1 || roundTrip.Notations[0].Annotation == nil ||
		roundTrip.Notations[0].Annotation.Documentation[0].Content != "Portable Network Graphics" {
		t.Fatalf("Notations = %#v\n%s", roundTrip.Notations, encoded)
	}
}

func TestParseAndMarshalPreserveComponentAnnotations(t *testing.T) {
	t.Parallel()

	source := []byte(`<schema xmlns="http://www.w3.org/2001/XMLSchema">
 <include schemaLocation="included.xsd"><annotation><documentation>include</documentation></annotation></include>
 <simpleType name="Code"><annotation><documentation>simple</documentation></annotation><restriction base="string"><annotation><documentation>restriction</documentation></annotation><minLength value="1"><annotation><documentation>facet</documentation></annotation></minLength></restriction></simpleType>
 <complexType name="Record"><annotation><documentation>complex</documentation></annotation><sequence><annotation><documentation>sequence</documentation></annotation><any><annotation><documentation>wildcard</documentation></annotation></any><choice><annotation><documentation>choice</documentation></annotation></choice><group ref="Items"><annotation><documentation>group reference</documentation></annotation></group></sequence><attributeGroup ref="Metadata"><annotation><documentation>attribute group reference</documentation></annotation></attributeGroup><anyAttribute><annotation><documentation>attribute wildcard</documentation></annotation></anyAttribute></complexType>
 <complexType name="Text"><simpleContent><annotation><documentation>content</documentation></annotation><extension base="string"><annotation><documentation>derivation</documentation></annotation></extension></simpleContent></complexType>
 <group name="Items"><annotation><documentation>group</documentation></annotation><sequence/></group>
 <attributeGroup name="Metadata"><annotation><documentation>attribute group</documentation></annotation></attributeGroup>
 <attribute name="code" type="string"><annotation><documentation>attribute</documentation></annotation></attribute>
 <element name="root" type="string"><annotation><documentation>element</documentation></annotation>
  <unique name="identity"><annotation><documentation>identity</documentation></annotation><selector xpath="."><annotation><documentation>selector</documentation></annotation></selector><field xpath="."><annotation><documentation>field</documentation></annotation></field></unique>
 </element>
</schema>`)
	document, err := xsd.Parse(context.Background(), source, xsd.ParseOptions{
		SystemID: "https://example.test/schema.xsd",
	})
	if err != nil {
		t.Fatal(err)
	}
	annotations := []*xsd.Annotation{
		document.References[0].Annotation,
		document.SimpleTypes[0].Annotation,
		document.SimpleTypes[0].VarietyAnnotation,
		document.SimpleTypes[0].Facets[0].Annotation,
		document.ComplexTypes[0].Annotation,
		document.ComplexTypes[0].Content.Annotation,
		document.ComplexTypes[0].Content.Particles[0].Wildcard.Annotation,
		document.ComplexTypes[0].Content.Particles[1].Group.Annotation,
		document.ComplexTypes[0].Content.Particles[2].Annotation,
		document.ComplexTypes[0].AttributeGroupReferences[0].Annotation,
		document.ComplexTypes[0].AttributeWildcard.Annotation,
		document.ComplexTypes[1].ContentAnnotation,
		document.ComplexTypes[1].DerivationAnnotation,
		document.ModelGroups[0].Annotation,
		document.AttributeGroups[0].Annotation,
		document.Attributes[0].Annotation,
		document.Elements[0].Annotation,
		document.Elements[0].IdentityConstraints[0].Annotation,
		document.Elements[0].IdentityConstraints[0].SelectorAnnotation,
		document.Elements[0].IdentityConstraints[0].FieldAnnotations[0],
	}
	for index, annotation := range annotations {
		if annotation == nil || len(annotation.Documentation) != 1 {
			t.Fatalf("annotations[%d] = %#v", index, annotation)
		}
	}
	encoded, err := xsd.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := xsd.Parse(context.Background(), encoded, xsd.ParseOptions{
		SystemID: "https://example.test/schema.xsd",
	})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v\n%s", err, encoded)
	}
	if roundTrip.References[0].Annotation == nil ||
		roundTrip.SimpleTypes[0].Annotation == nil ||
		roundTrip.SimpleTypes[0].VarietyAnnotation == nil ||
		roundTrip.SimpleTypes[0].Facets[0].Annotation == nil ||
		roundTrip.ComplexTypes[0].Annotation == nil ||
		roundTrip.ComplexTypes[0].Content.Annotation == nil ||
		roundTrip.ComplexTypes[0].Content.Particles[0].Wildcard.Annotation == nil ||
		roundTrip.ComplexTypes[0].Content.Particles[1].Group.Annotation == nil ||
		roundTrip.ComplexTypes[0].Content.Particles[2].Annotation == nil ||
		roundTrip.ComplexTypes[0].AttributeGroupReferences[0].Annotation == nil ||
		roundTrip.ComplexTypes[0].AttributeWildcard.Annotation == nil ||
		roundTrip.ComplexTypes[1].ContentAnnotation == nil ||
		roundTrip.ComplexTypes[1].DerivationAnnotation == nil ||
		roundTrip.ModelGroups[0].Annotation == nil ||
		roundTrip.AttributeGroups[0].Annotation == nil ||
		roundTrip.Attributes[0].Annotation == nil ||
		roundTrip.Elements[0].Annotation == nil ||
		roundTrip.Elements[0].IdentityConstraints[0].Annotation == nil ||
		roundTrip.Elements[0].IdentityConstraints[0].SelectorAnnotation == nil ||
		roundTrip.Elements[0].IdentityConstraints[0].FieldAnnotations[0] == nil {
		t.Fatalf("round trip lost component annotations: %#v\n%s", roundTrip, encoded)
	}
}

func TestParseRejectsUseOnGlobalAttribute(t *testing.T) {
	t.Parallel()

	_, err := xsd.Parse(context.Background(), []byte(
		`<schema xmlns="http://www.w3.org/2001/XMLSchema">
 <attribute name="value" use="required"/>
</schema>`), xsd.ParseOptions{})
	if err == nil {
		t.Fatal("Parse() error = nil, want global attribute use rejection")
	}
}

func TestParseResolvesQNameAttributesInTheirNamespaceScope(t *testing.T) {
	t.Parallel()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:scope">`+
			`<xs:simpleType name="Code"><xs:restriction xmlns:s="http://www.w3.org/2001/XMLSchema" base="s:string"/></xs:simpleType>`+
			`<xs:simpleType name="Choice"><xs:union xmlns:t="urn:scope" xmlns:s="http://www.w3.org/2001/XMLSchema" memberTypes="t:Code s:boolean"/></xs:simpleType>`+
			`<xs:element xmlns:t="urn:scope" name="root" type="t:Choice"/>`+
			`</xs:schema>`,
	), xsd.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if document.SimpleTypes[0].Base != (xsd.QName{Namespace: xsd.Namespace, Local: "string"}) ||
		len(document.SimpleTypes[1].MemberTypes) != 2 ||
		document.SimpleTypes[1].MemberTypes[0] != (xsd.QName{Namespace: "urn:scope", Local: "Code"}) ||
		document.SimpleTypes[1].MemberTypes[1] != (xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}) ||
		document.Elements[0].Type != (xsd.QName{Namespace: "urn:scope", Local: "Choice"}) {
		t.Fatalf("Parse() namespace scopes = %#v, %#v", document.SimpleTypes, document.Elements)
	}
}

func TestQNameValueNamespaceScopeSurvivesSerialization(t *testing.T) {
	t.Parallel()

	source := []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:scope">
 <xs:element xmlns:value="urn:value" name="root" type="xs:QName" fixed="value:item"/>
 <xs:simpleType name="Choice"><xs:restriction base="xs:QName">
  <xs:enumeration xmlns:value="urn:value" value="value:item"/>
 </xs:restriction></xs:simpleType>
</xs:schema>`)
	document, err := xsd.Parse(context.Background(), source, xsd.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := xsd.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := xsd.Parse(context.Background(), encoded, xsd.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v\n%s", err, encoded)
	}
	if roundTrip.Elements[0].ValueNamespaces["value"] != "urn:value" ||
		roundTrip.SimpleTypes[0].Facets[0].Namespaces["value"] != "urn:value" {
		t.Fatalf("value namespaces were lost: %#v\n%s", roundTrip, encoded)
	}
}
