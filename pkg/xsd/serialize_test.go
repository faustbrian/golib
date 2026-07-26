package xsd_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
)

func TestMarshalWithOptionsEnforcesResourceLimits(t *testing.T) {
	t.Parallel()

	document := &xsd.Document{Elements: []xsd.Element{{Name: "root"}}}
	encoded, err := xsd.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := xsd.MarshalWithOptions(document, xsd.MarshalOptions{
		MaxOutputBytes: int64(len(encoded)),
	}); err != nil {
		t.Fatalf("MarshalWithOptions(exact output) error = %v", err)
	}
	if _, err := xsd.MarshalWithOptions(document, xsd.MarshalOptions{
		MaxOutputBytes: int64(len(encoded) - 1),
	}); !errors.Is(err, xsd.ErrLimitExceeded) {
		t.Fatalf("MarshalWithOptions(output limit) error = %v", err)
	}

	group := &xsd.ModelGroup{Compositor: xsd.Sequence}
	group.Particles = []xsd.Particle{{Group: group}}
	cyclic := &xsd.Document{ModelGroups: []xsd.ModelGroupDefinition{{
		Name: "cycle", Content: group,
	}}}
	if _, err := xsd.MarshalWithOptions(cyclic, xsd.MarshalOptions{}); !errors.Is(
		err,
		xsd.ErrLimitExceeded,
	) {
		t.Fatalf("MarshalWithOptions(cycle) error = %v", err)
	}

	deep := &xsd.ModelGroup{Compositor: xsd.Sequence}
	deep.Particles = []xsd.Particle{{Group: &xsd.ModelGroup{Compositor: xsd.Sequence}}}
	if _, err := xsd.MarshalWithOptions(&xsd.Document{
		ModelGroups: []xsd.ModelGroupDefinition{{Name: "deep", Content: deep}},
	}, xsd.MarshalOptions{MaxDepth: 1}); !errors.Is(err, xsd.ErrLimitExceeded) {
		t.Fatalf("MarshalWithOptions(depth limit) error = %v", err)
	}

	if _, err := xsd.MarshalWithOptions(document, xsd.MarshalOptions{
		MaxComponents: 1,
	}); !errors.Is(err, xsd.ErrLimitExceeded) {
		t.Fatalf("MarshalWithOptions(component limit) error = %v", err)
	}
}

func TestMarshalWithOptionsRejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	for _, options := range []xsd.MarshalOptions{
		{MaxOutputBytes: -1},
		{MaxDepth: -1},
		{MaxComponents: -1},
	} {
		if _, err := xsd.MarshalWithOptions(&xsd.Document{}, options); err == nil {
			t.Fatalf("MarshalWithOptions(%+v) succeeded", options)
		}
	}
}

func TestMarshalIsDeterministicAndRoundTripsSchemaComponents(t *testing.T) {
	t.Parallel()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:serialize" targetNamespace="urn:serialize" elementFormDefault="qualified">
 <xs:simpleType name="Code"><xs:restriction base="xs:string">
  <xs:minLength value="2"/>
 </xs:restriction></xs:simpleType>
 <xs:complexType name="Root"><xs:sequence>
  <xs:element name="code" type="tns:Code"/>
  <xs:any namespace="##other" processContents="lax" minOccurs="0"/>
 </xs:sequence><xs:attribute name="status" type="xs:string" use="required"/></xs:complexType>
 <xs:element name="root" type="tns:Root"/>
</xs:schema>`), xsd.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := xsd.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := xsd.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("Marshal() is not deterministic:\n%s\n%s", first, second)
	}
	roundTrip, err := xsd.Parse(context.Background(), first, xsd.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v\n%s", err, first)
	}
	if roundTrip.TargetNamespace != "urn:serialize" ||
		len(roundTrip.SimpleTypes) != 1 || len(roundTrip.ComplexTypes) != 1 ||
		len(roundTrip.Elements) != 1 {
		t.Fatalf("round trip = %#v", roundTrip)
	}
	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/round-trip.xsd", Content: first,
	}); err != nil {
		t.Fatalf("Compile(Marshal()) error = %v\n%s", err, first)
	}
}

func TestMarshalAllocatesPrefixesForProgrammaticQNames(t *testing.T) {
	t.Parallel()

	document := &xsd.Document{
		TargetNamespace: "urn:generated",
		Elements: []xsd.Element{{
			Name: "root",
			Type: xsd.QName{Namespace: "urn:types", Local: "External"},
		}},
	}
	encoded, err := xsd.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := xsd.Parse(context.Background(), encoded, xsd.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v\n%s", err, encoded)
	}
	if parsed.Elements[0].Type != (xsd.QName{Namespace: "urn:types", Local: "External"}) {
		t.Fatalf("Element.Type = %#v\n%s", parsed.Elements[0].Type, encoded)
	}
}

func TestMarshalPreservesEmptyValueConstraints(t *testing.T) {
	t.Parallel()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:element name="value" fixed=""/>
 <xs:attribute name="code" default=""/>
</xs:schema>`), xsd.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := xsd.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`fixed=""`)) ||
		!bytes.Contains(encoded, []byte(`default=""`)) {
		t.Fatalf("Marshal() = %s", encoded)
	}
}

