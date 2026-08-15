package xsd

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"reflect"
	"testing"
)

var errSerializerWrite = errors.New("serializer write failed")

func TestSerializerPropagatesWriteFailuresAtEveryBoundary(t *testing.T) {
	t.Parallel()

	document, err := Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:t="urn:test" targetNamespace="urn:test">`+
			`<xs:annotation><xs:documentation><b xmlns="urn:doc">schema</b></xs:documentation><xs:appinfo><tool xmlns="urn:tool"/></xs:appinfo></xs:annotation>`+
			`<xs:include schemaLocation="included.xsd"><xs:annotation/></xs:include>`+
			`<xs:redefine schemaLocation="base.xsd"><xs:annotation/>`+
			`<xs:simpleType name="OldCode"><xs:restriction base="xs:string"/></xs:simpleType>`+
			`<xs:complexType name="OldRecord"><xs:sequence/></xs:complexType>`+
			`<xs:group name="OldItems"><xs:sequence/></xs:group>`+
			`<xs:attributeGroup name="OldMetadata"><xs:attribute name="old"/></xs:attributeGroup>`+
			`</xs:redefine>`+
			`<xs:simpleType name="Code"><xs:restriction base="xs:string"><xs:minLength value="1"/></xs:restriction></xs:simpleType>`+
			`<xs:simpleType name="Codes"><xs:list itemType="t:Code"/></xs:simpleType>`+
			`<xs:simpleType name="Choice"><xs:union memberTypes="xs:string t:Code"/></xs:simpleType>`+
			`<xs:group name="Items"><xs:choice><xs:element name="item"/><xs:any namespace="##other"/><xs:sequence/><xs:group ref="t:Items"/></xs:choice></xs:group>`+
			`<xs:attributeGroup name="Metadata"><xs:attribute name="status"/><xs:attributeGroup ref="t:Metadata"/><xs:anyAttribute namespace="##other"/></xs:attributeGroup>`+
			`<xs:complexType name="Record"><xs:complexContent><xs:extension base="xs:anyType"><xs:sequence><xs:element name="code" type="t:Code"/></xs:sequence><xs:attribute name="kind"/><xs:attributeGroup ref="t:Metadata"/><xs:anyAttribute namespace="##other"/></xs:extension></xs:complexContent></xs:complexType>`+
			`<xs:complexType name="Restricted"><xs:simpleContent><xs:restriction base="xs:string"><xs:simpleType><xs:restriction base="xs:string"/></xs:simpleType><xs:maxLength value="3"><xs:annotation/></xs:maxLength></xs:restriction></xs:simpleContent></xs:complexType>`+
			`<xs:notation name="png" public="image/png" system="png.dat"><xs:annotation/></xs:notation>`+
			`<xs:attribute name="global" type="xs:string"/>`+
			`<xs:element name="root" type="t:Record"><xs:key name="key"><xs:selector xpath="."/><xs:field xpath="@id"/></xs:key></xs:element>`+
			`</xs:schema>`,
	), ParseOptions{SystemID: "https://example.test/schema.xsd"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	serializer := newSerializer(document)
	counter := &countingTokenEncoder{delegate: serializer.encoder}
	serializer.encoder = counter
	if err := serializer.schema(document); err != nil {
		t.Fatalf("schema() error = %v", err)
	}
	if counter.tokens == 0 {
		t.Fatal("schema() wrote no tokens")
	}

	for failAt := 1; failAt <= counter.tokens; failAt++ {
		serializer := newSerializer(document)
		serializer.encoder = &failingTokenEncoder{
			delegate: serializer.encoder,
			failAt:   failAt,
		}
		err := serializer.schema(document)
		if !errors.Is(err, errSerializerWrite) {
			t.Fatalf("token %d: error = %v", failAt, err)
		}
	}
}

func TestSerializerRejectsUnknownQNameNamespaces(t *testing.T) {
	t.Parallel()

	serializer := newSerializer(&Document{})
	unknown := QName{Namespace: "urn:unknown", Local: "Name"}
	if _, err := serializer.qName(unknown); err == nil {
		t.Fatal("qName() accepted an unknown namespace")
	}
	if _, err := serializer.appendQName(nil, "type", unknown); err == nil {
		t.Fatal("appendQName() accepted an unknown namespace")
	}
	if got, err := serializer.qName(QName{}); err != nil || got != "" {
		t.Fatalf("qName(empty) = %q, %v", got, err)
	}
}

func TestSerializerCoversProgrammaticModelChoices(t *testing.T) {
	t.Parallel()

	annotation := &Annotation{Documentation: []Documentation{{Content: "plain text"}}}
	stringType := QName{Namespace: Namespace, Local: "string"}
	inlineRestriction := SimpleType{
		Variety: SimpleRestriction,
		Base:    stringType,
	}
	document := &Document{
		TargetNamespace: "urn:programmatic",
		Namespaces: map[string]string{
			"":     "ignored",
			"xml":  "ignored",
			"none": "",
			"p":    "urn:programmatic",
		},
		Annotations: []Annotation{*annotation},
		SimpleTypes: []SimpleType{
			{Name: "Restricted", Variety: SimpleRestriction, InlineBase: &inlineRestriction},
			{Name: "Listed", Variety: SimpleList, InlineItem: &inlineRestriction},
			{Name: "Unioned", Variety: SimpleUnion, InlineMembers: []SimpleType{inlineRestriction}},
		},
		ComplexTypes: []ComplexType{{
			Name: "Record",
			Content: &ModelGroup{
				Compositor: Sequence,
				MinOccurs:  0,
				Unbounded:  true,
				OccursSet:  true,
				Particles: []Particle{{
					MinOccurs: 0,
					MaxOccurs: 2,
					Element: &Element{
						Name:             "value",
						DefaultSet:       true,
						InlineSimpleType: &inlineRestriction,
					},
				}, {
					MinOccurs: 1,
					MaxOccurs: 1,
					Element:   &Element{Name: "fixed", Type: stringType, FixedSet: true},
				}},
			},
			Attributes: []AttributeUse{{
				Name:             "status",
				DefaultSet:       true,
				InlineSimpleType: &inlineRestriction,
			}, {
				Name:     "locked",
				Type:     stringType,
				Use:      AttributeRequired,
				FixedSet: true,
			}},
			AttributeGroupRefs: []QName{{Namespace: "urn:programmatic", Local: "Metadata"}},
			AttributeWildcard: &Wildcard{
				Namespaces:      []string{"##other"},
				ProcessContents: ProcessSkip,
				Annotation:      annotation,
			},
		}},
		AttributeGroups: []AttributeGroup{{Name: "Metadata"}},
	}
	encoded, err := Marshal(document)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if _, err := Parse(context.Background(), encoded, ParseOptions{}); err != nil {
		t.Fatalf("Parse(Marshal()) error = %v\n%s", err, encoded)
	}
	if _, err := Marshal(&Document{SimpleTypes: []SimpleType{{
		Name:    "Invalid",
		Variety: "invalid",
	}}}); err == nil {
		t.Fatal("Marshal() accepted an invalid simple type variety")
	}
}

func TestSerializerPropagatesFragmentFlushFailures(t *testing.T) {
	t.Parallel()

	for _, annotation := range []Annotation{
		{Documentation: []Documentation{{Markup: "<b>text</b>"}}},
		{AppInformation: []AppInfo{{Content: "<tool/>"}}},
	} {
		serializer := newSerializer(&Document{})
		serializer.encoder = &failingFlushEncoder{delegate: serializer.encoder}
		if err := serializer.annotation(annotation); !errors.Is(err, errSerializerWrite) {
			t.Fatalf("annotation() error = %v", err)
		}
	}
}

func TestMarshalPropagatesFinalFlushFailure(t *testing.T) {
	t.Parallel()

	document := &Document{}
	serializer := newSerializer(document)
	serializer.encoder = &failingFlushEncoder{delegate: serializer.encoder}
	if _, err := marshalWithSerializer(document, serializer); !errors.Is(err, errSerializerWrite) {
		t.Fatalf("marshalWithSerializer() error = %v", err)
	}
}

func TestMarshalLimitInternalsFailClosed(t *testing.T) {
	t.Parallel()

	writer := &marshalLimitWriter{
		writer: errorWriter{}, remaining: 1, maximum: 1,
	}
	if written, err := writer.Write([]byte("xx")); written != 0 || !errors.Is(err, errSerializerWrite) {
		t.Fatalf("limited write = %d, %v", written, err)
	}

	budget := &marshalBudget{
		limits: marshalLimits{MaxDepth: 1, MaxComponents: 1},
		active: make(map[marshalPointer]struct{}),
	}
	if err := budget.value(reflect.Value{}, 0); err != nil {
		t.Fatalf("invalid reflection value error = %v", err)
	}
	if err := budget.value(reflect.ValueOf([]int{1, 2}), 0); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("slice component limit error = %v", err)
	}
}

func TestMarshalLimitsTrackExactRemainingWork(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	writer := &marshalLimitWriter{writer: &output, remaining: 3, maximum: 3}
	if written, err := writer.Write([]byte("ab")); written != 2 || err != nil || writer.remaining != 1 {
		t.Fatalf("first limited write = %d, %v, remaining %d", written, err, writer.remaining)
	}
	if written, err := writer.Write([]byte("cd")); written != 1 ||
		!errors.Is(err, ErrLimitExceeded) || writer.remaining != 0 || output.String() != "abc" {
		t.Fatalf("overflowing write = %d, %v, remaining %d, output %q", written, err, writer.remaining, output.String())
	}

	budget := &marshalBudget{
		limits: marshalLimits{MaxDepth: 1, MaxComponents: 2},
		active: make(map[marshalPointer]struct{}),
	}
	if err := budget.value(reflect.ValueOf(struct{}{}), 1); err != nil || budget.work != 1 {
		t.Fatalf("exact-depth value error = %v, work = %d", err, budget.work)
	}
	if err := budget.add(1); err != nil || budget.work != 2 {
		t.Fatalf("exact component budget error = %v, work = %d", err, budget.work)
	}
	if err := budget.add(1); !errors.Is(err, ErrLimitExceeded) || budget.work != 2 {
		t.Fatalf("overflow component budget error = %v, work = %d", err, budget.work)
	}
	if err := budget.value(reflect.ValueOf(struct{}{}), 2); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("depth overflow error = %v", err)
	}
}

func TestNamespacePrefixAllocationUsesFirstAvailablePrefix(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		namespaces map[string]string
		want       string
	}{
		{namespaces: map[string]string{"ns1": "urn:one", "ns3": "urn:three"}, want: "ns2"},
		{namespaces: map[string]string{"ns1": "urn:one", "ns2": "urn:two"}, want: "ns3"},
	} {
		if prefix := nextNamespacePrefix(test.namespaces); prefix != test.want {
			t.Fatalf("nextNamespacePrefix(%v) = %q, want %q", test.namespaces, prefix, test.want)
		}
	}
}

func TestSerializerBuildsCanonicalNamespaceRegistry(t *testing.T) {
	t.Parallel()

	document := &Document{
		TargetNamespace: "urn:known",
		Namespaces: map[string]string{
			"":      "urn:default",
			"xml":   "http://www.w3.org/XML/1998/namespace",
			"empty": "",
			"z":     "urn:known",
			"a":     "urn:known",
		},
		Elements: []Element{{Type: QName{Namespace: "urn:missing", Local: "External"}}},
	}
	serializer := newSerializer(document)
	if _, exists := serializer.namespaces[""]; exists {
		t.Fatal("serializer retained the default namespace as a prefixed declaration")
	}
	if _, exists := serializer.namespaces["xml"]; exists {
		t.Fatal("serializer retained the reserved xml prefix")
	}
	if _, exists := serializer.namespaces["empty"]; exists {
		t.Fatal("serializer retained an empty namespace")
	}
	if _, exists := serializer.namespaces["tns"]; exists {
		t.Fatal("serializer added tns despite an existing target namespace prefix")
	}
	if serializer.prefixes["urn:known"] != "a" || serializer.namespaces["ns1"] != "urn:missing" {
		t.Fatalf("namespace registry = %#v, reverse = %#v", serializer.namespaces, serializer.prefixes)
	}
}

func TestSchemaSerializationDistinguishesSystemAndBaseURIs(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		base    string
		system  string
		present bool
	}{
		{name: "same URI", base: "https://example.test/schema.xsd", system: "https://example.test/schema.xsd"},
		{name: "different URI", base: "https://example.test/base/", system: "https://example.test/schema.xsd", present: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := Marshal(&Document{BaseURI: test.base, SystemID: test.system})
			if err != nil {
				t.Fatal(err)
			}
			hasBase := bytes.Contains(encoded, []byte(`xml:base="`))
			if hasBase != test.present {
				t.Fatalf("Marshal() = %s, xml:base presence = %v", encoded, hasBase)
			}
		})
	}
}

