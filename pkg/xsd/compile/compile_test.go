package compile_test

import (
	"context"
	"errors"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
	"github.com/faustbrian/golib/pkg/xsd/resolve"
)

func TestCompileResolvesCyclesOnceAndAppliesChameleonNamespace(t *testing.T) {
	t.Parallel()

	resolver, err := resolve.NewMemory(map[string][]byte{
		"https://example.test/common.xsd": []byte(`<xs:schema
 xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:include schemaLocation="root.xsd"/>
</xs:schema>`),
		"https://example.test/customer.xsd": []byte(`<xs:schema
 xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:customer"/>`),
	})
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}

	compiler, err := compile.New(compile.Options{Resolver: resolver})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(
		context.Background(),
		compile.Source{
			URI: "https://example.test/root.xsd",
			Content: []byte(`<xs:schema
 xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:order">
 <xs:include schemaLocation="common.xsd"/>
 <xs:import namespace="urn:customer" schemaLocation="customer.xsd"/>
</xs:schema>`),
		},
	)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	documents := set.Documents()
	if len(documents) != 3 {
		t.Fatalf("len(Documents()) = %d, want 3", len(documents))
	}
	common, ok := set.Document("https://example.test/common.xsd")
	if !ok {
		t.Fatal("common document not found")
	}
	if common.Namespace != "urn:order" || !common.Chameleon {
		t.Fatalf("common = %#v", common)
	}

	documents[0].URI = "mutated"
	again := set.Documents()
	if again[0].URI == "mutated" {
		t.Fatal("Documents() exposed mutable set storage")
	}
}

func TestCompileIndexesChameleonComponentsByEffectiveNamespace(t *testing.T) {
	t.Parallel()

	resolver, err := resolve.NewMemory(map[string][]byte{
		"https://example.test/common.xsd": []byte(`<xs:schema
 xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:element name="code" type="xs:string"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := compile.New(compile.Options{Resolver: resolver})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/root.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:order"><xs:include schemaLocation="common.xsd"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	element, ok := set.Element(xsd.QName{Namespace: "urn:order", Local: "code"})
	if !ok {
		t.Fatal("chameleon element not indexed in effective namespace")
	}
	if element.Type != (xsd.QName{Namespace: xsd.Namespace, Local: "string"}) {
		t.Fatalf("Element.Type = %#v", element.Type)
	}
}

func TestCompiledSetExposesDeterministicComponentInventory(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/inventory.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:inventory">
 <xs:notation name="zNotation" public="image/z"><xs:annotation><xs:documentation>z</xs:documentation></xs:annotation></xs:notation>
 <xs:notation name="aNotation" public="image/a"/>
 <xs:element name="element" type="xs:string"/>
 <xs:attribute name="attribute" type="xs:string"/>
 <xs:simpleType name="Simple"><xs:restriction base="xs:string"/></xs:simpleType>
 <xs:complexType name="Complex"/>
 <xs:group name="Model"><xs:sequence/></xs:group>
 <xs:attributeGroup name="Attributes"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNames := func(label string, got []xsd.QName, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s = %#v", label, got)
		}
		for index := range want {
			if got[index] != (xsd.QName{Namespace: "urn:inventory", Local: want[index]}) {
				t.Fatalf("%s[%d] = %#v, want %s", label, index, got[index], want[index])
			}
		}
	}
	assertNames("ElementNames", set.ElementNames(), "element")
	assertNames("AttributeNames", set.AttributeNames(), "attribute")
	assertNames("SimpleTypeNames", set.SimpleTypeNames(), "Simple")
	assertNames("ComplexTypeNames", set.ComplexTypeNames(), "Complex")
	assertNames("ModelGroupNames", set.ModelGroupNames(), "Model")
	assertNames("AttributeGroupNames", set.AttributeGroupNames(), "Attributes")
	assertNames("NotationNames", set.NotationNames(), "aNotation", "zNotation")

	name := xsd.QName{Namespace: "urn:inventory", Local: "zNotation"}
	notation, ok := set.Notation(name)
	if !ok || notation.Annotation == nil {
		t.Fatalf("Notation(%s) = %#v, %t", name.Local, notation, ok)
	}
	notation.Annotation.Documentation[0].Content = "mutated"
	again, ok := set.Notation(name)
	if !ok || again.Annotation.Documentation[0].Content != "z" {
		t.Fatalf("Notation() exposed storage: %#v, %t", again, ok)
	}
	if _, ok := set.Notation(xsd.QName{Local: "missing"}); ok {
		t.Fatal("Notation(missing) succeeded")
	}
}

func TestCompileRejectsDuplicateNotationComponents(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/notations.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:notation name="image" public="image/a"/>
 <xs:notation name="image" public="image/b"/>
</xs:schema>`),
	})
	if !errors.Is(err, compile.ErrDuplicateComponent) {
		t.Fatalf("Compile() error = %v, want ErrDuplicateComponent", err)
	}
}