func TestMarshalPreservesComponentIDs(t *testing.T) {
	t.Parallel()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" id="schema-id">
 <xs:include id="include-id" schemaLocation="included.xsd"/>
 <xs:simpleType id="simple-id" name="Code"><xs:restriction id="restriction-id" base="xs:string">
  <xs:length id="facet-id" value="1"/>
 </xs:restriction></xs:simpleType>
 <xs:group id="group-id" name="Items"><xs:sequence id="group-content-id"/></xs:group>
 <xs:attributeGroup id="attributes-id" name="Metadata">
  <xs:attribute id="use-id" name="code"/><xs:anyAttribute id="attribute-wildcard-id"/>
 </xs:attributeGroup>
 <xs:complexType id="complex-id" name="Record"><xs:sequence id="sequence-id">
  <xs:element id="local-element-id" name="value"/><xs:any id="wildcard-id"/>
  <xs:group id="group-reference-id" ref="Items"/>
 </xs:sequence><xs:attributeGroup id="attribute-reference-id" ref="Metadata"/></xs:complexType>
 <xs:complexType id="derived-id" name="Derived"><xs:complexContent id="content-id">
  <xs:extension id="extension-id" base="Record"/>
 </xs:complexContent></xs:complexType>
 <xs:attribute id="attribute-id" name="global"/>
 <xs:element id="element-id" name="root"><xs:key id="key-id" name="key">
  <xs:selector id="selector-id" xpath="."/><xs:field id="field-id" xpath="@global"/>
 </xs:key></xs:element>
</xs:schema>`), xsd.ParseOptions{})
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
	groupParticles := roundTrip.ComplexTypes[0].Content.Particles
	constraint := roundTrip.Elements[0].IdentityConstraints[0]
	if roundTrip.ID != "schema-id" || roundTrip.References[0].ID != "include-id" ||
		roundTrip.SimpleTypes[0].ID != "simple-id" ||
		roundTrip.SimpleTypes[0].VarietyID != "restriction-id" ||
		roundTrip.SimpleTypes[0].Facets[0].ID != "facet-id" ||
		roundTrip.ModelGroups[0].ID != "group-id" ||
		roundTrip.ModelGroups[0].Content.ID != "group-content-id" ||
		roundTrip.AttributeGroups[0].ID != "attributes-id" ||
		roundTrip.AttributeGroups[0].Attributes[0].ID != "use-id" ||
		roundTrip.AttributeGroups[0].Wildcard.ID != "attribute-wildcard-id" ||
		roundTrip.ComplexTypes[0].ID != "complex-id" ||
		roundTrip.ComplexTypes[0].Content.ID != "sequence-id" ||
		groupParticles[0].Element.ID != "local-element-id" ||
		groupParticles[1].Wildcard.ID != "wildcard-id" ||
		groupParticles[2].ID != "group-reference-id" ||
		roundTrip.ComplexTypes[0].AttributeGroupReferences[0].ID != "attribute-reference-id" ||
		roundTrip.ComplexTypes[1].ContentID != "content-id" ||
		roundTrip.ComplexTypes[1].DerivationID != "extension-id" ||
		roundTrip.Attributes[0].ID != "attribute-id" ||
		roundTrip.Elements[0].ID != "element-id" || constraint.ID != "key-id" ||
		constraint.SelectorID != "selector-id" || constraint.FieldIDs[0] != "field-id" {
		t.Fatalf("round trip lost component IDs: %#v\n%s", roundTrip, encoded)
	}
}

func TestMarshalPreservesAnnotationContentIDs(t *testing.T) {
	t.Parallel()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:annotation>
 <xs:documentation id="documentation-id">text</xs:documentation>
 <xs:appinfo id="appinfo-id"><tool xmlns="urn:tool"/></xs:appinfo>
</xs:annotation></xs:schema>`), xsd.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := xsd.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := xsd.Parse(context.Background(), encoded, xsd.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	annotation := roundTrip.Annotations[0]
	if annotation.Documentation[0].ID != "documentation-id" ||
		annotation.AppInformation[0].ID != "appinfo-id" {
		t.Fatalf("annotation = %#v\n%s", annotation, encoded)
	}
}

