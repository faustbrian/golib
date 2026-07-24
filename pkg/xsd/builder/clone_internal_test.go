package builder

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestBuildPropagatesSerializationFailures(t *testing.T) {
	t.Parallel()

	schema := New("urn:test")
	schema.document.SimpleTypes = []xsd.SimpleType{{Name: "Invalid", Variety: "invalid"}}
	if _, err := schema.Build(context.Background()); err == nil {
		t.Fatal("Build() succeeded")
	}
}

func TestCloneSimpleTypeRecursivelyIsolatesNestedDefinitions(t *testing.T) {
	t.Parallel()

	annotation := &xsd.Annotation{Documentation: []xsd.Documentation{{Content: "original"}}}
	definition := xsd.SimpleType{
		Variety:    xsd.SimpleUnion,
		Annotation: annotation,
		Facets: []xsd.Facet{{
			Namespaces: map[string]string{"value": "urn:value"},
		}},
		InlineMembers: []xsd.SimpleType{{
			Variety: xsd.SimpleList,
			InlineItem: &xsd.SimpleType{
				Variety: xsd.SimpleRestriction,
				InlineBase: &xsd.SimpleType{
					Variety:    xsd.SimpleRestriction,
					Base:       xsd.QName{Namespace: xsd.Namespace, Local: "string"},
					Annotation: annotation,
				},
			},
		}},
	}

	clone := cloneSimpleType(definition)
	definition.Annotation.Documentation[0].Content = "mutated"
	definition.Facets[0].Namespaces["value"] = "urn:mutated"
	definition.InlineMembers[0].InlineItem.InlineBase.Base.Local = "boolean"
	definition.InlineMembers[0].InlineItem.InlineBase.Annotation.
		Documentation[0].Content = "mutated"

	base := clone.InlineMembers[0].InlineItem.InlineBase
	if clone.Annotation.Documentation[0].Content != "original" ||
		clone.Facets[0].Namespaces["value"] != "urn:value" ||
		base.Base.Local != "string" ||
		base.Annotation.Documentation[0].Content != "original" {
		t.Fatalf("cloneSimpleType() retained aliases: %#v", clone)
	}
}

func TestCloneSimpleTypePreservesNestedPointerCycles(t *testing.T) {
	t.Parallel()

	nested := &xsd.SimpleType{Variety: xsd.SimpleRestriction}
	nested.InlineBase = nested
	clone := cloneSimpleType(xsd.SimpleType{
		Variety:    xsd.SimpleList,
		InlineItem: nested,
	})
	if clone.InlineItem == nested || clone.InlineItem.InlineBase != clone.InlineItem {
		t.Fatalf("cloneSimpleType() cycle = %#v", clone.InlineItem)
	}
}

func TestCloneComplexTypeIsolatesAttributeGroupAnnotations(t *testing.T) {
	t.Parallel()

	annotation := &xsd.Annotation{Documentation: []xsd.Documentation{{Content: "original"}}}
	definition := xsd.ComplexType{
		AttributeGroupReferences: []xsd.AttributeGroupReference{{
			Ref:        xsd.QName{Namespace: "urn:test", Local: "Metadata"},
			Annotation: annotation,
		}},
	}
	clone := cloneComplexType(definition)
	annotation.Documentation[0].Content = "mutated"
	if clone.AttributeGroupReferences[0].Annotation.Documentation[0].Content != "original" {
		t.Fatalf("cloneComplexType() retained an annotation alias: %#v", clone)
	}
}

func TestCloneComplexTypeIsolatesSimpleContentRestrictions(t *testing.T) {
	t.Parallel()

	definition := xsd.ComplexType{
		InlineSimpleType: &xsd.SimpleType{
			Variety: xsd.SimpleRestriction,
			Base:    xsd.QName{Namespace: xsd.Namespace, Local: "string"},
		},
		SimpleFacets: []xsd.Facet{{
			Kind:       xsd.FacetEnumeration,
			Value:      "p:value",
			Namespaces: map[string]string{"p": "urn:value"},
		}},
	}
	clone := cloneComplexType(definition)
	definition.InlineSimpleType.Base.Local = "boolean"
	definition.SimpleFacets[0].Namespaces["p"] = "urn:mutated"
	if clone.InlineSimpleType.Base.Local != "string" ||
		clone.SimpleFacets[0].Namespaces["p"] != "urn:value" {
		t.Fatalf("cloneComplexType() retained simple-content aliases: %#v", clone)
	}
}

func TestCloneElementIsolatesValueNamespaces(t *testing.T) {
	t.Parallel()

	element := xsd.Element{ValueNamespaces: map[string]string{"value": "urn:value"}}
	clone := cloneElement(element)
	clone.ValueNamespaces["value"] = "urn:mutated"
	if element.ValueNamespaces["value"] != "urn:value" {
		t.Fatal("cloneElement() retained a value namespace map alias")
	}
}

func TestCloneAttributeUsesIsolatesValueNamespaces(t *testing.T) {
	t.Parallel()

	attributes := []xsd.AttributeUse{{
		ValueNamespaces: map[string]string{"value": "urn:value"},
	}}
	clone := cloneAttributeUses(attributes)
	clone[0].ValueNamespaces["value"] = "urn:mutated"
	if attributes[0].ValueNamespaces["value"] != "urn:value" {
		t.Fatal("cloneAttributeUses() retained a value namespace map alias")
	}
}