func TestCompileEnforcesNotationValueSpace(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	valid := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:t="urn:notation" targetNamespace="urn:notation">
 <xs:notation name="png" public="image/png"/>
 <xs:simpleType name="Image"><xs:restriction base="xs:NOTATION">
  <xs:pattern value="t:png"/>
  <xs:enumeration value="t:png"/>
 </xs:restriction></xs:simpleType>
 <xs:simpleType name="DerivedImage"><xs:restriction base="t:Image"><xs:pattern value="t:png"/></xs:restriction></xs:simpleType>
 <xs:element name="image" type="t:Image"/>
</xs:schema>`
	if _, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/valid-notation.xsd", Content: []byte(valid),
	}); err != nil {
		t.Fatalf("Compile(valid NOTATION) error = %v", err)
	}
	for _, schema := range []string{
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:element name="value" type="xs:NOTATION"/></xs:schema>`,
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:attribute name="value" type="xs:NOTATION"/></xs:schema>`,
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:complexType name="Root"><xs:sequence><xs:element name="value" type="xs:NOTATION"/></xs:sequence></xs:complexType></xs:schema>`,
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:simpleType name="Value"><xs:restriction base="xs:NOTATION"/></xs:simpleType></xs:schema>`,
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:t="urn:notation" targetNamespace="urn:notation"><xs:simpleType name="Value"><xs:restriction base="xs:NOTATION"><xs:enumeration value="t:missing"/></xs:restriction></xs:simpleType></xs:schema>`,
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:simpleType name="Value"><xs:restriction base="xs:NOTATION"><xs:enumeration value="missing:value"/></xs:restriction></xs:simpleType></xs:schema>`,
	} {
		if _, err := compiler.Compile(context.Background(), compile.Source{
			URI: "https://example.test/invalid-notation.xsd", Content: []byte(schema),
		}); !errors.Is(err, compile.ErrInvalidComponent) {
			t.Fatalf("Compile(invalid NOTATION) error = %v, want ErrInvalidComponent", err)
		}
	}
}

func TestCompileRejectsDuplicateTypeComponentsAcrossKinds(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/root.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:order">
 <xs:simpleType name="Code"><xs:restriction base="xs:string"/></xs:simpleType>
 <xs:complexType name="Code"/>
</xs:schema>`),
	})
	if !errors.Is(err, compile.ErrDuplicateComponent) {
		t.Fatalf("Compile() error = %v, want ErrDuplicateComponent", err)
	}
}

func TestCompiledComplexTypesDoNotExposeMutableModelStorage(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/root.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:order"><xs:complexType name="Order">
 <xs:sequence><xs:element name="id" type="xs:string"/></xs:sequence>
 <xs:attribute name="status" type="xs:string"/>
</xs:complexType></xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	name := xsd.QName{Namespace: "urn:order", Local: "Order"}
	typeDefinition, ok := set.ComplexType(name)
	if !ok {
		t.Fatal("complex type missing")
	}
	typeDefinition.Content.Particles[0].Element.Name = "mutated"
	typeDefinition.Attributes[0].Name = "mutated"

	again, ok := set.ComplexType(name)
	if !ok {
		t.Fatal("complex type missing after mutation")
	}
	if again.Content.Particles[0].Element.Name != "id" ||
		again.Attributes[0].Name != "status" {
		t.Fatalf("ComplexType() exposed storage: %#v", again)
	}
}