func TestMarshalPreservesSimpleContentRestrictionTypesAndFacets(t *testing.T) {
	t.Parallel()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:complexType name="Restricted"><xs:simpleContent>
  <xs:restriction base="xs:string"><xs:simpleType>
   <xs:restriction base="xs:string"><xs:minLength value="2"/></xs:restriction>
  </xs:simpleType><xs:maxLength value="3" fixed="true"/>
  </xs:restriction>
 </xs:simpleContent></xs:complexType>
</xs:schema>`), xsd.ParseOptions{})
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
	typeDefinition := roundTrip.ComplexTypes[0]
	if typeDefinition.InlineSimpleType == nil ||
		len(typeDefinition.InlineSimpleType.Facets) != 1 ||
		len(typeDefinition.SimpleFacets) != 1 ||
		typeDefinition.SimpleFacets[0].Kind != xsd.FacetMaxLength ||
		!typeDefinition.SimpleFacets[0].Fixed {
		t.Fatalf("ComplexTypes[0] = %#v\n%s", typeDefinition, encoded)
	}
}

func TestMarshalPreservesRedefinitionBodies(t *testing.T) {
	t.Parallel()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:redefine schemaLocation="base.xsd">
  <xs:annotation><xs:documentation>redefinition</xs:documentation></xs:annotation>
  <xs:simpleType name="Code"><xs:restriction base="xs:string"/></xs:simpleType>
  <xs:complexType name="Record"><xs:sequence><xs:element name="code" type="Code"/></xs:sequence></xs:complexType>
  <xs:group name="items"><xs:sequence><xs:element name="item"/></xs:sequence></xs:group>
  <xs:attributeGroup name="metadata"><xs:attribute name="version"/></xs:attributeGroup>
 </xs:redefine>
</xs:schema>`), xsd.ParseOptions{SystemID: "https://example.test/schema.xsd"})
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
	if len(roundTrip.Redefinitions) != 1 ||
		roundTrip.Redefinitions[0].Reference.Annotation == nil ||
		len(roundTrip.Redefinitions[0].SimpleTypes) != 1 ||
		len(roundTrip.Redefinitions[0].ComplexTypes) != 1 ||
		len(roundTrip.Redefinitions[0].ModelGroups) != 1 ||
		len(roundTrip.Redefinitions[0].AttributeGroups) != 1 {
		t.Fatalf("Redefinitions = %#v\n%s", roundTrip.Redefinitions, encoded)
	}
}

