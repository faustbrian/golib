package builder_test

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/builder"
	"github.com/faustbrian/golib/pkg/xsd/compile"
)

func TestBuildProducesAnIsolatedCompilableSchema(t *testing.T) {
	t.Parallel()

	schema := builder.New("urn:builder")
	if err := schema.AddSimpleRestriction(
		"Code",
		xsd.QName{Namespace: xsd.Namespace, Local: "string"},
		xsd.Facet{Kind: xsd.FacetMinLength, Value: "2"},
	); err != nil {
		t.Fatal(err)
	}
	content := &xsd.ModelGroup{Compositor: xsd.Sequence, Particles: []xsd.Particle{{
		MinOccurs: 1,
		MaxOccurs: 1,
		Element: &xsd.Element{
			Name: "code",
			Type: xsd.QName{Namespace: "urn:builder", Local: "Code"},
		},
	}}}
	if err := schema.AddComplexType("Root", content); err != nil {
		t.Fatal(err)
	}
	content.Particles[0].Element.Name = "mutated"
	if err := schema.AddElement(
		"root",
		xsd.QName{Namespace: "urn:builder", Local: "Root"},
	); err != nil {
		t.Fatal(err)
	}
	document, err := schema.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if document.ComplexTypes[0].Content.Particles[0].Element.Name != "code" {
		t.Fatalf("Build() retained caller mutation: %#v", document.ComplexTypes[0])
	}
	encoded, err := xsd.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:builder:test", Content: encoded,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBuilderRejectsMalformedAndDuplicateNames(t *testing.T) {
	t.Parallel()

	schema := builder.New("urn:builder")
	typeName := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	if err := schema.AddElement("not valid", typeName); err == nil {
		t.Fatal("AddElement() accepted a malformed name")
	}
	if err := schema.AddElement("root", typeName); err != nil {
		t.Fatal(err)
	}
	if err := schema.AddElement("root", typeName); err == nil {
		t.Fatal("AddElement() accepted a duplicate")
	}
}

func TestBuilderSupportsAttributesAndFormDefaults(t *testing.T) {
	t.Parallel()

	schema := builder.New("urn:builder")
	if err := schema.SetFormDefaults(xsd.FormUnqualified, xsd.FormQualified); err != nil {
		t.Fatal(err)
	}
	if err := schema.AddAttribute(
		"code",
		xsd.QName{Namespace: xsd.Namespace, Local: "string"},
	); err != nil {
		t.Fatal(err)
	}
	document, err := schema.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if document.ElementFormDefault != xsd.FormUnqualified ||
		document.AttributeFormDefault != xsd.FormQualified ||
		len(document.Attributes) != 1 {
		t.Fatalf("Build() = %#v", document)
	}
}

func TestBuilderRejectsInvalidFormsAndMissingTypes(t *testing.T) {
	t.Parallel()

	schema := builder.New("urn:builder")
	if err := schema.SetFormDefaults("invalid", xsd.FormQualified); err == nil {
		t.Fatal("SetFormDefaults() accepted an invalid form")
	}
	if err := schema.AddSimpleRestriction("Code", xsd.QName{}); err == nil {
		t.Fatal("AddSimpleRestriction() accepted an empty base")
	}
	if err := schema.AddElement("root", xsd.QName{}); err == nil {
		t.Fatal("AddElement() accepted an empty type")
	}
	if err := schema.AddAttribute("code", xsd.QName{}); err == nil {
		t.Fatal("AddAttribute() accepted an empty type")
	}
	typeName := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	if err := schema.AddSimpleRestriction("Code", typeName); err != nil {
		t.Fatal(err)
	}
	if err := schema.AddSimpleRestriction("Code", typeName); err == nil {
		t.Fatal("AddSimpleRestriction() accepted a duplicate")
	}
	if err := schema.AddComplexType("Record", nil); err != nil {
		t.Fatal(err)
	}
	if err := schema.AddComplexType("Record", nil); err == nil {
		t.Fatal("AddComplexType() accepted a duplicate")
	}
	if err := schema.AddAttribute("global", typeName); err != nil {
		t.Fatal(err)
	}
	if err := schema.AddAttribute("global", typeName); err == nil {
		t.Fatal("AddAttribute() accepted a duplicate")
	}
}