func TestRedefinitionSerializationConsumesBodiesExactlyOnce(t *testing.T) {
	t.Parallel()

	document := &Document{
		References: []SchemaReference{
			{Kind: ReferenceRedefine, Location: "first.xsd"},
			{Kind: ReferenceRedefine, Location: "second.xsd"},
		},
		Redefinitions: []Redefinition{{
			Reference:   SchemaReference{Kind: ReferenceRedefine, Location: "first.xsd"},
			SimpleTypes: []SimpleType{{Name: "Code", Variety: SimpleRestriction}},
		}},
	}
	encoded, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(encoded, []byte(`<xs:redefine`)) != 2 ||
		bytes.Count(encoded, []byte(`<xs:simpleType name="Code"`)) != 1 {
		t.Fatalf("Marshal() = %s", encoded)
	}
}

func TestRedefinitionReferenceFallbackPropagatesWriteFailure(t *testing.T) {
	t.Parallel()

	document := &Document{References: []SchemaReference{{
		Kind: ReferenceRedefine, Location: "fallback.xsd",
	}}}
	serializer := newSerializer(document)
	serializer.encoder = &failingTokenEncoder{
		delegate: serializer.encoder,
		failAt:   2,
	}
	if err := serializer.schema(document); !errors.Is(err, errSerializerWrite) {
		t.Fatalf("schema() error = %v", err)
	}
}