func TestMarshalRoundTripsEveryComponentFamily(t *testing.T) {
	t.Parallel()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="urn:all"
 targetNamespace="urn:all" elementFormDefault="qualified" attributeFormDefault="unqualified"
 blockDefault="extension" finalDefault="restriction" version="1.0" xml:lang="en">
 <xs:annotation id="schema"><xs:documentation source="urn:docs" xml:lang="en">All components</xs:documentation></xs:annotation>
 <xs:include schemaLocation="included.xsd"/>
 <xs:import namespace="urn:external" schemaLocation="external.xsd"/>
 <xs:simpleType name="Code" final="restriction"><xs:restriction base="xs:string">
  <xs:minLength value="1"/><xs:maxLength value="8"/><xs:pattern value="[A-Z]+"/>
  <xs:enumeration value="A"/><xs:whiteSpace value="collapse" fixed="true"/>
 </xs:restriction></xs:simpleType>
 <xs:simpleType name="Codes"><xs:list><xs:simpleType><xs:restriction base="tns:Code"/></xs:simpleType></xs:list></xs:simpleType>
 <xs:simpleType name="Choice"><xs:union memberTypes="xs:string tns:Code">
  <xs:simpleType><xs:restriction base="xs:decimal"><xs:minInclusive value="0"/></xs:restriction></xs:simpleType>
 </xs:union></xs:simpleType>
 <xs:group name="Items"><xs:choice minOccurs="0" maxOccurs="unbounded">
  <xs:element name="first" type="tns:Code"/><xs:any namespace="##other" processContents="lax"/>
 </xs:choice></xs:group>
 <xs:attributeGroup name="Metadata"><xs:attribute name="version" type="xs:string" use="required"/>
  <xs:anyAttribute namespace="##other" processContents="skip"/>
 </xs:attributeGroup>
 <xs:complexType name="Base" mixed="true" abstract="true" block="extension" final="restriction">
  <xs:sequence minOccurs="0" maxOccurs="2">
   <xs:element name="code" minOccurs="0"><xs:simpleType><xs:restriction base="tns:Code"/></xs:simpleType></xs:element>
   <xs:choice><xs:element name="left"/><xs:group ref="tns:Items"/></xs:choice>
  </xs:sequence>
  <xs:attribute name="status" form="qualified" default=""><xs:simpleType><xs:restriction base="xs:string"/></xs:simpleType></xs:attribute>
  <xs:attributeGroup ref="tns:Metadata"/>
 </xs:complexType>
 <xs:complexType name="Text"><xs:simpleContent><xs:extension base="tns:Code">
  <xs:attribute name="lang" type="xs:language"/>
 </xs:extension></xs:simpleContent></xs:complexType>
 <xs:complexType name="Extended"><xs:complexContent><xs:extension base="tns:Base">
  <xs:sequence><xs:element name="extra" type="xs:string"/></xs:sequence>
 </xs:extension></xs:complexContent></xs:complexType>
 <xs:attribute name="global" fixed=""><xs:simpleType><xs:restriction base="xs:string"/></xs:simpleType></xs:attribute>
 <xs:element name="head" type="tns:Base" abstract="true"/>
 <xs:element name="root" type="tns:Extended" substitutionGroup="tns:head" nillable="true"
  block="restriction" final="extension">
  <xs:unique name="uniqueCode"><xs:selector xpath="tns:code"/><xs:field xpath="."/></xs:unique>
  <xs:key name="codeKey"><xs:selector xpath="tns:code"/><xs:field xpath="."/></xs:key>
  <xs:keyref name="codeRef" refer="tns:codeKey"><xs:selector xpath="tns:code"/><xs:field xpath="."/></xs:keyref>
 </xs:element>
</xs:schema>`), xsd.ParseOptions{SystemID: "https://example.test/all.xsd"})
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
	if len(roundTrip.References) != 2 || len(roundTrip.SimpleTypes) != 3 ||
		len(roundTrip.ComplexTypes) != 3 || len(roundTrip.ModelGroups) != 1 ||
		len(roundTrip.AttributeGroups) != 1 || len(roundTrip.Attributes) != 1 ||
		len(roundTrip.Elements) != 2 || len(roundTrip.Annotations) != 1 {
		t.Fatalf("round trip lost components: %#v", roundTrip)
	}
}