func TestCompiledGroupsDoNotExposeMutableModelStorage(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/groups.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:test"><xs:group name="Fields"><xs:sequence>
 <xs:element name="id" type="xs:string"/></xs:sequence></xs:group>
 <xs:attributeGroup name="Metadata">
 <xs:attribute name="status" type="xs:string"/></xs:attributeGroup>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	name := xsd.QName{Namespace: "urn:test", Local: "Fields"}
	modelGroup, ok := set.ModelGroup(name)
	if !ok {
		t.Fatal("model group missing")
	}
	modelGroup.Content.Particles[0].Element.Name = "mutated"
	modelGroup, _ = set.ModelGroup(name)
	if modelGroup.Content.Particles[0].Element.Name != "id" {
		t.Fatalf("ModelGroup() exposed storage: %#v", modelGroup)
	}

	attributeName := xsd.QName{Namespace: "urn:test", Local: "Metadata"}
	attributeGroup, ok := set.AttributeGroup(attributeName)
	if !ok {
		t.Fatal("attribute group missing")
	}
	attributeGroup.Attributes[0].Name = "mutated"
	attributeGroup, _ = set.AttributeGroup(attributeName)
	if attributeGroup.Attributes[0].Name != "status" {
		t.Fatalf("AttributeGroup() exposed storage: %#v", attributeGroup)
	}
}

func TestCompiledSimpleTypesDoNotExposeMutableModelStorage(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/simple.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:test"><xs:simpleType name="Code">
 <xs:restriction base="xs:string"><xs:minLength value="2"/></xs:restriction>
</xs:simpleType></xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	name := xsd.QName{Namespace: "urn:test", Local: "Code"}
	typeDefinition, ok := set.SimpleType(name)
	if !ok {
		t.Fatal("simple type missing")
	}
	typeDefinition.Facets[0].Value = "999"

	again, ok := set.SimpleType(name)
	if !ok || again.Facets[0].Value != "2" {
		t.Fatalf("SimpleType() exposed storage: %#v", again)
	}
}

