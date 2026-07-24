package compile

import (
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestCompiledComponentClonesIsolateAnnotations(t *testing.T) {
	t.Parallel()

	annotation := testAnnotation()
	simple := xsd.SimpleType{
		Annotation:        annotation,
		VarietyAnnotation: annotation,
		Facets: []xsd.Facet{{
			Namespaces: map[string]string{"value": "urn:value"},
			Annotation: annotation,
		}},
		InlineBase:    &xsd.SimpleType{Annotation: annotation},
		InlineItem:    &xsd.SimpleType{Annotation: annotation},
		InlineMembers: []xsd.SimpleType{{Annotation: annotation}},
	}
	constraint := xsd.IdentityConstraint{
		Fields:             []string{"."},
		Namespaces:         map[string]string{"t": "urn:test"},
		Annotation:         annotation,
		SelectorAnnotation: annotation,
		FieldAnnotations:   []*xsd.Annotation{annotation},
	}
	element := xsd.Element{
		Annotation:          annotation,
		ValueNamespaces:     map[string]string{"value": "urn:value"},
		InlineSimpleType:    &simple,
		IdentityConstraints: []xsd.IdentityConstraint{constraint},
	}
	wildcard := &xsd.Wildcard{
		Namespaces: []string{"urn:test"},
		Annotation: annotation,
	}
	group := &xsd.ModelGroup{
		Compositor: xsd.Sequence,
		Annotation: annotation,
		Particles: []xsd.Particle{{
			Element:    &element,
			Wildcard:   wildcard,
			Annotation: annotation,
		}},
	}
	attribute := xsd.AttributeUse{
		Annotation:       annotation,
		ValueNamespaces:  map[string]string{"value": "urn:value"},
		InlineSimpleType: &simple,
	}
	complex := xsd.ComplexType{
		Annotation:           annotation,
		ContentAnnotation:    annotation,
		DerivationAnnotation: annotation,
		Content:              group,
		Attributes:           []xsd.AttributeUse{attribute},
		AttributeGroupRefs:   []xsd.QName{{Local: "Legacy"}},
		AttributeGroupReferences: []xsd.AttributeGroupReference{{
			Ref:        xsd.QName{Local: "Detailed"},
			Annotation: annotation,
		}},
		AttributeWildcard: wildcard,
	}

	clonedSimple := cloneSimpleType(simple)
	assertAnnotationClone(t, annotation, clonedSimple.Annotation)
	assertAnnotationClone(t, annotation, clonedSimple.VarietyAnnotation)
	assertAnnotationClone(t, annotation, clonedSimple.Facets[0].Annotation)
	clonedSimple.Facets[0].Namespaces["value"] = "urn:mutated"
	if simple.Facets[0].Namespaces["value"] != "urn:value" {
		t.Fatal("cloneSimpleType() retained a facet namespace map alias")
	}
	assertAnnotationClone(t, annotation, clonedSimple.InlineBase.Annotation)
	assertAnnotationClone(t, annotation, clonedSimple.InlineItem.Annotation)
	assertAnnotationClone(t, annotation, clonedSimple.InlineMembers[0].Annotation)

	clonedElement := cloneElement(element)
	clonedElement.ValueNamespaces["value"] = "urn:mutated"
	if element.ValueNamespaces["value"] != "urn:value" {
		t.Fatal("cloneElement() retained a value namespace map alias")
	}
	assertAnnotationClone(t, annotation, clonedElement.Annotation)
	assertAnnotationClone(t, annotation, clonedElement.IdentityConstraints[0].Annotation)
	assertAnnotationClone(t, annotation, clonedElement.IdentityConstraints[0].SelectorAnnotation)
	assertAnnotationClone(t, annotation, clonedElement.IdentityConstraints[0].FieldAnnotations[0])

	clonedComplex := cloneComplexType(complex)
	assertAnnotationClone(t, annotation, clonedComplex.Annotation)
	assertAnnotationClone(t, annotation, clonedComplex.ContentAnnotation)
	assertAnnotationClone(t, annotation, clonedComplex.DerivationAnnotation)
	assertAnnotationClone(t, annotation, clonedComplex.Content.Annotation)
	assertAnnotationClone(t, annotation, clonedComplex.Content.Particles[0].Annotation)
	assertAnnotationClone(t, annotation, clonedComplex.Content.Particles[0].Wildcard.Annotation)
	assertAnnotationClone(t, annotation, clonedComplex.Attributes[0].Annotation)
	clonedComplex.Attributes[0].ValueNamespaces["value"] = "urn:mutated"
	if complex.Attributes[0].ValueNamespaces["value"] != "urn:value" {
		t.Fatal("cloneComplexType() retained an attribute value namespace map alias")
	}
	assertAnnotationClone(t, annotation, clonedComplex.AttributeGroupReferences[0].Annotation)
	assertAnnotationClone(t, annotation, clonedComplex.AttributeWildcard.Annotation)
	clonedComplex.AttributeGroupRefs[0].Local = "Mutated"
	if complex.AttributeGroupRefs[0].Local != "Legacy" {
		t.Fatal("cloneComplexType() retained an attribute group slice alias")
	}
}

func TestCompiledAttributeCloneIsolatesValueNamespaces(t *testing.T) {
	t.Parallel()

	attribute := xsd.Attribute{ValueNamespaces: map[string]string{"value": "urn:value"}}
	clone := cloneAttribute(attribute)
	clone.ValueNamespaces["value"] = "urn:mutated"
	if attribute.ValueNamespaces["value"] != "urn:value" {
		t.Fatal("cloneAttribute() retained a value namespace map alias")
	}
}

func TestCompiledGroupAccessorsDeepCopyMetadata(t *testing.T) {
	t.Parallel()

	annotation := testAnnotation()
	modelName := xsd.QName{Namespace: "urn:test", Local: "Items"}
	attributeName := xsd.QName{Namespace: "urn:test", Local: "Metadata"}
	set := &Set{
		modelGroups: map[xsd.QName]xsd.ModelGroupDefinition{
			modelName: {
				Annotation: annotation,
				Content:    &xsd.ModelGroup{Compositor: xsd.Sequence},
			},
		},
		attributeGroups: map[xsd.QName]xsd.AttributeGroup{
			attributeName: {
				Annotation: annotation,
				AttributeGroupReferences: []xsd.AttributeGroupReference{{
					Ref:        modelName,
					Annotation: annotation,
				}},
			},
		},
	}
	model, ok := set.ModelGroup(modelName)
	if !ok {
		t.Fatal("ModelGroup() did not find the declaration")
	}
	assertAnnotationClone(t, annotation, model.Annotation)
	attributes, ok := set.AttributeGroup(attributeName)
	if !ok {
		t.Fatal("AttributeGroup() did not find the declaration")
	}
	assertAnnotationClone(t, annotation, attributes.Annotation)
	assertAnnotationClone(
		t,
		annotation,
		attributes.AttributeGroupReferences[0].Annotation,
	)
	if _, ok := set.ModelGroup(xsd.QName{Local: "Missing"}); ok {
		t.Fatal("ModelGroup() found a missing declaration")
	}
	if _, ok := set.AttributeGroup(xsd.QName{Local: "Missing"}); ok {
		t.Fatal("AttributeGroup() found a missing declaration")
	}
}

func testAnnotation() *xsd.Annotation {
	return &xsd.Annotation{
		Documentation:  []xsd.Documentation{{Content: "original"}},
		AppInformation: []xsd.AppInfo{{Content: "<original/>"}},
	}
}

func assertAnnotationClone(t *testing.T, original, clone *xsd.Annotation) {
	t.Helper()
	if clone == nil || clone == original {
		t.Fatalf("annotation clone = %#v, original = %#v", clone, original)
	}
	clone.Documentation[0].Content = "mutated"
	clone.AppInformation[0].Content = "<mutated/>"
	if original.Documentation[0].Content != "original" ||
		original.AppInformation[0].Content != "<original/>" {
		t.Fatal("annotation clone retained content slice aliases")
	}
}