func TestBuilderDeepCopiesNestedAnonymousTypes(t *testing.T) {
	t.Parallel()

	annotation := &xsd.Annotation{Documentation: []xsd.Documentation{{Content: "original"}}}
	simple := &xsd.SimpleType{
		Variety:    xsd.SimpleRestriction,
		Base:       xsd.QName{Namespace: xsd.Namespace, Local: "string"},
		Facets:     []xsd.Facet{{Kind: xsd.FacetMinLength, Value: "1", Annotation: annotation}},
		Annotation: annotation,
	}
	complex := &xsd.ComplexType{
		Annotation: annotation,
		Attributes: []xsd.AttributeUse{{
			Name:       "status",
			Annotation: annotation,
			InlineSimpleType: &xsd.SimpleType{
				Variety: xsd.SimpleRestriction,
				Base:    xsd.QName{Namespace: xsd.Namespace, Local: "string"},
			},
		}},
		AttributeWildcard: &xsd.Wildcard{Namespaces: []string{"##other"}, Annotation: annotation},
	}
	content := &xsd.ModelGroup{Compositor: xsd.Sequence, MinOccurs: 0, MaxOccurs: 2, OccursSet: true, Annotation: annotation, Particles: []xsd.Particle{
		{MinOccurs: 1, MaxOccurs: 1, Group: &xsd.ModelGroup{Compositor: xsd.Choice, Particles: []xsd.Particle{{
			MinOccurs: 1,
			MaxOccurs: 1,
			Element: &xsd.Element{
				Name:             "simple",
				Annotation:       annotation,
				InlineSimpleType: simple,
				IdentityConstraints: []xsd.IdentityConstraint{{
					Name:               "identity",
					Kind:               xsd.IdentityUnique,
					Selector:           ".",
					Fields:             []string{"."},
					Namespaces:         map[string]string{"tns": "urn:builder"},
					Annotation:         annotation,
					SelectorAnnotation: annotation,
					FieldAnnotations:   []*xsd.Annotation{annotation},
				}},
			},
		}}}},
		{MinOccurs: 1, MaxOccurs: 1, Element: &xsd.Element{Name: "complex", InlineComplexType: complex}},
		{MinOccurs: 0, MaxOccurs: 1, Wildcard: &xsd.Wildcard{Namespaces: []string{"##other"}, Annotation: annotation}},
	}}
	schema := builder.New("urn:builder")
	if err := schema.AddComplexType("Root", content); err != nil {
		t.Fatal(err)
	}
	simple.Facets[0].Value = "99"
	complex.Attributes[0].InlineSimpleType.Base.Local = "boolean"
	complex.AttributeWildcard.Namespaces[0] = "##local"
	content.Particles[2].Wildcard.Namespaces[0] = "##local"
	annotation.Documentation[0].Content = "mutated"
	if err := schema.AddElement("root", xsd.QName{Namespace: "urn:builder", Local: "Root"}); err != nil {
		t.Fatal(err)
	}
	document, err := schema.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	particles := document.ComplexTypes[0].Content.Particles
	simpleElement := particles[0].Group.Particles[0].Element
	if simpleElement.InlineSimpleType.Facets[0].Value != "1" ||
		document.ComplexTypes[0].Content.MinOccurs != 0 ||
		document.ComplexTypes[0].Content.MaxOccurs != 2 ||
		simpleElement.Annotation.Documentation[0].Content != "original" ||
		simpleElement.InlineSimpleType.Annotation.Documentation[0].Content != "original" ||
		simpleElement.InlineSimpleType.Facets[0].Annotation.Documentation[0].Content != "original" ||
		simpleElement.IdentityConstraints[0].Annotation.Documentation[0].Content != "original" ||
		simpleElement.IdentityConstraints[0].SelectorAnnotation.Documentation[0].Content != "original" ||
		simpleElement.IdentityConstraints[0].FieldAnnotations[0].Documentation[0].Content != "original" ||
		document.ComplexTypes[0].Content.Annotation.Documentation[0].Content != "original" ||
		particles[1].Element.InlineComplexType.Annotation.Documentation[0].Content != "original" ||
		particles[1].Element.InlineComplexType.Attributes[0].Annotation.Documentation[0].Content != "original" ||
		simpleElement.IdentityConstraints[0].Fields[0] != "." ||
		simpleElement.IdentityConstraints[0].Namespaces["tns"] != "urn:builder" ||
		particles[1].Element.InlineComplexType.Attributes[0].InlineSimpleType.Base.Local != "string" ||
		particles[1].Element.InlineComplexType.AttributeWildcard.Namespaces[0] != "##other" ||
		particles[1].Element.InlineComplexType.AttributeWildcard.Annotation.Documentation[0].Content != "original" ||
		particles[2].Wildcard.Namespaces[0] != "##other" ||
		particles[2].Wildcard.Annotation.Documentation[0].Content != "original" {
		t.Fatalf("Build() retained caller mutations: %#v", particles)
	}
}

func TestBuildRejectsAnInvalidGeneratedSchema(t *testing.T) {
	t.Parallel()

	schema := builder.New("urn:builder")
	if err := schema.AddSimpleRestriction(
		"Code",
		xsd.QName{Namespace: "urn:missing", Local: "Missing"},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := schema.Build(context.Background()); err == nil {
		t.Fatal("Build() accepted an invalid generated schema")
	}
}