func TestCompileNormalizesChameleonLocalNamesAndReferences(t *testing.T) {
	t.Parallel()

	resolver, err := resolve.NewMemory(map[string][]byte{
		"https://example.test/common.xsd": []byte(`<xs:schema
 xmlns:xs="http://www.w3.org/2001/XMLSchema"
 elementFormDefault="qualified" attributeFormDefault="unqualified">
 <xs:simpleType name="LocalCode"><xs:restriction base="xs:string"/></xs:simpleType>
 <xs:complexType name="Common">
  <xs:sequence><xs:element name="code" type="LocalCode"/></xs:sequence>
  <xs:attribute name="lang" type="xs:string"/>
 </xs:complexType>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := compile.New(compile.Options{Resolver: resolver})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/root.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:order"><xs:include schemaLocation="common.xsd"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	typeDefinition, ok := set.ComplexType(xsd.QName{Namespace: "urn:order", Local: "Common"})
	if !ok {
		t.Fatal("complex type missing")
	}
	element := typeDefinition.Content.Particles[0].Element
	if element.Form != xsd.FormQualified ||
		element.Type != (xsd.QName{Namespace: "urn:order", Local: "LocalCode"}) {
		t.Fatalf("local element = %#v", element)
	}
	attribute := typeDefinition.Attributes[0]
	if attribute.Form != xsd.FormUnqualified {
		t.Fatalf("local attribute = %#v", attribute)
	}
}

func TestCompileRejectsInvalidAndUnresolvedComponentContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
		want   error
	}{
		{
			name: "default and fixed",
			schema: `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:element name="value" type="xs:string" default="a" fixed="a"/>
</xs:schema>`,
			want: compile.ErrInvalidComponent,
		},
		{
			name: "unresolved type",
			schema: `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:test" targetNamespace="urn:test">
 <xs:element name="value" type="tns:Missing"/>
</xs:schema>`,
			want: compile.ErrUnresolvedComponent,
		},
		{
			name: "all occurrence",
			schema: `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:test" targetNamespace="urn:test">
 <xs:complexType name="Invalid"><xs:all>
  <xs:element name="value" type="xs:string" maxOccurs="2"/>
 </xs:all></xs:complexType>
</xs:schema>`,
			want: compile.ErrInvalidComponent,
		},
		{
			name: "recursive simple type",
			schema: `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:test" targetNamespace="urn:test">
 <xs:simpleType name="Cycle"><xs:restriction base="tns:Cycle"/></xs:simpleType>
</xs:schema>`,
			want: compile.ErrInvalidComponent,
		},
		{
			name: "list without item type",
			schema: `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:simpleType name="Items"><xs:list/></xs:simpleType>
</xs:schema>`,
			want: compile.ErrInvalidComponent,
		},
		{
			name: "unresolved model group",
			schema: `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:test" targetNamespace="urn:test">
 <xs:complexType name="Value"><xs:sequence>
  <xs:group ref="tns:Missing"/>
 </xs:sequence></xs:complexType>
</xs:schema>`,
			want: compile.ErrUnresolvedComponent,
		},
		{
			name: "recursive attribute groups",
			schema: `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:test" targetNamespace="urn:test">
 <xs:attributeGroup name="First"><xs:attributeGroup ref="tns:Second"/></xs:attributeGroup>
 <xs:attributeGroup name="Second"><xs:attributeGroup ref="tns:First"/></xs:attributeGroup>
</xs:schema>`,
			want: compile.ErrInvalidComponent,
		},
		{
			name: "unresolved key reference",
			schema: `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:test" targetNamespace="urn:test">
 <xs:element name="root" type="xs:string"><xs:keyref name="reference" refer="tns:missing">
  <xs:selector xpath="tns:item"/><xs:field xpath="@id"/>
 </xs:keyref></xs:element>
</xs:schema>`,
			want: compile.ErrUnresolvedComponent,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			compiler, err := compile.New(compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = compiler.Compile(context.Background(), compile.Source{
				URI:     "https://example.test/invalid.xsd",
				Content: []byte(test.schema),
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("Compile() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCompileComplexContentRestriction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name: "narrows particle occurrences and attributes",
			content: `<xs:sequence>
  <xs:element name="value" type="xs:string" minOccurs="1" maxOccurs="2"/>
 </xs:sequence>
 <xs:attribute name="code" type="xs:string" use="required"/>`,
		},
		{
			name: "adds an element",
			content: `<xs:sequence>
  <xs:element name="other" type="xs:string"/>
 </xs:sequence>`,
			wantErr: true,
		},
		{
			name: "drops a required attribute",
			content: `<xs:sequence>
  <xs:element name="value" type="xs:string"/>
 </xs:sequence>`,
			wantErr: true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			compiler, err := compile.New(compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = compiler.Compile(context.Background(), compile.Source{
				URI: "https://example.test/restriction.xsd",
				Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:test" targetNamespace="urn:test">
 <xs:complexType name="Base"><xs:sequence>
  <xs:element name="value" type="xs:string" minOccurs="0" maxOccurs="3"/>
 </xs:sequence><xs:attribute name="code" type="xs:string" use="required"/></xs:complexType>
 <xs:complexType name="Restricted"><xs:complexContent><xs:restriction base="tns:Base">` +
					test.content + `</xs:restriction></xs:complexContent></xs:complexType>
</xs:schema>`),
			})
			if test.wantErr && !errors.Is(err, compile.ErrInvalidComponent) {
				t.Fatalf("Compile() error = %v, want ErrInvalidComponent", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
		})
	}
}

