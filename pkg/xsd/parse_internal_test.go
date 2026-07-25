package xsd

import (
	"context"
	"encoding/xml"
	"errors"
	"strings"
	"testing"
)

func TestParseInlineSimpleTypesBranches(t *testing.T) {
	t.Parallel()

	decoder, start := decoderAtStart(t, `<list xmlns="`+Namespace+`">`+
		`<annotation><documentation>item</documentation></annotation>`+
		`<simpleType><restriction base="string"/></simpleType></list>`)
	types, annotation, err := parseInlineSimpleTypes(
		decoder,
		start,
		map[string]string{"": Namespace},
	)
	if err != nil {
		t.Fatalf("parseInlineSimpleTypes() error = %v", err)
	}
	if len(types) != 1 || annotation == nil ||
		len(annotation.Documentation) != 1 {
		t.Fatalf("types/annotation = %#v, %#v", types, annotation)
	}

	for _, test := range []struct {
		name string
		xml  string
		want error
	}{
		{name: "directive", xml: `<list xmlns="` + Namespace + `"><!DOCTYPE x></list>`, want: ErrDTDForbidden},
		{name: "truncated", xml: `<list xmlns="` + Namespace + `">`},
		{name: "broken annotation", xml: `<list xmlns="` + Namespace + `"><annotation>`},
		{name: "broken simple type", xml: `<list xmlns="` + Namespace + `"><simpleType>`},
		{name: "broken skipped element", xml: `<list xmlns="` + Namespace + `"><other>`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decoder, start := decoderAtStart(t, test.xml)
			_, _, err := parseInlineSimpleTypes(decoder, start, nil)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("parseInlineSimpleTypes() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseSimpleTypeBranches(t *testing.T) {
	t.Parallel()

	decoder, start := decoderAtStart(t, `<simpleType xmlns="`+Namespace+`" xmlns:f="urn:foreign" name="Code" final="restriction" f:ignored="yes">`+
		`<annotation id="code"/><restriction base="string"><minLength value="1"/></restriction></simpleType>`)
	definition, err := parseSimpleType(
		decoder,
		start,
		map[string]string{"": Namespace},
	)
	if err != nil || definition.Name != "Code" ||
		definition.Variety != SimpleRestriction || definition.Annotation == nil ||
		len(definition.Facets) != 1 {
		t.Fatalf("parseSimpleType() = %#v, %v", definition, err)
	}

	for _, test := range []struct {
		name string
		xml  string
		want error
	}{
		{name: "final", xml: `<simpleType xmlns="` + Namespace + `" final="invalid"/>`},
		{name: "directive", xml: `<simpleType xmlns="` + Namespace + `"><!DOCTYPE x></simpleType>`, want: ErrDTDForbidden},
		{name: "truncated", xml: `<simpleType xmlns="` + Namespace + `">`},
		{name: "annotation", xml: `<simpleType xmlns="` + Namespace + `"><annotation>`},
		{name: "missing variety", xml: `<simpleType xmlns="` + Namespace + `"/>`},
		{name: "restriction base", xml: `<simpleType xmlns="` + Namespace + `"><restriction base="missing:Base"/></simpleType>`},
		{name: "restriction body", xml: `<simpleType xmlns="` + Namespace + `"><restriction>`},
		{name: "list item", xml: `<simpleType xmlns="` + Namespace + `"><list itemType="missing:Item"/></simpleType>`},
		{name: "list body", xml: `<simpleType xmlns="` + Namespace + `"><list>`},
		{
			name: "multiple list items",
			xml: `<simpleType xmlns="` + Namespace + `"><list>` +
				`<simpleType><restriction base="string"/></simpleType>` +
				`<simpleType><restriction base="string"/></simpleType>` +
				`</list></simpleType>`,
		},
		{name: "union member", xml: `<simpleType xmlns="` + Namespace + `"><union memberTypes="missing:Member"/></simpleType>`},
		{name: "union body", xml: `<simpleType xmlns="` + Namespace + `"><union>`},
		{name: "skipped child", xml: `<simpleType xmlns="` + Namespace + `"><unknown>`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decoder, start := decoderAtStart(t, test.xml)
			_, err := parseSimpleType(decoder, start, nil)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("parseSimpleType() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseEntryBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		source  string
		options ParseOptions
		want    error
	}{
		{name: "negative limit", source: `<schema xmlns="` + Namespace + `"/>`, options: ParseOptions{MaxDocumentBytes: -1}},
		{name: "negative depth", source: `<schema xmlns="` + Namespace + `"/>`, options: ParseOptions{MaxDepth: -1}},
		{name: "negative elements", source: `<schema xmlns="` + Namespace + `"/>`, options: ParseOptions{MaxElements: -1}},
		{name: "byte limit", source: `<schema xmlns="` + Namespace + `"/>`, options: ParseOptions{MaxDocumentBytes: 1}, want: ErrLimitExceeded},
		{name: "depth limit", source: `<schema xmlns="` + Namespace + `"><annotation/></schema>`, options: ParseOptions{MaxDepth: 1}, want: ErrLimitExceeded},
		{name: "element limit", source: `<schema xmlns="` + Namespace + `"><annotation/></schema>`, options: ParseOptions{MaxElements: 1}, want: ErrLimitExceeded},
		{name: "empty document", want: ErrNotSchema},
		{name: "malformed XML", source: `<schema xmlns="` + Namespace + `">`},
		{name: "DTD", source: `<!DOCTYPE schema><schema xmlns="` + Namespace + `"/>`, want: ErrDTDForbidden},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(context.Background(), []byte(test.source), test.options)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("Parse() error = %v, want %v", err, test.want)
			}
		})
	}
	if _, err := Parse(
		context.Background(),
		[]byte(`<schema xmlns="`+Namespace+`"><annotation/></schema>`),
		ParseOptions{MaxDepth: 2, MaxElements: 2},
	); err != nil {
		t.Fatalf("Parse(exact structural limits) error = %v", err)
	}
}

func TestParseDocumentPropagatesComponentErrors(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		body  string
		isDTD bool
	}{
		{name: "annotation", body: `<annotation>`},
		{name: "import location", body: `<import schemaLocation="%"/>`},
		{name: "reference annotation", body: `<include schemaLocation="child.xsd"><annotation>`},
		{name: "redefinition", body: `<redefine schemaLocation="child.xsd"><simpleType>`},
		{name: "notation", body: `<notation name="n" public="p"><annotation>`},
		{name: "element declaration", body: `<element type="missing:Type"/>`},
		{name: "element body", body: `<element name="value"><simpleType>`},
		{name: "attribute declaration", body: `<attribute type="missing:Type"/>`},
		{name: "attribute body", body: `<attribute name="value"><simpleType>`},
		{name: "simple type", body: `<simpleType name="Value"><restriction>`},
		{name: "complex type", body: `<complexType name="Value"><sequence>`},
		{name: "model group", body: `<group name="Items"><sequence>`},
		{name: "attribute group", body: `<attributeGroup name="Items"><attribute>`},
		{name: "unknown component", body: `<unknown>`},
		{name: "foreign component", body: `<foreign xmlns="urn:foreign">`},
		{name: "element directive", body: `<element name="value"><!DOCTYPE value></element>`, isDTD: true},
		{name: "identity directive", body: `<element name="value"><key name="key"><!DOCTYPE key></key></element>`, isDTD: true},
		{name: "model group directive", body: `<group name="Items"><!DOCTYPE group></group>`, isDTD: true},
		{name: "attribute group directive", body: `<attributeGroup name="Items"><!DOCTYPE group></attributeGroup>`, isDTD: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			schema := `<schema xmlns="` + Namespace + `">` + test.body + `</schema>`
			decoder, start := decoderAtStart(t, schema)
			_, err := parseDocument(decoder, start, "test.xsd")
			if err == nil {
				t.Fatal("Parse() succeeded")
			}
			if test.isDTD && !errors.Is(err, ErrDTDForbidden) {
				t.Fatalf("Parse() error = %v, want ErrDTDForbidden", err)
			}
		})
	}
}