func TestIdentityConstraintSerializationHandlesSparseFieldMetadata(t *testing.T) {
	t.Parallel()

	document := &Document{Elements: []Element{{
		Name: "root",
		IdentityConstraints: []IdentityConstraint{{
			Kind:             IdentityKey,
			Name:             "key",
			Selector:         ".",
			Fields:           []string{"@first", "@second"},
			FieldIDs:         []string{"first-id"},
			FieldAnnotations: []*Annotation{{}},
		}},
	}}}
	encoded, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(encoded, []byte(`<xs:field`)) != 2 ||
		bytes.Count(encoded, []byte(`id="first-id"`)) != 1 ||
		bytes.Count(encoded, []byte(`<xs:annotation`)) != 1 {
		t.Fatalf("Marshal() = %s", encoded)
	}
}

func TestComplexTypeSerializationPreservesIndependentContentFlags(t *testing.T) {
	t.Parallel()

	encoded, err := Marshal(&Document{ComplexTypes: []ComplexType{
		{Name: "ImplicitMixed", Mixed: true},
		{Name: "ExplicitFalse", MixedSet: true},
		{
			Name:       "ComplexRestriction",
			Derivation: DerivationRestriction,
			SimpleFacets: []Facet{{
				Kind: FacetLength, Value: "1",
			}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`name="ImplicitMixed" mixed="true"`)) ||
		!bytes.Contains(encoded, []byte(`name="ExplicitFalse" mixed="false"`)) ||
		bytes.Contains(encoded, []byte(`<xs:length value="1"`)) {
		t.Fatalf("Marshal() = %s", encoded)
	}
}

func TestSerializerRejectsUnknownNamespacesAtComponentBoundaries(t *testing.T) {
	t.Parallel()

	unknown := QName{Namespace: "urn:unknown", Local: "Name"}
	for _, test := range []struct {
		name string
		run  func(*serializer) error
	}{
		{name: "restriction base", run: func(s *serializer) error {
			return s.simpleType(SimpleType{Variety: SimpleRestriction, Base: unknown})
		}},
		{name: "list item", run: func(s *serializer) error {
			return s.simpleType(SimpleType{Variety: SimpleList, ItemType: unknown})
		}},
		{name: "union member", run: func(s *serializer) error {
			return s.simpleType(SimpleType{Variety: SimpleUnion, MemberTypes: []QName{unknown}})
		}},
		{name: "complex base", run: func(s *serializer) error {
			return s.complexType(ComplexType{Derivation: DerivationExtension, Base: unknown})
		}},
		{name: "model group reference", run: func(s *serializer) error {
			return s.modelGroupChildren(ModelGroup{Particles: []Particle{{GroupRef: unknown}}})
		}},
		{name: "element type", run: func(s *serializer) error {
			return s.element(Element{Type: unknown})
		}},
		{name: "element reference", run: func(s *serializer) error {
			return s.element(Element{Ref: unknown})
		}},
		{name: "substitution group", run: func(s *serializer) error {
			return s.element(Element{SubstitutionGroup: unknown})
		}},
		{name: "identity reference", run: func(s *serializer) error {
			return s.identityConstraint(IdentityConstraint{Refer: unknown})
		}},
		{name: "attribute type", run: func(s *serializer) error {
			return s.attributeUse(AttributeUse{Type: unknown})
		}},
		{name: "attribute reference", run: func(s *serializer) error {
			return s.attributeUse(AttributeUse{Ref: unknown})
		}},
		{name: "attribute group reference", run: func(s *serializer) error {
			return s.attributeGroupReferences([]QName{unknown}, nil)
		}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.run(newSerializer(&Document{})); err == nil {
				t.Fatal("serializer accepted unknown namespace")
			}
		})
	}
}

func TestSerializerPropagatesComponentAnnotationFailures(t *testing.T) {
	t.Parallel()

	annotation := &Annotation{}
	for _, test := range []struct {
		name string
		run  func(*serializer) error
	}{
		{name: "simple type", run: func(s *serializer) error {
			return s.simpleType(SimpleType{Annotation: annotation, Variety: SimpleRestriction})
		}},
		{name: "complex type", run: func(s *serializer) error {
			return s.complexType(ComplexType{Annotation: annotation})
		}},
		{name: "model group definition", run: func(s *serializer) error {
			return s.modelGroupDefinition(ModelGroupDefinition{Annotation: annotation})
		}},
		{name: "model group", run: func(s *serializer) error {
			return s.modelGroup(ModelGroup{Compositor: Sequence, Annotation: annotation})
		}},
		{name: "attribute group", run: func(s *serializer) error {
			return s.attributeGroup(AttributeGroup{Annotation: annotation})
		}},
		{name: "element", run: func(s *serializer) error {
			return s.element(Element{Annotation: annotation})
		}},
		{name: "identity constraint", run: func(s *serializer) error {
			return s.identityConstraint(IdentityConstraint{Kind: IdentityKey, Annotation: annotation})
		}},
		{name: "attribute", run: func(s *serializer) error {
			return s.attributeUse(AttributeUse{Annotation: annotation})
		}},
		{name: "annotated element", run: func(s *serializer) error {
			return s.annotatedElement("xs:any", nil, annotation)
		}},
		{name: "nested model group", run: func(s *serializer) error {
			return s.modelGroupChildren(ModelGroup{Particles: []Particle{{Group: &ModelGroup{
				Compositor: Sequence, Annotation: annotation,
			}}}})
		}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			serializer := newSerializer(&Document{})
			serializer.encoder = &failingTokenEncoder{
				delegate: serializer.encoder,
				failAt:   2,
			}
			if err := test.run(serializer); !errors.Is(err, errSerializerWrite) {
				t.Fatalf("serializer error = %v", err)
			}
		})
	}
}

func TestSerializerPropagatesNestedComponentFailures(t *testing.T) {
	t.Parallel()

	invalid := SimpleType{Variety: "invalid"}
	for _, test := range []struct {
		name string
		run  func(*serializer) error
	}{
		{name: "union member", run: func(s *serializer) error {
			return s.simpleType(SimpleType{Variety: SimpleUnion, InlineMembers: []SimpleType{invalid}})
		}},
		{name: "inline element simple type", run: func(s *serializer) error {
			return s.element(Element{InlineSimpleType: &invalid})
		}},
		{name: "inline element complex type", run: func(s *serializer) error {
			return s.element(Element{InlineComplexType: &ComplexType{
				Derivation: DerivationExtension,
				Base:       QName{Namespace: "urn:unknown", Local: "Base"},
			}})
		}},
		{name: "element identity constraint", run: func(s *serializer) error {
			return s.element(Element{IdentityConstraints: []IdentityConstraint{{
				Kind:  IdentityKeyRef,
				Refer: QName{Namespace: "urn:unknown", Local: "Key"},
			}}})
		}},
		{name: "inline attribute type", run: func(s *serializer) error {
			return s.attributeUse(AttributeUse{InlineSimpleType: &invalid})
		}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.run(newSerializer(&Document{})); err == nil {
				t.Fatal("serializer did not propagate nested error")
			}
		})
	}
}