func TestCompileAnonymousComplexContentExtension(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/anonymous-extension.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:test" targetNamespace="urn:test">
 <xs:complexType name="Base"><xs:sequence>
  <xs:element name="base" type="xs:string"/>
 </xs:sequence><xs:attribute name="id" type="xs:string" use="required"/></xs:complexType>
 <xs:element name="root"><xs:complexType><xs:complexContent>
  <xs:extension base="tns:Base"><xs:sequence>
   <xs:element name="extra" type="xs:string"/>
  </xs:sequence></xs:extension>
 </xs:complexContent></xs:complexType></xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	element, ok := set.Element(xsd.QName{Namespace: "urn:test", Local: "root"})
	if !ok || element.InlineComplexType == nil {
		t.Fatal("anonymous complex type missing")
	}
	typeDefinition := element.InlineComplexType
	if typeDefinition.Content == nil || len(typeDefinition.Content.Particles) != 2 {
		t.Fatalf("Content = %#v, want base followed by extension", typeDefinition.Content)
	}
	if len(typeDefinition.Attributes) != 1 || typeDefinition.Attributes[0].Name != "id" {
		t.Fatalf("Attributes = %#v, want inherited id", typeDefinition.Attributes)
	}
}

func TestCompileRejectsDuplicateAnonymousComplexTypeAttributes(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/duplicate-anonymous-attributes.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:element name="root"><xs:complexType>
  <xs:attribute name="same"/><xs:attribute name="same"/>
 </xs:complexType></xs:element>
</xs:schema>`),
	})
	if !errors.Is(err, compile.ErrInvalidComponent) {
		t.Fatalf("Compile() error = %v, want ErrInvalidComponent", err)
	}
}

func TestCompileResolvesLocationlessImportsThroughCatalog(t *testing.T) {
	t.Parallel()

	resources, err := resolve.NewMemory(map[string][]byte{
		"urn:schema:dependency": []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:dependency"><xs:simpleType name="Code"><xs:restriction base="xs:string"/></xs:simpleType></xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := resolve.NewCatalog(
		map[string]string{"urn:dependency": "urn:schema:dependency"},
		resources,
	)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := compile.New(compile.Options{Resolver: catalog})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:schema:root",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:dep="urn:dependency"><xs:import namespace="urn:dependency"/>
 <xs:element name="root" type="dep:Code"/></xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := set.SimpleType(xsd.QName{Namespace: "urn:dependency", Local: "Code"}); !ok {
		t.Fatal("catalog import did not contribute its simple type")
	}
}

func TestCompileRejectsAmbiguousContentModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model string
	}{
		{
			name: "duplicate choice elements",
			model: `<xs:choice><xs:element name="value" type="xs:string"/>
 <xs:element name="value" type="xs:decimal"/></xs:choice>`,
		},
		{
			name: "optional sequence prefix",
			model: `<xs:sequence><xs:element name="value" type="xs:string" minOccurs="0"/>
 <xs:element name="value" type="xs:string"/></xs:sequence>`,
		},
		{
			name: "element wildcard overlap",
			model: `<xs:choice><xs:element name="value" type="xs:string"/>
 <xs:any namespace="##targetNamespace"/></xs:choice>`,
		},
		{
			name: "repeated sequence boundary",
			model: `<xs:sequence><xs:element name="value" maxOccurs="2"/>
 <xs:element name="value"/></xs:sequence>`,
		},
		{
			name: "repeated choice boundary",
			model: `<xs:sequence><xs:choice maxOccurs="unbounded">
 <xs:element name="value"/><xs:element name="other"/></xs:choice>
 <xs:element name="value"/></xs:sequence>`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			compiler, err := compile.New(compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = compiler.Compile(context.Background(), compile.Source{
				URI: "https://example.test/ambiguous.xsd",
				Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:test" elementFormDefault="qualified">
 <xs:complexType name="Ambiguous">` + test.model + `</xs:complexType>
</xs:schema>`),
			})
			if !errors.Is(err, compile.ErrInvalidComponent) {
				t.Fatalf("Compile() error = %v, want ErrInvalidComponent", err)
			}
		})
	}
}

