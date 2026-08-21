package xsd

import (
	"context"
	"testing"
)

// Keep this representative parser contract nonparallel so fail-fast mutation
// runs reject broken token handling before scheduling broader parallel suites.
func TestRepresentativeSchemaParsesBeforeParallelCampaigns(t *testing.T) {
	source := []byte(`<schema xmlns="` + Namespace + `">
<simpleType name="Code"><restriction base="string"/></simpleType>
<element name="root"><key name="identity"><selector xpath="."/><field xpath="@id"/></key></element>
</schema>`)
	document, err := Parse(context.Background(), source, ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if document == nil || len(document.SimpleTypes) != 1 ||
		document.SimpleTypes[0].Name != "Code" || document.SimpleTypes[0].Variety != SimpleRestriction ||
		len(document.Elements) != 1 ||
		len(document.Elements[0].IdentityConstraints) != 1 {
		t.Fatalf("Parse() document = %#v", document)
	}
}