func TestMarshalRejectsNilDocument(t *testing.T) {
	t.Parallel()

	if _, err := Marshal(nil); err == nil {
		t.Fatal("Marshal(nil) succeeded")
	}
}

func TestSerializerCoversRemainingModelMetadata(t *testing.T) {
	t.Parallel()

	member := QName{Namespace: "urn:member", Local: "Type"}
	document := &Document{
		SystemID: "https://example.test/schema.xsd",
		BaseURI:  "https://example.test/base/",
		AttributeGroups: []AttributeGroup{{Attributes: []AttributeUse{{
			InlineSimpleType: &SimpleType{Variety: SimpleRestriction, Base: member},
		}}}},
		Redefinitions: []Redefinition{{
			AttributeGroups: []AttributeGroup{{
				Attributes: []AttributeUse{{
					InlineSimpleType: &SimpleType{Variety: SimpleRestriction, Base: member},
				}},
				References: []QName{member},
			}},
		}},
	}
	namespaces := documentQNameNamespaces(document)
	if _, ok := namespaces[member.Namespace]; !ok {
		t.Fatalf("documentQNameNamespaces() = %#v", namespaces)
	}
	encoded, err := Marshal(document)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !bytes.Contains(encoded, []byte(`xml:base="https://example.test/base/"`)) {
		t.Fatalf("Marshal() = %s", encoded)
	}
}