func TestParseRejectsUnknownAttributesAtGrammarBoundaries(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`<notation name="n" public="p" unknown="x"/>`,
		`<element name="root"><key name="k" unknown="x"/></element>`,
		`<element name="root"><key name="k"><selector xpath="." unknown="x"/></key></element>`,
		`<attribute name="value" unknown="x"/>`,
		`<simpleType name="Value" unknown="x"/>`,
		`<simpleType name="Value"><restriction base="string" unknown="x"/></simpleType>`,
		`<simpleType name="Value"><list itemType="string" unknown="x"/></simpleType>`,
		`<simpleType name="Value"><union memberTypes="string" unknown="x"/></simpleType>`,
		`<simpleType name="Value"><restriction base="string"><length value="1" unknown="x"/></restriction></simpleType>`,
		`<complexType name="Value" unknown="x"/>`,
		`<complexType name="Value"><complexContent unknown="x"/></complexType>`,
		`<complexType name="Value"><complexContent><extension base="anyType" unknown="x"/></complexContent></complexType>`,
		`<complexType name="Value"><sequence unknown="x"/></complexType>`,
		`<complexType name="Value"><group ref="Items" unknown="x"/></complexType>`,
		`<group name="Items" unknown="x"><sequence/></group>`,
		`<attributeGroup name="Items" unknown="x"/>`,
		`<complexType name="Value"><anyAttribute unknown="x"/></complexType>`,
		`<complexType name="Value"><attributeGroup ref="Items" unknown="x"/></complexType>`,
		`<complexType name="Value"><attribute name="code" unknown="x"/></complexType>`,
		`<include schemaLocation="child.xsd" unknown="x"/>`,
		`<annotation unknown="x"/>`,
		`<annotation><appinfo unknown="x"/></annotation>`,
		`<annotation><documentation unknown="x"/></annotation>`,
	} {
		source := `<schema xmlns="` + Namespace + `">` + body + `</schema>`
		if _, err := Parse(context.Background(), []byte(source), ParseOptions{}); err == nil {
			t.Fatalf("Parse() accepted unknown attribute in %s", body)
		}
	}

	decoder, start := decoderAtStart(t, `<redefine xmlns="`+Namespace+`" unknown="x"/>`)
	if _, err := parseRedefinition(
		decoder,
		start,
		SchemaReference{Kind: ReferenceRedefine},
		nil,
	); err == nil {
		t.Fatal("parseRedefinition() accepted an unknown attribute")
	}
}