func TestCompileIncludesSubstitutionMembersInParticleAttribution(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		headBlock string
		wantError bool
	}{
		{name: "open head", wantError: true},
		{name: "blocked head", headBlock: ` block="substitution"`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			compiler, err := compile.New(compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = compiler.Compile(context.Background(), compile.Source{
				URI: "https://example.test/substitution-upa.xsd",
				Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:test" targetNamespace="urn:test">
 <xs:element name="head" type="xs:string"` + test.headBlock + `/>
 <xs:element name="member" type="xs:string" substitutionGroup="tns:head"/>
 <xs:complexType name="Content"><xs:choice>
  <xs:element ref="tns:head"/><xs:element ref="tns:member"/>
 </xs:choice></xs:complexType>
</xs:schema>`),
			})
			if test.wantError && !errors.Is(err, compile.ErrInvalidComponent) {
				t.Fatalf("Compile() error = %v, want ErrInvalidComponent", err)
			}
			if !test.wantError && err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
		})
	}
}

func TestCompileUsesDenyResolverByDefault(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/root.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:include schemaLocation="file:///etc/passwd"/>
</xs:schema>`),
	})
	if !errors.Is(err, resolve.ErrAccessDenied) {
		t.Fatalf("Compile() error = %v, want ErrAccessDenied", err)
	}
}

func TestCompileAcceptsCurrentNodeIdentitySelector(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/identity.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:element name="root" type="xs:string">
  <xs:unique name="self"><xs:selector xpath="."/><xs:field xpath="."/></xs:unique>
 </xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
}

func TestCompileRejectsDeclarationPropertiesOnElementReference(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/reference.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:element name="item" type="xs:string"/>
 <xs:complexType name="Container"><xs:sequence>
  <xs:element ref="item" type="xs:string"/>
 </xs:sequence></xs:complexType>
</xs:schema>`),
	})
	if !errors.Is(err, compile.ErrInvalidComponent) {
		t.Fatalf("Compile() error = %v, want ErrInvalidComponent", err)
	}
}

func TestCompileRejectsInvalidElementValueConstraints(t *testing.T) {
	t.Parallel()

	for _, declaration := range []string{
		`<xs:element name="value" type="xs:boolean" default="yes"/>`,
		`<xs:element name="value" type="xs:ID" fixed="identifier"/>`,
	} {
		compiler, err := compile.New(compile.Options{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = compiler.Compile(context.Background(), compile.Source{
			URI: "https://example.test/value.xsd",
			Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">` +
				declaration + `</xs:schema>`),
		})
		if !errors.Is(err, compile.ErrInvalidComponent) {
			t.Fatalf("Compile(%s) error = %v, want ErrInvalidComponent", declaration, err)
		}
	}
}