func TestSerializerPropagatesRemainingNestedFailures(t *testing.T) {
	t.Parallel()

	annotation := &Annotation{}
	for _, test := range []struct {
		name   string
		failAt int
		run    func(*serializer) error
	}{
		{name: "plain documentation", failAt: 3, run: func(s *serializer) error {
			return s.annotation(Annotation{Documentation: []Documentation{{Content: "text"}}})
		}},
		{name: "content annotation", failAt: 3, run: func(s *serializer) error {
			return s.complexType(ComplexType{
				Derivation: DerivationExtension, ContentAnnotation: annotation,
			})
		}},
		{name: "derivation annotation", failAt: 4, run: func(s *serializer) error {
			return s.complexType(ComplexType{
				Derivation: DerivationExtension, DerivationAnnotation: annotation,
			})
		}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			serializer := newSerializer(&Document{})
			serializer.encoder = &failingTokenEncoder{
				delegate: serializer.encoder,
				failAt:   test.failAt,
			}
			if err := test.run(serializer); !errors.Is(err, errSerializerWrite) {
				t.Fatalf("serializer error = %v", err)
			}
		})
	}

	serializer := newSerializer(&Document{})
	err := serializer.modelGroupChildren(ModelGroup{Particles: []Particle{{
		Group: &ModelGroup{Compositor: Sequence, Particles: []Particle{{
			GroupRef: QName{Namespace: "urn:unknown", Local: "Group"},
		}}},
	}}})
	if err == nil {
		t.Fatal("modelGroupChildren() did not propagate nested error")
	}
}

type countingTokenEncoder struct {
	delegate interface {
		EncodeToken(xml.Token) error
		Flush() error
	}
	tokens int
}

func (e *countingTokenEncoder) EncodeToken(token xml.Token) error {
	e.tokens++
	return e.delegate.EncodeToken(token)
}

func (e *countingTokenEncoder) Flush() error {
	return e.delegate.Flush()
}

type failingTokenEncoder struct {
	delegate interface {
		EncodeToken(xml.Token) error
		Flush() error
	}
	failAt int
	tokens int
}

func (e *failingTokenEncoder) EncodeToken(token xml.Token) error {
	e.tokens++
	if e.tokens == e.failAt {
		return errSerializerWrite
	}
	return e.delegate.EncodeToken(token)
}

func (e *failingTokenEncoder) Flush() error {
	return e.delegate.Flush()
}

type failingFlushEncoder struct {
	delegate interface {
		EncodeToken(xml.Token) error
		Flush() error
	}
}

func (e *failingFlushEncoder) EncodeToken(token xml.Token) error {
	return e.delegate.EncodeToken(token)
}

func (e *failingFlushEncoder) Flush() error {
	return errSerializerWrite
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errSerializerWrite
}