func TestParseContentDerivationBranches(t *testing.T) {
	t.Parallel()

	decoder, start := decoderAtStart(t, `<complexContent xmlns="`+Namespace+`" mixed="true">`+
		`<annotation><documentation>content</documentation></annotation>`+
		`<extension xmlns:t="urn:types" base="t:Base">`+
		`<annotation><documentation>derivation</documentation></annotation>`+
		`<group ref="t:Items"><annotation/></group>`+
		`<attribute name="code" type="string"><annotation/></attribute>`+
		`<attributeGroup ref="t:Metadata"><annotation/></attributeGroup>`+
		`<anyAttribute namespace="##other"><annotation/></anyAttribute>`+
		`</extension></complexContent>`)
	definition := ComplexType{}
	err := parseContentDerivation(
		decoder,
		start,
		&definition,
		map[string]string{"": Namespace, "t": "urn:types"},
	)
	if err != nil {
		t.Fatalf("parseContentDerivation() error = %v", err)
	}
	if !definition.MixedSet || !definition.Mixed ||
		definition.Derivation != DerivationExtension ||
		definition.Base != (QName{Namespace: "urn:types", Local: "Base"}) ||
		definition.ContentAnnotation == nil ||
		definition.DerivationAnnotation == nil || definition.Content == nil ||
		len(definition.Attributes) != 1 ||
		len(definition.AttributeGroupRefs) != 1 ||
		definition.AttributeWildcard == nil {
		t.Fatalf("definition = %#v", definition)
	}

	for _, test := range []struct {
		name string
		xml  string
		want error
	}{
		{name: "invalid mixed", xml: `<complexContent xmlns="` + Namespace + `" mixed="maybe"/>`},
		{name: "directive", xml: `<complexContent xmlns="` + Namespace + `"><!DOCTYPE x></complexContent>`, want: ErrDTDForbidden},
		{name: "truncated", xml: `<complexContent xmlns="` + Namespace + `">`},
		{name: "broken annotation", xml: `<complexContent xmlns="` + Namespace + `"><annotation>`},
		{name: "invalid base", xml: `<complexContent xmlns="` + Namespace + `"><extension base="missing:Base"/></complexContent>`},
		{name: "broken derivation", xml: `<complexContent xmlns="` + Namespace + `"><restriction>`},
		{name: "broken skipped element", xml: `<complexContent xmlns="` + Namespace + `"><other>`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decoder, start := decoderAtStart(t, test.xml)
			err := parseContentDerivation(decoder, start, &ComplexType{}, nil)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("parseContentDerivation() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseRedefinitionPropagatesAnnotationErrors(t *testing.T) {
	t.Parallel()

	decoder, start := decoderAtStart(t, `<redefine xmlns="`+Namespace+`" schemaLocation="base.xsd">`+
		`<annotation unknown="value"/></redefine>`)
	if _, err := parseRedefinition(
		decoder,
		start,
		SchemaReference{Kind: ReferenceRedefine, URI: "base.xsd"},
		nil,
	); err == nil {
		t.Fatal("parseRedefinition() accepted an invalid annotation")
	}
}

func TestParseComplexTypeBranches(t *testing.T) {
	t.Parallel()

	decoder, start := decoderAtStart(t, `<complexType xmlns="`+Namespace+`" xmlns:f="urn:foreign"`+
		` name="Record" abstract="true" mixed="false" block="extension" final="restriction" f:ignored="yes">`+
		`<group ref="Items"><annotation/></group>`+
		`<attribute name="code"><annotation/></attribute>`+
		`<attributeGroup ref="Metadata"><annotation/></attributeGroup>`+
		`<anyAttribute namespace="##other"><annotation/></anyAttribute>`+
		`</complexType>`)
	definition, err := parseComplexType(
		decoder,
		start,
		map[string]string{"": Namespace},
	)
	if err != nil {
		t.Fatalf("parseComplexType() error = %v", err)
	}
	if definition.Name != "Record" || !definition.Abstract ||
		!definition.MixedSet || definition.Mixed || definition.Content == nil ||
		definition.AttributeWildcard == nil || len(definition.Attributes) != 1 ||
		len(definition.AttributeGroupRefs) != 1 {
		t.Fatalf("definition = %#v", definition)
	}

	for _, test := range []struct {
		name string
		xml  string
		want error
	}{
		{name: "abstract", xml: `<complexType xmlns="` + Namespace + `" abstract="invalid"/>`},
		{name: "mixed", xml: `<complexType xmlns="` + Namespace + `" mixed="invalid"/>`},
		{name: "block", xml: `<complexType xmlns="` + Namespace + `" block="invalid"/>`},
		{name: "final", xml: `<complexType xmlns="` + Namespace + `" final="invalid"/>`},
		{name: "directive", xml: `<complexType xmlns="` + Namespace + `"><!DOCTYPE x></complexType>`, want: ErrDTDForbidden},
		{name: "truncated", xml: `<complexType xmlns="` + Namespace + `">`},
		{name: "annotation", xml: `<complexType xmlns="` + Namespace + `"><annotation>`},
		{name: "group reference", xml: `<complexType xmlns="` + Namespace + `"><group ref="missing:Items"/></complexType>`},
		{name: "group annotation", xml: `<complexType xmlns="` + Namespace + `"><group ref="Items"><annotation>`},
		{name: "wildcard", xml: `<complexType xmlns="` + Namespace + `"><anyAttribute processContents="invalid"/></complexType>`},
		{name: "wildcard annotation", xml: `<complexType xmlns="` + Namespace + `"><anyAttribute><annotation>`},
		{name: "content derivation", xml: `<complexType xmlns="` + Namespace + `"><complexContent>`},
		{name: "model group body", xml: `<complexType xmlns="` + Namespace + `"><sequence><element>`},
		{name: "model group occurrence", xml: `<complexType xmlns="` + Namespace + `"><sequence minOccurs="invalid"/></complexType>`},
		{name: "attribute declaration", xml: `<complexType xmlns="` + Namespace + `"><attribute type="missing:Type"/></complexType>`},
		{name: "attribute body", xml: `<complexType xmlns="` + Namespace + `"><attribute name="value">`},
		{name: "attribute group reference", xml: `<complexType xmlns="` + Namespace + `"><attributeGroup ref="missing:Group"/></complexType>`},
		{name: "attribute group annotation", xml: `<complexType xmlns="` + Namespace + `"><attributeGroup ref="Group"><annotation>`},
		{name: "skipped child", xml: `<complexType xmlns="` + Namespace + `"><unknown>`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decoder, start := decoderAtStart(t, test.xml)
			_, err := parseComplexType(decoder, start, nil)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("parseComplexType() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseDerivationBodyRejectsInvalidChildren(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		xml  string
		want error
	}{
		{name: "directive", xml: `<extension xmlns="` + Namespace + `"><!DOCTYPE x></extension>`, want: ErrDTDForbidden},
		{name: "truncated", xml: `<extension xmlns="` + Namespace + `">`},
		{name: "annotation", xml: `<extension xmlns="` + Namespace + `"><annotation>`},
		{name: "group reference", xml: `<extension xmlns="` + Namespace + `"><group ref="missing:Group"/></extension>`},
		{name: "group annotation", xml: `<extension xmlns="` + Namespace + `"><group ref="Group"><annotation>`},
		{name: "wildcard", xml: `<extension xmlns="` + Namespace + `"><anyAttribute processContents="invalid"/></extension>`},
		{name: "wildcard annotation", xml: `<extension xmlns="` + Namespace + `"><anyAttribute><annotation>`},
		{name: "model group", xml: `<extension xmlns="` + Namespace + `"><sequence minOccurs="invalid"/></extension>`},
		{name: "model group body", xml: `<extension xmlns="` + Namespace + `"><sequence><element>`},
		{name: "attribute", xml: `<extension xmlns="` + Namespace + `"><attribute type="missing:Type"/></extension>`},
		{name: "attribute body", xml: `<extension xmlns="` + Namespace + `"><attribute name="value">`},
		{name: "attribute group", xml: `<extension xmlns="` + Namespace + `"><attributeGroup ref="missing:Group"/></extension>`},
		{name: "attribute group annotation", xml: `<extension xmlns="` + Namespace + `"><attributeGroup ref="Group"><annotation>`},
		{name: "skipped child", xml: `<extension xmlns="` + Namespace + `"><other>`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decoder, start := decoderAtStart(t, test.xml)
			err := parseDerivationBody(decoder, start, &ComplexType{}, nil)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("parseDerivationBody() error = %v, want %v", err, test.want)
			}
		})
	}

	t.Run("multiple simple-content inline types", func(t *testing.T) {
		t.Parallel()
		decoder, start := decoderAtStart(t, `<restriction xmlns="`+Namespace+`">`+
			`<simpleType><restriction base="string"/></simpleType>`+
			`<simpleType><restriction base="string"/></simpleType>`+
			`</restriction>`)
		definition := ComplexType{
			SimpleContent: true,
			Derivation:    DerivationRestriction,
		}
		if err := parseDerivationBody(decoder, start, &definition, nil); err == nil {
			t.Fatal("parseDerivationBody() accepted multiple inline simple types")
		}
	})
}

func TestParseCompositorDecisionTable(t *testing.T) {
	t.Parallel()

	for lexical, want := range map[string]Compositor{
		"sequence": Sequence,
		"choice":   Choice,
		"all":      All,
	} {
		got, ok := parseCompositor(lexical)
		if !ok || got != want {
			t.Fatalf("parseCompositor(%q) = %q, %t", lexical, got, ok)
		}
	}
	if got, ok := parseCompositor("unknown"); ok || got != "" {
		t.Fatalf("parseCompositor(unknown) = %q, %t", got, ok)
	}
}

func TestParseFacetKindDecisionTable(t *testing.T) {
	t.Parallel()

	for _, kind := range []FacetKind{
		FacetLength,
		FacetMinLength,
		FacetMaxLength,
		FacetPattern,
		FacetEnumeration,
		FacetWhiteSpace,
		FacetMaxInclusive,
		FacetMaxExclusive,
		FacetMinInclusive,
		FacetMinExclusive,
		FacetTotalDigits,
		FacetFractionDigits,
	} {
		got, ok := parseFacetKind(string(kind))
		if !ok || got != kind {
			t.Fatalf("parseFacetKind(%q) = %q, %t", kind, got, ok)
		}
	}
	if got, ok := parseFacetKind("unknown"); ok || got != "" {
		t.Fatalf("parseFacetKind(unknown) = %q, %t", got, ok)
	}
}

func TestParseRestrictionFacetBranches(t *testing.T) {
	t.Parallel()

	decoder, start := decoderAtStart(t, `<restriction xmlns="`+Namespace+`">`+
		`<annotation id="restriction"/>`+
		`<simpleType><restriction base="string"/></simpleType>`+
		`<minLength xmlns:f="urn:foreign" value="1" fixed="true" f:ignored="yes"><annotation/></minLength>`+
		`</restriction>`)
	definition := SimpleType{}
	err := parseRestrictionFacets(
		decoder,
		start,
		&definition,
		map[string]string{"": Namespace},
	)
	if err != nil {
		t.Fatalf("parseRestrictionFacets() error = %v", err)
	}
	if definition.VarietyAnnotation == nil || definition.InlineBase == nil ||
		len(definition.Facets) != 1 ||
		definition.Facets[0].Kind != FacetMinLength ||
		definition.Facets[0].Value != "1" || !definition.Facets[0].Fixed ||
		definition.Facets[0].Annotation == nil {
		t.Fatalf("definition = %#v", definition)
	}

	for _, test := range []struct {
		name string
		xml  string
		want error
	}{
		{name: "directive", xml: `<restriction xmlns="` + Namespace + `"><!DOCTYPE x></restriction>`, want: ErrDTDForbidden},
		{name: "truncated", xml: `<restriction xmlns="` + Namespace + `">`},
		{name: "annotation", xml: `<restriction xmlns="` + Namespace + `"><annotation>`},
		{name: "simple type", xml: `<restriction xmlns="` + Namespace + `"><simpleType>`},
		{
			name: "multiple inline bases",
			xml: `<restriction xmlns="` + Namespace + `">` +
				`<simpleType><restriction base="string"/></simpleType>` +
				`<simpleType><restriction base="string"/></simpleType>` +
				`</restriction>`,
		},
		{name: "fixed value", xml: `<restriction xmlns="` + Namespace + `"><length fixed="invalid"/></restriction>`},
		{name: "facet annotation", xml: `<restriction xmlns="` + Namespace + `"><length><annotation>`},
		{name: "skipped child", xml: `<restriction xmlns="` + Namespace + `"><unknown>`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decoder, start := decoderAtStart(t, test.xml)
			err := parseRestrictionFacets(decoder, start, &SimpleType{}, nil)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("parseRestrictionFacets() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseModelGroupBranches(t *testing.T) {
	t.Parallel()

	decoder, start := decoderAtStart(t, `<sequence xmlns="`+Namespace+`" xmlns:t="urn:test">`+
		`<annotation id="group"/>`+
		`<element name="value" minOccurs="0"><annotation/></element>`+
		`<any namespace="##other" processContents="lax"><annotation/></any>`+
		`<group ref="t:Items"><annotation/></group>`+
		`<choice><element name="alternative"/></choice>`+
		`</sequence>`)
	group, err := parseModelGroup(
		decoder,
		start,
		Sequence,
		map[string]string{"": Namespace, "t": "urn:test"},
	)
	if err != nil {
		t.Fatalf("parseModelGroup() error = %v", err)
	}
	if group.Annotation == nil || len(group.Particles) != 4 ||
		group.Particles[0].Element == nil ||
		group.Particles[1].Wildcard == nil ||
		group.Particles[2].GroupRef.Local != "Items" ||
		group.Particles[3].Group == nil {
		t.Fatalf("group = %#v", group)
	}

	for _, test := range []struct {
		name string
		xml  string
		want error
	}{
		{name: "directive", xml: `<sequence xmlns="` + Namespace + `"><!DOCTYPE x></sequence>`, want: ErrDTDForbidden},
		{name: "truncated", xml: `<sequence xmlns="` + Namespace + `">`},
		{name: "annotation", xml: `<sequence xmlns="` + Namespace + `"><annotation>`},
		{name: "element attributes", xml: `<sequence xmlns="` + Namespace + `"><element type="missing:Type"/></sequence>`},
		{name: "element occurrence", xml: `<sequence xmlns="` + Namespace + `"><element minOccurs="invalid"/></sequence>`},
		{name: "element body", xml: `<sequence xmlns="` + Namespace + `"><element>`},
		{name: "wildcard occurrence", xml: `<sequence xmlns="` + Namespace + `"><any minOccurs="invalid"/></sequence>`},
		{name: "wildcard policy", xml: `<sequence xmlns="` + Namespace + `"><any processContents="invalid"/></sequence>`},
		{name: "wildcard annotation", xml: `<sequence xmlns="` + Namespace + `"><any><annotation>`},
		{name: "group occurrence", xml: `<sequence xmlns="` + Namespace + `"><group minOccurs="invalid"/></sequence>`},
		{name: "group attribute", xml: `<sequence xmlns="` + Namespace + `"><group ref="Items" unknown="x"/></sequence>`},
		{name: "group reference", xml: `<sequence xmlns="` + Namespace + `"><group ref="missing:Items"/></sequence>`},
		{name: "group annotation", xml: `<sequence xmlns="` + Namespace + `"><group ref="Items"><annotation>`},
		{name: "nested occurrence", xml: `<sequence xmlns="` + Namespace + `"><choice minOccurs="invalid"/></sequence>`},
		{name: "nested body", xml: `<sequence xmlns="` + Namespace + `"><choice>`},
		{name: "skipped child", xml: `<sequence xmlns="` + Namespace + `"><unknown>`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decoder, start := decoderAtStart(t, test.xml)
			_, err := parseModelGroup(decoder, start, Sequence, nil)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("parseModelGroup() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSetModelGroupOccurrenceBranches(t *testing.T) {
	t.Parallel()

	group := ModelGroup{}
	err := setModelGroupOccurrence(&group, xml.StartElement{Attr: []xml.Attr{
		{Name: xml.Name{Local: "minOccurs"}, Value: "0"},
		{Name: xml.Name{Local: "maxOccurs"}, Value: "unbounded"},
	}})
	if err != nil || !group.OccursSet || group.MinOccurs != 0 ||
		!group.Unbounded {
		t.Fatalf("setModelGroupOccurrence() = %#v, %v", group, err)
	}
	if err := setModelGroupOccurrence(&group, xml.StartElement{Attr: []xml.Attr{{
		Name: xml.Name{Local: "minOccurs"}, Value: "invalid",
	}}}); err == nil {
		t.Fatal("setModelGroupOccurrence() accepted an invalid occurrence")
	}
}

func TestParseRedefinitionBranches(t *testing.T) {
	t.Parallel()

	decoder, start := decoderAtStart(t, `<redefine xmlns="`+Namespace+`">`+
		`<annotation><documentation>replacement</documentation></annotation>`+
		`<simpleType name="Code"><restriction base="string"/></simpleType>`+
		`<complexType name="Record"/>`+
		`<group name="Items"><sequence/></group>`+
		`<attributeGroup name="Metadata"/>`+
		`</redefine>`)
	got, err := parseRedefinition(
		decoder,
		start,
		SchemaReference{Kind: ReferenceRedefine},
		map[string]string{"": Namespace},
	)
	if err != nil {
		t.Fatalf("parseRedefinition() error = %v", err)
	}
	if got.Reference.Annotation == nil || len(got.SimpleTypes) != 1 ||
		len(got.ComplexTypes) != 1 || len(got.ModelGroups) != 1 ||
		len(got.AttributeGroups) != 1 {
		t.Fatalf("redefinition = %#v", got)
	}

	for _, test := range []struct {
		name string
		xml  string
		want error
	}{
		{name: "directive", xml: `<redefine xmlns="` + Namespace + `"><!DOCTYPE x></redefine>`, want: ErrDTDForbidden},
		{name: "truncated", xml: `<redefine xmlns="` + Namespace + `">`},
		{name: "foreign child", xml: `<redefine xmlns="` + Namespace + `"><foreign xmlns="urn:foreign">`},
		{name: "annotation", xml: `<redefine xmlns="` + Namespace + `"><annotation>`},
		{name: "element attributes", xml: `<redefine xmlns="` + Namespace + `"><element type="missing:Type"/></redefine>`},
		{name: "element body", xml: `<redefine xmlns="` + Namespace + `"><element name="value">`},
		{name: "attribute attributes", xml: `<redefine xmlns="` + Namespace + `"><attribute type="missing:Type"/></redefine>`},
		{name: "attribute body", xml: `<redefine xmlns="` + Namespace + `"><attribute name="value">`},
		{name: "simple type", xml: `<redefine xmlns="` + Namespace + `"><simpleType>`},
		{name: "complex type", xml: `<redefine xmlns="` + Namespace + `"><complexType>`},
		{name: "model group", xml: `<redefine xmlns="` + Namespace + `"><group>`},
		{name: "attribute group", xml: `<redefine xmlns="` + Namespace + `"><attributeGroup>`},
		{name: "unknown child", xml: `<redefine xmlns="` + Namespace + `"><unknown>`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decoder, start := decoderAtStart(t, test.xml)
			_, err := parseRedefinition(decoder, start, SchemaReference{}, nil)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("parseRedefinition() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSchemaReferenceResolutionBranches(t *testing.T) {
	t.Parallel()

	const xmlNamespace = "http://www.w3.org/XML/1998/namespace"
	start := xml.StartElement{Attr: []xml.Attr{
		{Name: xml.Name{Local: "namespace"}, Value: "urn:imported"},
		{Name: xml.Name{Local: "schemaLocation"}, Value: "types.xsd"},
		{Name: xml.Name{Space: xmlNamespace, Local: "base"}, Value: "schemas/"},
	}}
	reference, err := parseSchemaReference(
		ReferenceImport,
		"https://example.test/root.xsd",
		start,
	)
	if err != nil {
		t.Fatalf("parseSchemaReference() error = %v", err)
	}
	if reference.Namespace != "urn:imported" ||
		reference.Location != "types.xsd" ||
		reference.URI != "https://example.test/schemas/types.xsd" {
		t.Fatalf("reference = %#v", reference)
	}
	withoutLocation, err := parseSchemaReference(ReferenceImport, "", xml.StartElement{})
	if err != nil || withoutLocation.URI != "" {
		t.Fatalf("locationless reference = %#v, %v", withoutLocation, err)
	}

	for _, test := range []struct {
		name string
		base string
		attr xml.Attr
	}{
		{
			name: "invalid XML base",
			base: "https://example.test/root.xsd",
			attr: xml.Attr{Name: xml.Name{Space: xmlNamespace, Local: "base"}, Value: "%"},
		},
		{
			name: "invalid location",
			attr: xml.Attr{Name: xml.Name{Local: "schemaLocation"}, Value: "%"},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSchemaReference(
				ReferenceInclude,
				test.base,
				xml.StartElement{Attr: []xml.Attr{test.attr}},
			)
			if err == nil {
				t.Fatal("parseSchemaReference() succeeded")
			}
		})
	}

	if _, err := resolveURI("%", "child.xsd"); err == nil {
		t.Fatal("resolveURI() accepted an invalid base URI")
	}
	if got, err := resolveURI("", "child.xsd"); err != nil || got != "child.xsd" {
		t.Fatalf("resolveURI() = %q, %v", got, err)
	}
}

func TestReferenceKindDecisionTable(t *testing.T) {
	t.Parallel()

	for lexical, want := range map[string]ReferenceKind{
		"include":  ReferenceInclude,
		"import":   ReferenceImport,
		"redefine": ReferenceRedefine,
	} {
		got, ok := referenceKind(lexical)
		if !ok || got != want {
			t.Fatalf("referenceKind(%q) = %q, %t", lexical, got, ok)
		}
	}
	if got, ok := referenceKind("unknown"); ok || got != "" {
		t.Fatalf("referenceKind(unknown) = %q, %t", got, ok)
	}
}

func TestAnnotationParsingBranches(t *testing.T) {
	t.Parallel()

	decoder, start := decoderAtStart(t, `<annotation xmlns="`+Namespace+`" id="notes">`+
		`<documentation source="urn:docs" xml:lang="en"> Read <b xmlns="urn:doc">this</b> </documentation>`+
		`<appinfo source="urn:tool"><tool xmlns="urn:tool"/></appinfo>`+
		`</annotation>`)
	annotation, err := parseAnnotation(decoder, start)
	if err != nil {
		t.Fatalf("parseAnnotation() error = %v", err)
	}
	if annotation.ID != "notes" || len(annotation.Documentation) != 1 ||
		annotation.Documentation[0].Content != "Read this" ||
		annotation.Documentation[0].Source != "urn:docs" ||
		annotation.Documentation[0].Language != "en" ||
		len(annotation.AppInformation) != 1 {
		t.Fatalf("annotation = %#v", annotation)
	}

	for _, test := range []struct {
		name string
		xml  string
		want error
	}{
		{name: "directive", xml: `<annotation xmlns="` + Namespace + `"><!DOCTYPE x></annotation>`, want: ErrDTDForbidden},
		{name: "truncated", xml: `<annotation xmlns="` + Namespace + `">`},
		{name: "documentation", xml: `<annotation xmlns="` + Namespace + `"><documentation>`},
		{name: "app info", xml: `<annotation xmlns="` + Namespace + `"><appinfo>`},
		{name: "unknown", xml: `<annotation xmlns="` + Namespace + `"><unknown>`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decoder, start := decoderAtStart(t, test.xml)
			_, err := parseAnnotation(decoder, start)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("parseAnnotation() error = %v, want %v", err, test.want)
			}
		})
	}

	for _, markup := range []string{"plain <b>text</b>", "<!DOCTYPE unsafe>", "<broken>"} {
		text, err := documentationText(markup)
		if markup == "plain <b>text</b>" {
			if err != nil || text != "plain text" {
				t.Fatalf("documentationText(%q) = %q, %v", markup, text, err)
			}
		} else if err == nil {
			t.Fatalf("documentationText(%q) succeeded", markup)
		}
	}
}

func TestAnnotationChildrenAndAttributeBodyBranches(t *testing.T) {
	t.Parallel()

	decoder, start := decoderAtStart(t, `<element xmlns="`+Namespace+`">`+
		`<annotation id="child"/></element>`)
	annotation, err := parseAnnotationChildren(decoder, start)
	if err != nil || annotation == nil || annotation.ID != "child" {
		t.Fatalf("parseAnnotationChildren() = %#v, %v", annotation, err)
	}

	decoder, start = decoderAtStart(t, `<attribute xmlns="`+Namespace+`">`+
		`<annotation id="attribute"/>`+
		`<simpleType><restriction base="string"/></simpleType>`+
		`</attribute>`)
	inline, annotation, err := parseAttributeBody(
		decoder,
		start,
		map[string]string{"": Namespace},
	)
	if err != nil || inline == nil || annotation == nil ||
		annotation.ID != "attribute" {
		t.Fatalf("parseAttributeBody() = %#v, %#v, %v", inline, annotation, err)
	}

	for _, test := range []struct {
		name      string
		xml       string
		attribute bool
		want      error
	}{
		{name: "children directive", xml: `<element xmlns="` + Namespace + `"><!DOCTYPE x></element>`, want: ErrDTDForbidden},
		{name: "children truncated", xml: `<element xmlns="` + Namespace + `">`},
		{name: "children annotation", xml: `<element xmlns="` + Namespace + `"><annotation>`},
		{name: "children skipped", xml: `<element xmlns="` + Namespace + `"><unknown>`},
		{name: "attribute directive", xml: `<attribute xmlns="` + Namespace + `"><!DOCTYPE x></attribute>`, attribute: true, want: ErrDTDForbidden},
		{name: "attribute truncated", xml: `<attribute xmlns="` + Namespace + `">`, attribute: true},
		{name: "attribute annotation", xml: `<attribute xmlns="` + Namespace + `"><annotation>`, attribute: true},
		{name: "attribute simple type", xml: `<attribute xmlns="` + Namespace + `"><simpleType>`, attribute: true},
		{
			name: "attribute multiple inline types",
			xml: `<attribute xmlns="` + Namespace + `">` +
				`<simpleType><restriction base="string"/></simpleType>` +
				`<simpleType><restriction base="string"/></simpleType>` +
				`</attribute>`,
			attribute: true,
		},
		{name: "attribute skipped", xml: `<attribute xmlns="` + Namespace + `"><unknown>`, attribute: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decoder, start := decoderAtStart(t, test.xml)
			var err error
			if test.attribute {
				_, _, err = parseAttributeBody(decoder, start, nil)
			} else {
				_, err = parseAnnotationChildren(decoder, start)
			}
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("parser error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestNotationParsingBranches(t *testing.T) {
	t.Parallel()

	decoder, start := decoderAtStart(t, `<notation xmlns="`+Namespace+`" xmlns:f="urn:foreign"`+
		` id="image" name="png" public="image/png" system="png.dat"`+
		` f:foreign="ignored"><annotation/></notation>`)
	notation, err := parseNotation(decoder, start)
	if err != nil || notation.ID != "image" || notation.Name != "png" ||
		notation.Public != "image/png" || notation.System != "png.dat" ||
		notation.Annotation == nil {
		t.Fatalf("parseNotation() = %#v, %v", notation, err)
	}

	for _, test := range []struct {
		name string
		xml  string
		want error
	}{
		{name: "directive", xml: `<notation xmlns="` + Namespace + `"><!DOCTYPE x></notation>`, want: ErrDTDForbidden},
		{name: "truncated", xml: `<notation xmlns="` + Namespace + `">`},
		{name: "annotation", xml: `<notation xmlns="` + Namespace + `"><annotation>`},
		{name: "skipped", xml: `<notation xmlns="` + Namespace + `"><unknown>`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decoder, start := decoderAtStart(t, test.xml)
			_, err := parseNotation(decoder, start)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("parseNotation() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParserPrimitiveDecisionTables(t *testing.T) {
	t.Parallel()

	for lexical, want := range map[string]bool{
		"true":  true,
		"1":     true,
		"false": false,
		"0":     false,
	} {
		got, err := parseBoolean(lexical)
		if err != nil || got != want {
			t.Fatalf("parseBoolean(%q) = %t, %v", lexical, got, err)
		}
	}
	if _, err := parseBoolean("yes"); err == nil {
		t.Fatal("parseBoolean(yes) succeeded")
	}

	for lexical, want := range map[string]Form{
		"qualified":   FormQualified,
		"unqualified": FormUnqualified,
	} {
		got, err := parseForm(lexical)
		if err != nil || got != want {
			t.Fatalf("parseForm(%q) = %q, %v", lexical, got, err)
		}
	}
	if _, err := parseForm("default"); err == nil {
		t.Fatal("parseForm(default) succeeded")
	}

	for _, lexical := range []string{"#all", "extension restriction substitution list union", ""} {
		if _, err := parseDerivationSet(lexical); err != nil {
			t.Fatalf("parseDerivationSet(%q) error = %v", lexical, err)
		}
	}
	if _, err := parseDerivationSet("invalid"); err == nil {
		t.Fatal("parseDerivationSet(invalid) succeeded")
	}
}

func TestParticleAttributeAndWildcardParsingBranches(t *testing.T) {
	t.Parallel()

	particle, err := parseOccurrence(xml.StartElement{Attr: []xml.Attr{
		{Name: xml.Name{Local: "minOccurs"}, Value: "0"},
		{Name: xml.Name{Local: "maxOccurs"}, Value: "unbounded"},
		{Name: xml.Name{Space: "urn:foreign", Local: "ignored"}, Value: "yes"},
	}})
	if err != nil || particle.MinOccurs != 0 || !particle.Unbounded {
		t.Fatalf("parseOccurrence() = %#v, %v", particle, err)
	}
	for _, attrs := range [][]xml.Attr{
		{{Name: xml.Name{Local: "minOccurs"}, Value: "invalid"}},
		{{Name: xml.Name{Local: "maxOccurs"}, Value: "invalid"}},
		{{Name: xml.Name{Local: "minOccurs"}, Value: "2"}, {Name: xml.Name{Local: "maxOccurs"}, Value: "1"}},
	} {
		if _, err := parseOccurrence(xml.StartElement{Attr: attrs}); err == nil {
			t.Fatalf("parseOccurrence(%#v) succeeded", attrs)
		}
	}

	wildcard, err := parseWildcard(xml.StartElement{Attr: []xml.Attr{
		{Name: xml.Name{Local: "namespace"}, Value: "urn:a urn:b"},
		{Name: xml.Name{Local: "processContents"}, Value: "lax"},
		{Name: xml.Name{Space: "urn:foreign", Local: "ignored"}, Value: "yes"},
	}})
	if err != nil || len(wildcard.Namespaces) != 2 ||
		wildcard.ProcessContents != ProcessLax {
		t.Fatalf("parseWildcard() = %#v, %v", wildcard, err)
	}
	if got, err := parseWildcard(xml.StartElement{}); err != nil ||
		got.ProcessContents != ProcessStrict || got.Namespaces[0] != "##any" {
		t.Fatalf("default wildcard = %#v, %v", got, err)
	}
	for _, attrs := range [][]xml.Attr{
		{{Name: xml.Name{Local: "namespace"}, Value: " "}},
		{{Name: xml.Name{Local: "processContents"}, Value: "invalid"}},
	} {
		if _, err := parseWildcard(xml.StartElement{Attr: attrs}); err == nil {
			t.Fatalf("parseWildcard(%#v) succeeded", attrs)
		}
	}

	namespaces := map[string]string{"g": "urn:groups"}
	group, err := parseGroupReferenceParticle(xml.StartElement{Attr: []xml.Attr{
		{Name: xml.Name{Local: "ref"}, Value: "g:Items"},
		{Name: xml.Name{Local: "minOccurs"}, Value: "0"},
	}}, namespaces)
	if err != nil || group.GroupRef.Local != "Items" || group.MinOccurs != 0 {
		t.Fatalf("parseGroupReferenceParticle() = %#v, %v", group, err)
	}
	for _, attrs := range [][]xml.Attr{
		{{Name: xml.Name{Local: "minOccurs"}, Value: "invalid"}},
		{{Name: xml.Name{Local: "ref"}, Value: "missing:Items"}},
		nil,
	} {
		if _, err := parseGroupReferenceParticle(xml.StartElement{Attr: attrs}, nil); err == nil {
			t.Fatalf("parseGroupReferenceParticle(%#v) succeeded", attrs)
		}
	}
	if got, err := parseAttributeGroupReference(xml.StartElement{Attr: []xml.Attr{{
		Name: xml.Name{Local: "ref"}, Value: "g:Metadata",
	}}}, namespaces); err != nil || got.Local != "Metadata" {
		t.Fatalf("parseAttributeGroupReference() = %#v, %v", got, err)
	}
	if _, err := parseAttributeGroupReference(xml.StartElement{}, nil); err == nil {
		t.Fatal("parseAttributeGroupReference() accepted a missing ref")
	}
	if _, err := parseAttributeGroupReference(xml.StartElement{Attr: []xml.Attr{{
		Name: xml.Name{Local: "ref"}, Value: "missing:Metadata",
	}}}, nil); err == nil {
		t.Fatal("parseAttributeGroupReference() accepted an unknown prefix")
	}
}

func TestDeclarationAttributesRejectInvalidLexicalValues(t *testing.T) {
	t.Parallel()

	namespaces := map[string]string{"xs": Namespace}
	for _, test := range []struct {
		name      string
		attribute string
		value     string
	}{
		{name: "type QName", attribute: "type", value: "missing:Type"},
		{name: "reference QName", attribute: "ref", value: "missing:element"},
		{name: "substitution group QName", attribute: "substitutionGroup", value: "missing:head"},
		{name: "form", attribute: "form", value: "default"},
		{name: "abstract", attribute: "abstract", value: "yes"},
		{name: "nillable", attribute: "nillable", value: "yes"},
		{name: "block", attribute: "block", value: "invalid"},
		{name: "final", attribute: "final", value: "invalid"},
	} {
		test := test
		t.Run("element "+test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseElement(xml.StartElement{Attr: []xml.Attr{{
				Name:  xml.Name{Local: test.attribute},
				Value: test.value,
			}}}, namespaces)
			if err == nil {
				t.Fatal("parseElement() succeeded")
			}
		})
	}

	for _, test := range []struct {
		name      string
		attribute string
		value     string
	}{
		{name: "reference QName", attribute: "ref", value: "missing:attribute"},
		{name: "type QName", attribute: "type", value: "missing:Type"},
		{name: "form", attribute: "form", value: "default"},
		{name: "use", attribute: "use", value: "sometimes"},
	} {
		test := test
		t.Run("attribute use "+test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseAttributeUse(xml.StartElement{Attr: []xml.Attr{{
				Name:  xml.Name{Local: test.attribute},
				Value: test.value,
			}}}, namespaces)
			if err == nil {
				t.Fatal("parseAttributeUse() succeeded")
			}
		})
	}
}

func TestSchemaAttributesRejectInvalidLexicalValues(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		attribute string
		value     string
	}{
		{name: "base URI", attribute: `xml:base`, value: "%"},
		{name: "element form", attribute: "elementFormDefault", value: "default"},
		{name: "attribute form", attribute: "attributeFormDefault", value: "default"},
		{name: "block default", attribute: "blockDefault", value: "invalid"},
		{name: "final default", attribute: "finalDefault", value: "invalid"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			schema := `<schema xmlns="` + Namespace + `" ` + test.attribute + `="` + test.value + `"/>`
			if _, err := Parse(context.Background(), []byte(schema), ParseOptions{}); err == nil {
				t.Fatal("Parse() succeeded")
			}
		})
	}
}

func TestIdentityConstraintErrorBranches(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`<key xmlns="` + Namespace + `" refer="missing:key"/>`,
		`<key xmlns="` + Namespace + `">`,
		`<key xmlns="` + Namespace + `"><annotation>`,
		`<key xmlns="` + Namespace + `"><selector xpath="."><annotation>`,
		`<key xmlns="` + Namespace + `"><unknown>`,
	} {
		decoder, start := decoderAtStart(t, source)
		if _, err := parseIdentityConstraint(
			decoder,
			start,
			IdentityKey,
			map[string]string{},
		); err == nil {
			t.Fatalf("parseIdentityConstraint(%q) succeeded", source)
		}
	}

	decoder, start := decoderAtStart(t, `<key xmlns="`+Namespace+`" xmlns:f="urn:foreign" f:ignored="yes"/>`)
	if _, err := parseIdentityConstraint(decoder, start, IdentityKey, nil); err != nil {
		t.Fatalf("parseIdentityConstraint() error = %v", err)
	}
}

func TestNestedComponentErrorBranches(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`<element xmlns="` + Namespace + `"><annotation>`,
		`<element xmlns="` + Namespace + `"><unknown>`,
		`<element xmlns="` + Namespace + `">` +
			`<simpleType><restriction base="string"/></simpleType>` +
			`<simpleType><restriction base="string"/></simpleType></element>`,
		`<element xmlns="` + Namespace + `">` +
			`<simpleType><restriction base="string"/></simpleType>` +
			`<complexType/></element>`,
	} {
		decoder, start := decoderAtStart(t, source)
		if err := parseElementBody(decoder, start, &Element{}, nil); err == nil {
			t.Fatalf("parseElementBody(%q) succeeded", source)
		}
	}

	for _, source := range []string{
		`<group xmlns="` + Namespace + `"><annotation>`,
		`<group xmlns="` + Namespace + `"><unknown>`,
	} {
		decoder, start := decoderAtStart(t, source)
		if _, err := parseModelGroupDefinition(decoder, start, nil); err == nil {
			t.Fatalf("parseModelGroupDefinition(%q) succeeded", source)
		}
	}

	for _, source := range []string{
		`<attributeGroup xmlns="` + Namespace + `"><annotation>`,
		`<attributeGroup xmlns="` + Namespace + `"><anyAttribute processContents="invalid"/>`,
		`<attributeGroup xmlns="` + Namespace + `"><anyAttribute><annotation>`,
		`<attributeGroup xmlns="` + Namespace + `"><attribute type="missing:Type"/>`,
		`<attributeGroup xmlns="` + Namespace + `"><attribute><simpleType>`,
		`<attributeGroup xmlns="` + Namespace + `"><attributeGroup ref="missing:Group"/>`,
		`<attributeGroup xmlns="` + Namespace + `" xmlns:g="urn:groups"><attributeGroup ref="g:Group"><annotation>`,
		`<attributeGroup xmlns="` + Namespace + `"><unknown>`,
	} {
		decoder, start := decoderAtStart(t, source)
		if _, err := parseAttributeGroupDefinition(decoder, start, nil); err == nil {
			t.Fatalf("parseAttributeGroupDefinition(%q) succeeded", source)
		}
	}
	decoder, start := decoderAtStart(t, `<attributeGroup xmlns="`+Namespace+`" xmlns:g="urn:groups"><attributeGroup ref="g:Group"><annotation>`)
	if _, err := parseAttributeGroupDefinition(
		decoder,
		start,
		map[string]string{"g": "urn:groups"},
	); err == nil {
		t.Fatal("parseAttributeGroupDefinition(reference annotation) succeeded")
	}
}

func TestParserBoundaryBranches(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Parse(canceled, []byte(`<schema xmlns="`+Namespace+`"/>`), ParseOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Parse() error = %v, want context.Canceled", err)
	}
	if _, err := parseValidated(canceled, []byte(`<schema xmlns="`+Namespace+`"/>`), ParseOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("parseValidated() error = %v, want context.Canceled", err)
	}
	if _, err := parseValidated(context.Background(), []byte(`<!DOCTYPE schema><schema xmlns="`+Namespace+`"/>`), ParseOptions{}); !errors.Is(err, ErrDTDForbidden) {
		t.Fatalf("parseValidated(DTD) error = %v", err)
	}

	for _, source := range []string{
		`<schema xmlns="` + Namespace + `">`,
		`<schema xmlns="` + Namespace + `"><!DOCTYPE schema></schema>`,
	} {
		decoder, start := decoderAtStart(t, source)
		if _, err := parseDocument(decoder, start, "test.xsd"); err == nil {
			t.Fatalf("parseDocument(%q) succeeded", source)
		}
	}

	for _, lexical := range []string{"", " value", ":value", "value:", "one:two:three"} {
		if _, err := parseQName(lexical, nil); err == nil {
			t.Fatalf("parseQName(%q) succeeded", lexical)
		}
	}

	foreign := xml.Name{Space: "urn:foreign", Local: "ignored"}
	if _, err := parseElement(xml.StartElement{Attr: []xml.Attr{{Name: foreign}}}, nil); err != nil {
		t.Fatalf("parseElement() error = %v", err)
	}
	if _, err := parseAttribute(xml.StartElement{Attr: []xml.Attr{{Name: foreign}}}, nil); err != nil {
		t.Fatalf("parseAttribute() error = %v", err)
	}
	if _, err := parseAttributeUse(xml.StartElement{Attr: []xml.Attr{{Name: foreign}}}, nil); err != nil {
		t.Fatalf("parseAttributeUse() error = %v", err)
	}
	if _, err := Parse(context.Background(), []byte(
		`<schema xmlns="`+Namespace+`" xmlns:f="urn:foreign">`+
			`<notation name="n" public="p" f:ignored="yes"/>`+
			`</schema>`,
	), ParseOptions{}); err != nil {
		t.Fatalf("Parse(notation foreign attribute) error = %v", err)
	}
}

func TestParseSimpleContentRestrictionFailures(t *testing.T) {
	t.Parallel()

	decoder := xml.NewDecoder(strings.NewReader(""))
	if _, err := parseFacet(decoder, xml.StartElement{
		Name: xml.Name{Space: Namespace, Local: "unknown"},
	}, nil); err == nil {
		t.Fatal("parseFacet(unknown) succeeded")
	}
	for _, body := range []string{
		`<simpleType><restriction base="missing:Type"/></simpleType>`,
		`<maxLength value="3" fixed="maybe"/>`,
	} {
		_, err := Parse(context.Background(), []byte(
			`<schema xmlns="`+Namespace+`"><complexType name="Restricted">`+
				`<simpleContent><restriction base="string">`+body+
				`</restriction></simpleContent></complexType></schema>`,
		), ParseOptions{})
		if err == nil {
			t.Fatalf("Parse(%s) succeeded", body)
		}
	}
}

func TestContextReaderChecksCancellation(t *testing.T) {
	t.Parallel()

	reader := &contextReader{ctx: context.Background(), reader: strings.NewReader("ok")}
	buffer := make([]byte, 2)
	if count, err := reader.Read(buffer); err != nil || count != 2 || string(buffer) != "ok" {
		t.Fatalf("Read() = %d, %v, %q", count, err, buffer)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	reader = &contextReader{ctx: canceled, reader: strings.NewReader("ignored")}
	if count, err := reader.Read(buffer); !errors.Is(err, context.Canceled) || count != 0 {
		t.Fatalf("Read() = %d, %v", count, err)
	}
}

func decoderAtStart(t *testing.T, source string) (*xml.Decoder, xml.StartElement) {
	t.Helper()

	decoder := xml.NewDecoder(strings.NewReader(source))
	token, err := decoder.Token()
	if err != nil {
		t.Fatalf("decoder.Token() error = %v", err)
	}
	start, ok := token.(xml.StartElement)
	if !ok {
		t.Fatalf("first token = %T, want xml.StartElement", token)
	}
	return decoder, start
}