func TestCompileRejectsInvalidAttributeValueConstraints(t *testing.T) {
	t.Parallel()

	for _, declaration := range []string{
		`<xs:attribute name="value" type="xs:boolean" default="yes"/>`,
		`<xs:attribute name="value" default="A"><xs:simpleType><xs:restriction base="xs:string"><xs:minLength value="2"/></xs:restriction></xs:simpleType></xs:attribute>`,
		`<xs:complexType name="Container"><xs:attribute name="value" type="xs:boolean" fixed="yes"/></xs:complexType>`,
		`<xs:attribute name="global" type="xs:boolean"/><xs:complexType name="Container"><xs:attribute ref="global" default="yes"/></xs:complexType>`,
	} {
		compiler, err := compile.New(compile.Options{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = compiler.Compile(context.Background(), compile.Source{
			URI: "https://example.test/attribute-value.xsd",
			Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">` +
				declaration + `</xs:schema>`),
		})
		if !errors.Is(err, compile.ErrInvalidComponent) {
			t.Fatalf("Compile(%s) error = %v, want ErrInvalidComponent", declaration, err)
		}
	}
}

func TestCompileComparesValueConstraintsInTheDeclaredValueSpace(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		content   string
		wantError bool
	}{
		{
			name: "QName prefixes may differ",
			content: `<xs:simpleType name="Code"><xs:restriction base="xs:QName">` +
				`<xs:enumeration value="allowed:item" xmlns:allowed="urn:value"/>` +
				`</xs:restriction></xs:simpleType>` +
				`<xs:element name="value" type="Code" default="actual:item" ` +
				`xmlns:actual="urn:value"/>`,
		},
		{
			name: "QName namespaces must match",
			content: `<xs:simpleType name="Code"><xs:restriction base="xs:QName">` +
				`<xs:enumeration value="allowed:item" xmlns:allowed="urn:value"/>` +
				`</xs:restriction></xs:simpleType>` +
				`<xs:element name="value" type="Code" default="actual:item" ` +
				`xmlns:actual="urn:other"/>`,
			wantError: true,
		},
		{
			name: "list items use item value equality",
			content: `<xs:simpleType name="IntegerList"><xs:list itemType="xs:integer"/>` +
				`</xs:simpleType><xs:simpleType name="Pair">` +
				`<xs:restriction base="IntegerList"><xs:enumeration value="01 2"/>` +
				`</xs:restriction></xs:simpleType>` +
				`<xs:element name="value" type="Pair" default="1 02"/>`,
		},
		{
			name: "union members use member value equality",
			content: `<xs:simpleType name="BooleanOrInteger">` +
				`<xs:union memberTypes="xs:boolean xs:integer"/>` +
				`</xs:simpleType><xs:simpleType name="Choice">` +
				`<xs:restriction base="BooleanOrInteger">` +
				`<xs:enumeration value="true"/></xs:restriction></xs:simpleType>` +
				`<xs:element name="value" type="Choice" default="1"/>`,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			compiler, err := compile.New(compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = compiler.Compile(context.Background(), compile.Source{
				URI: "https://example.test/value-space.xsd",
				Content: []byte(`<xs:schema ` +
					`xmlns:xs="http://www.w3.org/2001/XMLSchema">` +
					test.content + `</xs:schema>`),
			})
			if test.wantError && !errors.Is(err, compile.ErrInvalidComponent) {
				t.Fatalf("Compile() error = %v, want ErrInvalidComponent", err)
			}
			if !test.wantError && err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
		})
	}
}

func TestCompileEnforcesSimpleTypeFinal(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/final.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:simpleType name="Base" final="restriction"><xs:restriction base="xs:string"/></xs:simpleType>
 <xs:simpleType name="Derived"><xs:restriction base="Base"/></xs:simpleType>
</xs:schema>`),
	})
	if !errors.Is(err, compile.ErrInvalidComponent) {
		t.Fatalf("Compile() error = %v, want ErrInvalidComponent", err)
	}
}

func TestCompileAllowsEmptyRestrictionOfNullableContent(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/nullable-restriction.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:restriction" targetNamespace="urn:restriction">
 <xs:complexType name="Base"><xs:sequence>
  <xs:element name="optional" minOccurs="0"/>
 </xs:sequence></xs:complexType>
 <xs:complexType name="Empty"><xs:complexContent>
  <xs:restriction base="tns:Base"/>
 </xs:complexContent></xs:complexType>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCompileRejectsInvalidMixedSimpleContentRestrictions(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		`<xs:complexType name="Base"><xs:simpleContent>` +
			`<xs:extension base="xs:string"/>` +
			`</xs:simpleContent></xs:complexType>` +
			`<xs:complexType name="Derived"><xs:simpleContent>` +
			`<xs:restriction base="Base"><xs:simpleType>` +
			`<xs:restriction base="xs:integer"/>` +
			`</xs:simpleType></xs:restriction>` +
			`</xs:simpleContent></xs:complexType>`,
		`<xs:element name="value"><xs:complexType><xs:simpleContent>` +
			`<xs:restriction base="xs:string"/>` +
			`</xs:simpleContent></xs:complexType></xs:element>`,
		`<xs:complexType name="Derived"><xs:simpleContent>` +
			`<xs:restriction base="xs:string">` +
			`<xs:minInclusive value="1"/>` +
			`</xs:restriction></xs:simpleContent></xs:complexType>`,
		`<xs:complexType name="Base" mixed="true"><xs:sequence>` +
			`<xs:element name="required"/></xs:sequence></xs:complexType>` +
			`<xs:complexType name="Derived"><xs:simpleContent>` +
			`<xs:restriction base="Base"><xs:simpleType>` +
			`<xs:restriction base="xs:string"/></xs:simpleType>` +
			`</xs:restriction></xs:simpleContent></xs:complexType>`,
		`<xs:complexType name="Base" mixed="true"><xs:sequence>` +
			`<xs:element name="optional" minOccurs="0"/>` +
			`</xs:sequence></xs:complexType>` +
			`<xs:complexType name="Derived"><xs:simpleContent>` +
			`<xs:restriction base="Base"/>` +
			`</xs:simpleContent></xs:complexType>`,
	} {
		compiler, err := compile.New(compile.Options{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = compiler.Compile(context.Background(), compile.Source{
			URI: "https://example.test/invalid-mixed-simple-content.xsd",
			Content: []byte(`<xs:schema ` +
				`xmlns:xs="http://www.w3.org/2001/XMLSchema">` +
				content + `</xs:schema>`),
		})
		if !errors.Is(err, compile.ErrInvalidComponent) {
			t.Fatalf("Compile() error = %v, want ErrInvalidComponent", err)
		}
	}
}

func TestCompileRejectsInconsistentAttributeUseSets(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		`<xs:complexType name="Duplicate">` +
			`<xs:attribute name="value"/><xs:attribute name="value"/>` +
			`</xs:complexType>`,
		`<xs:complexType name="MultipleIDs">` +
			`<xs:attribute name="first" type="xs:ID"/>` +
			`<xs:attribute name="second" type="xs:ID"/>` +
			`</xs:complexType>`,
		`<xs:attribute name="global"/><xs:attributeGroup name="Duplicate">` +
			`<xs:attribute ref="global"/><xs:attribute ref="global"/>` +
			`</xs:attributeGroup>`,
	} {
		compiler, err := compile.New(compile.Options{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = compiler.Compile(context.Background(), compile.Source{
			URI: "https://example.test/attribute-use-set.xsd",
			Content: []byte(`<xs:schema ` +
				`xmlns:xs="http://www.w3.org/2001/XMLSchema">` +
				content + `</xs:schema>`),
		})
		if !errors.Is(err, compile.ErrInvalidComponent) {
			t.Fatalf("Compile() error = %v, want ErrInvalidComponent", err)
		}
	}
}

func TestCompileAllowsNarrowerElementTermsInComplexRestrictions(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/narrow-element-term.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:complexType name="Base"><xs:sequence>
  <xs:element name="value" type="xs:long" nillable="true"/>
 </xs:sequence></xs:complexType>
 <xs:complexType name="Derived"><xs:complexContent><xs:restriction base="Base">
  <xs:sequence><xs:element name="value" type="xs:int"/></xs:sequence>
 </xs:restriction></xs:complexContent></xs:complexType>
</xs:schema>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
}

func TestCompileBoundsSchemaCount(t *testing.T) {
	t.Parallel()

	resolver, err := resolve.NewMemory(map[string][]byte{
		"https://example.test/next.xsd": []byte(
			`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`,
		),
	})
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}
	compiler, err := compile.New(compile.Options{
		Resolver: resolver,
		Limits:   compile.Limits{MaxSchemas: 1},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/root.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:include schemaLocation="next.xsd"/>
</xs:schema>`),
	})
	if !errors.Is(err, compile.ErrLimitExceeded) {
		t.Fatalf("Compile() error = %v, want ErrLimitExceeded", err)
	}
}
