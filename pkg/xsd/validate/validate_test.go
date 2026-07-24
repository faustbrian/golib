package validate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
	"github.com/faustbrian/golib/pkg/xsd/validate"
)

func TestValidateReaderMatchesByteValidation(t *testing.T) {
	t.Parallel()

	validator := newValidator(t)
	for _, instance := range [][]byte{
		[]byte(`<amount xmlns="urn:order">1.5</amount>`),
		[]byte(`<amount xmlns="urn:order">invalid</amount>`),
		[]byte(`<missing xmlns="urn:order"/>`),
	} {
		want, wantErr := validator.Validate(context.Background(), instance)
		got, gotErr := validator.ValidateReader(context.Background(), bytes.NewReader(instance))
		if !errors.Is(gotErr, wantErr) || !reflect.DeepEqual(got, want) {
			t.Fatalf("ValidateReader(%q) = %#v, %v; want %#v, %v", instance, got, gotErr, want, wantErr)
		}
	}
}

func TestValidateReaderEnforcesInputBoundaries(t *testing.T) {
	t.Parallel()

	set := validatorSet(t)
	exact := []byte(`<amount xmlns="urn:order">1</amount>`)
	exactValidator, err := validate.New(set, validate.Options{
		Limits: validate.Limits{MaxBytes: int64(len(exact))},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := exactValidator.ValidateReader(context.Background(), bytes.NewReader(exact))
	if err != nil || !result.Valid {
		t.Fatalf("ValidateReader(exact limit) = %#v, %v", result, err)
	}
	maximumValidator, err := validate.New(set, validate.Options{
		Limits: validate.Limits{MaxBytes: math.MaxInt64},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err = maximumValidator.ValidateReader(context.Background(), bytes.NewReader(exact))
	if err != nil || !result.Valid {
		t.Fatalf("ValidateReader(maximum limit) = %#v, %v", result, err)
	}

	validator, err := validate.New(set, validate.Options{Limits: validate.Limits{MaxBytes: 8}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validator.ValidateReader(
		context.Background(),
		bytes.NewBufferString(`<amount xmlns="urn:order">1</amount>`),
	); !errors.Is(err, validate.ErrLimitExceeded) {
		t.Fatalf("ValidateReader(limit) error = %v", err)
	}
	if _, err := validator.ValidateReader(
		context.Background(),
		bytes.NewBufferString("         "),
	); !errors.Is(err, validate.ErrLimitExceeded) {
		t.Fatalf("ValidateReader(token limit) error = %v", err)
	}
	if _, err := validator.ValidateReader(context.Background(), nil); err == nil {
		t.Fatal("ValidateReader(nil) succeeded")
	}
	want := errors.New("reader failed")
	if _, err := validator.ValidateReader(context.Background(), failingReader{err: want}); !errors.Is(err, want) {
		t.Fatalf("ValidateReader(reader error) = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := validator.ValidateReader(canceled, bytes.NewBufferString(`<amount/>`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateReader(canceled) = %v", err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestValidateChecksGlobalElementDatatypeExactly(t *testing.T) {
	t.Parallel()

	validator := newValidator(t)
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<amount xmlns="urn:order">999999999999999999.0000000001</amount>`),
	)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !result.Valid || len(result.Diagnostics) != 0 {
		t.Fatalf("Result = %#v", result)
	}

	result, err = validator.Validate(
		context.Background(),
		[]byte(`<amount xmlns="urn:order">1e3</amount>`),
	)
	if err != nil {
		t.Fatalf("Validate(invalid) error = %v", err)
	}
	if result.Valid || len(result.Diagnostics) != 1 ||
		result.Diagnostics[0].Code != "cvc-datatype-valid.1.2.1" {
		t.Fatalf("invalid Result = %#v", result)
	}
}

func TestValidateAcceptsDerivedBuiltInXSIType(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:built-in-types",
		Content: []byte(
			`<schema xmlns="http://www.w3.org/2001/XMLSchema">` +
				`<element name="value" type="string"/>` +
				`</schema>`,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(context.Background(), []byte(
		`<value xmlns:xs="http://www.w3.org/2001/XMLSchema" `+
			`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" `+
			`xsi:type="xs:token">normalized value</value>`,
	))
	if err != nil || !result.Valid {
		t.Fatalf("Validate() = %#v, %v", result, err)
	}
}

func TestValidateTreeMatchesByteValidation(t *testing.T) {
	t.Parallel()

	validator := newValidator(t)
	fromBytes, err := validator.Validate(
		context.Background(),
		[]byte(`<amount xmlns="urn:order">1e3</amount>`),
	)
	if err != nil {
		t.Fatal(err)
	}
	fromTree, err := validator.ValidateTree(context.Background(), validate.Node{
		Name: xsd.QName{Namespace: "urn:order", Local: "amount"},
		Text: "1e3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fromTree.Valid != fromBytes.Valid ||
		len(fromTree.Diagnostics) != len(fromBytes.Diagnostics) ||
		fromTree.Diagnostics[0].Code != fromBytes.Diagnostics[0].Code ||
		fromTree.Diagnostics[0].Path != fromBytes.Diagnostics[0].Path {
		t.Fatalf("bytes = %#v, tree = %#v", fromBytes, fromTree)
	}

	limited, err := validate.New(validatorSet(t), validate.Options{
		Limits: validate.Limits{MaxNodes: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = limited.ValidateTree(context.Background(), validate.Node{
		Name:     xsd.QName{Namespace: "urn:order", Local: "complex"},
		Children: []validate.Node{{Name: xsd.QName{Local: "child"}}},
	})
	if !errors.Is(err, validate.ErrLimitExceeded) {
		t.Fatalf("ValidateTree(limit) error = %v, want ErrLimitExceeded", err)
	}

	result, err := complexValidator(t).ValidateTree(context.Background(), validate.Node{
		Name: xsd.QName{Namespace: "urn:order", Local: "order"},
		Attributes: map[xsd.QName]string{
			{Local: "status"}: "ready",
		},
		Children: []validate.Node{{
			Name: xsd.QName{Namespace: "urn:order", Local: "id"},
			Text: "value",
		}},
	})
	if err != nil || !result.Valid {
		t.Fatalf("ValidateTree(children) = %#v, %v", result, err)
	}
}

func TestValidateTreeEnforcesOwnedInputBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		limits  validate.Limits
		node    validate.Node
		context func() context.Context
		limit   bool
	}{
		{
			name: "canceled context",
			node: validate.Node{Name: xsd.QName{Namespace: "urn:order", Local: "amount"}},
			context: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
		},
		{
			name:   "depth",
			limits: validate.Limits{MaxDepth: 1},
			node:   validate.Node{Name: xsd.QName{Local: "root"}, Children: []validate.Node{{Name: xsd.QName{Local: "child"}}}},
			limit:  true,
		},
		{
			name:   "nodes",
			limits: validate.Limits{MaxNodes: 1},
			node:   validate.Node{Name: xsd.QName{Local: "root"}, Children: []validate.Node{{Name: xsd.QName{Local: "child"}}}},
			limit:  true,
		},
		{
			name:   "text bytes",
			limits: validate.Limits{MaxTextBytes: 1},
			node:   validate.Node{Name: xsd.QName{Local: "root"}, Text: "xx"},
			limit:  true,
		},
		{
			name:   "attributes",
			limits: validate.Limits{MaxAttributes: 1},
			node: validate.Node{Name: xsd.QName{Local: "root"}, Attributes: map[xsd.QName]string{
				{Local: "first"}:  "1",
				{Local: "second"}: "2",
			}},
			limit: true,
		},
		{name: "missing node name", node: validate.Node{}},
		{
			name: "missing attribute name",
			node: validate.Node{Name: xsd.QName{Local: "root"}, Attributes: map[xsd.QName]string{
				{}: "value",
			}},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			validator, err := validate.New(validatorSet(t), validate.Options{Limits: test.limits})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if test.context != nil {
				ctx = test.context()
			}
			_, err = validator.ValidateTree(ctx, test.node)
			if test.name == "canceled context" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("ValidateTree() error = %v", err)
				}
				return
			}
			if test.limit {
				if !errors.Is(err, validate.ErrLimitExceeded) {
					t.Fatalf("ValidateTree() error = %v, want ErrLimitExceeded", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ValidateTree() accepted an unnamed tree component")
			}
		})
	}
}

func TestValidateSimpleContentRejectsUndeclaredAttributes(t *testing.T) {
	t.Parallel()

	validator := newValidator(t)
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<amount xmlns="urn:order" extra="value">1</amount>`),
	)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if result.Valid || len(result.Diagnostics) != 1 ||
		result.Diagnostics[0].Code != "cvc-type.3.1.1" {
		t.Fatalf("Result = %#v", result)
	}
}

func TestValidateRejectsDuplicateExpandedAttributeNames(t *testing.T) {
	t.Parallel()

	validator := newValidator(t)
	_, err := validator.Validate(
		context.Background(),
		[]byte(`<amount xmlns="urn:order" xmlns:a="urn:attr" xmlns:b="urn:attr" a:value="1" b:value="2">1</amount>`),
	)
	var parseErr *xsd.ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("Validate() error = %v, want ParseError", err)
	}
}

func TestValidateRejectsUnknownAndAbstractRoots(t *testing.T) {
	t.Parallel()

	validator := newValidator(t)
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<missing xmlns="urn:order"/>`),
	)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if result.Valid || result.Diagnostics[0].Code != "cvc-elt.1.a" {
		t.Fatalf("unknown Result = %#v", result)
	}

	result, err = validator.Validate(
		context.Background(),
		[]byte(`<template xmlns="urn:order"/>`),
	)
	if err != nil {
		t.Fatalf("Validate(abstract) error = %v", err)
	}
	if result.Valid || result.Diagnostics[0].Code != "cvc-elt.2" {
		t.Fatalf("abstract Result = %#v", result)
	}
}

func TestValidateRejectsDTDBeforeInstanceProcessing(t *testing.T) {
	t.Parallel()

	validator := newValidator(t)
	_, err := validator.Validate(
		context.Background(),
		[]byte(`<!DOCTYPE amount SYSTEM "https://attacker.invalid/entity.dtd">
<amount xmlns="urn:order">1</amount>`),
	)
	if !errors.Is(err, xsd.ErrDTDForbidden) {
		t.Fatalf("Validate() error = %v, want ErrDTDForbidden", err)
	}
}

func TestValidateEmptyComplexTypeRejectsChildContent(t *testing.T) {
	t.Parallel()

	validator := newValidator(t)
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<complex xmlns="urn:order"/>`),
	)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !result.Valid {
		t.Fatalf("Result = %#v", result)
	}

	result, err = validator.Validate(
		context.Background(),
		[]byte(`<complex xmlns="urn:order"><child/></complex>`),
	)
	if err != nil {
		t.Fatalf("Validate(child) error = %v", err)
	}
	if result.Valid || len(result.Diagnostics) != 1 ||
		result.Diagnostics[0].Code != "cvc-complex-type.2.1" {
		t.Fatalf("child Result = %#v", result)
	}
}

func TestValidateComplexSequenceChoiceAndRequiredAttributes(t *testing.T) {
	t.Parallel()

	validator := complexValidator(t)
	result, err := validator.Validate(context.Background(), []byte(
		`<order xmlns="urn:order" status="new"><id>A-1</id>`+
			`<price>1.20</price><note>ready</note></order>`,
	))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !result.Valid || len(result.Diagnostics) != 0 {
		t.Fatalf("valid Result = %#v", result)
	}

	result, err = validator.Validate(context.Background(), []byte(
		`<order xmlns="urn:order"><price>1e3</price></order>`,
	))
	if err != nil {
		t.Fatalf("Validate(invalid) error = %v", err)
	}
	codes := map[string]bool{}
	for _, diagnostic := range result.Diagnostics {
		codes[diagnostic.Code] = true
	}
	if result.Valid || !codes["cvc-complex-type.2.4.a"] ||
		!codes["cvc-complex-type.4"] {
		t.Fatalf("invalid Result = %#v", result)
	}
}

func TestValidateAllCompositorAcceptsAnyOrderAndRequiresMembers(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/all.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:all" targetNamespace="urn:all" elementFormDefault="qualified">
 <xs:element name="root" type="tns:Root"/>
 <xs:complexType name="Root"><xs:all>
  <xs:element name="first" type="xs:string"/>
  <xs:element name="second" type="xs:string" minOccurs="0"/>
 </xs:all></xs:complexType>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:all"><second>b</second><first>a</first></root>`),
	)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !result.Valid {
		t.Fatalf("valid Result = %#v", result)
	}

	result, err = validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:all"><second>b</second></root>`),
	)
	if err != nil {
		t.Fatalf("Validate(missing) error = %v", err)
	}
	if result.Valid || result.Diagnostics[0].Code != "cvc-complex-type.2.4.a" {
		t.Fatalf("missing Result = %#v", result)
	}
}

func TestValidateChoiceAppliesAlternativeOccurrenceConstraints(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/choice-occurs.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:choice" targetNamespace="urn:choice" elementFormDefault="qualified">
 <xs:element name="root" type="tns:Root"/><xs:complexType name="Root"><xs:choice>
  <xs:element name="optional" type="xs:string" minOccurs="0"/>
  <xs:element name="item" type="xs:string" maxOccurs="2"/>
 </xs:choice></xs:complexType>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:choice"><item>a</item><item>b</item></root>`),
	)
	if err != nil || !result.Valid {
		t.Fatalf("Validate(choice occurrences) = %#v, %v", result, err)
	}
}

func TestValidateXSITypeDerivationAndBlock(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/xsi-type.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:type" targetNamespace="urn:type" elementFormDefault="qualified">
 <xs:complexType name="Base"><xs:sequence><xs:element name="value" type="xs:string"/></xs:sequence></xs:complexType>
 <xs:complexType name="Extended"><xs:complexContent><xs:extension base="tns:Base"/></xs:complexContent></xs:complexType>
 <xs:element name="open" type="tns:Base"/>
 <xs:element name="closed" type="tns:Base" block="extension"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(context.Background(), []byte(
		`<open xmlns="urn:type" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="Extended"><value>x</value></open>`))
	if err != nil || !result.Valid {
		t.Fatalf("Validate(open) = %#v, %v", result, err)
	}
	result, err = validator.Validate(context.Background(), []byte(
		`<closed xmlns="urn:type" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="Extended"><value>x</value></closed>`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || result.Diagnostics[0].Code != "cvc-elt.4.3" {
		t.Fatalf("Validate(closed) = %#v", result)
	}
}

func TestValidateNillableDefaultAndFixedElementConstraints(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/values.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:values">
 <xs:element name="score" type="xs:decimal" nillable="true" default="2.5"/>
 <xs:element name="requiredScore" type="xs:decimal"/>
 <xs:element name="code" type="xs:string" fixed="X"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, instance := range []string{
		`<score xmlns="urn:values" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:nil="true"/>`,
		`<score xmlns="urn:values"/>`,
		`<code xmlns="urn:values"/>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil {
			t.Fatalf("Validate(%s) error = %v", instance, validateErr)
		}
		if !result.Valid {
			t.Fatalf("Validate(%s) = %#v", instance, result)
		}
	}

	tests := []struct {
		instance string
		code     string
	}{
		{
			instance: `<requiredScore xmlns="urn:values" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:nil="true"/>`,
			code:     "cvc-elt.3.1",
		},
		{
			instance: `<score xmlns="urn:values" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:nil="true">1</score>`,
			code:     "cvc-elt.3.2.1",
		},
		{instance: `<code xmlns="urn:values">Y</code>`, code: "cvc-elt.5.2.2.2.1"},
	}
	for _, test := range tests {
		result, validateErr := validator.Validate(context.Background(), []byte(test.instance))
		if validateErr != nil {
			t.Fatalf("Validate(%s) error = %v", test.instance, validateErr)
		}
		if result.Valid || result.Diagnostics[0].Code != test.code {
			t.Fatalf("Validate(%s) = %#v, want %s", test.instance, result, test.code)
		}
	}
}

func TestValidateEmptyAndMixedFixedElementConstraints(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/fixed-content.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:fixed" elementFormDefault="qualified">
 <xs:element name="empty" type="xs:anySimpleType" fixed=""/>
 <xs:element name="mixed" fixed="alpha beta"><xs:complexType mixed="true">
  <xs:sequence><xs:element name="separator" minOccurs="0"/></xs:sequence>
 </xs:complexType></xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range []string{
		`<empty xmlns="urn:fixed">.</empty>`,
		`<mixed xmlns="urn:fixed">alpha<separator/> beta</mixed>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil {
			t.Fatal(validateErr)
		}
		if result.Valid || result.Diagnostics[0].Code != "cvc-elt.5.2.2.2.1" {
			t.Fatalf("Validate(%s) = %#v", instance, result)
		}
	}
}

func TestValidateFixedConstraintsUseAnonymousValueSpaces(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/anonymous-fixed.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:fixed" targetNamespace="urn:fixed">
 <xs:simpleType name="Number"><xs:restriction>
  <xs:simpleType><xs:restriction base="xs:decimal"/></xs:simpleType>
 </xs:restriction></xs:simpleType>
 <xs:element name="number" type="tns:Number" fixed="1.0"/>
 <xs:element name="record"><xs:complexType>
  <xs:attribute name="enabled" fixed="true"><xs:simpleType><xs:restriction base="xs:boolean"/></xs:simpleType></xs:attribute>
  <xs:attribute name="labels" fixed="a b"><xs:simpleType><xs:list itemType="xs:string"/></xs:simpleType></xs:attribute>
  <xs:attribute name="numbers" fixed="1 2"><xs:simpleType><xs:list><xs:simpleType><xs:restriction base="xs:decimal"/></xs:simpleType></xs:list></xs:simpleType></xs:attribute>
  <xs:attribute name="flag" fixed="true"><xs:simpleType><xs:union memberTypes="xs:boolean xs:string"/></xs:simpleType></xs:attribute>
  <xs:attribute name="amount" fixed="1"><xs:simpleType><xs:union><xs:simpleType><xs:restriction base="xs:decimal"/></xs:simpleType><xs:simpleType><xs:restriction base="xs:string"/></xs:simpleType></xs:union></xs:simpleType></xs:attribute>
 </xs:complexType></xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range []string{
		`<number xmlns="urn:fixed">1.00</number>`,
		`<record xmlns="urn:fixed" enabled="1" labels="a b" numbers="1.0 2.00" flag="1" amount="1.0"/>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || !result.Valid {
			t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
		}
	}
	for _, instance := range []string{
		`<record xmlns="urn:fixed" enabled="1" labels="a" numbers="1 2" flag="1" amount="1"/>`,
		`<record xmlns="urn:fixed" enabled="1" labels="a c" numbers="1 2" flag="1" amount="1"/>`,
		`<record xmlns="urn:fixed" enabled="1" labels="a b" numbers="1 3" flag="1" amount="1"/>`,
		`<record xmlns="urn:fixed" enabled="1" labels="a b" numbers="1 2" flag="false" amount="1"/>`,
		`<record xmlns="urn:fixed" enabled="1" labels="a b" numbers="1 2" flag="not-boolean" amount="1"/>`,
		`<record xmlns="urn:fixed" enabled="1" labels="a b" numbers="1 2" flag="1" amount="2"/>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil {
			t.Fatal(validateErr)
		}
		if result.Valid || result.Diagnostics[0].Code != "cvc-complex-type.3.1" {
			t.Fatalf("Validate(%s) = %#v", instance, result)
		}
	}
}

func TestValidateFixedConstraintsUseBuiltInValueSpaces(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/builtin-fixed.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:t="urn:fixed" targetNamespace="urn:fixed">
 <xs:element name="hex" type="xs:hexBinary" fixed="0a"/>
 <xs:element name="binary" type="xs:base64Binary" fixed="YQ=="/>
 <xs:element name="instant" type="xs:dateTime" fixed="2000-01-01T10:00:00Z"/>
 <xs:element name="duration" type="xs:duration" fixed="P1Y"/>
 <xs:element xmlns:a="urn:value" name="qname" type="xs:QName" fixed="a:item"/>
 <xs:element name="record"><xs:complexType>
  <xs:attribute xmlns:a="urn:value" name="qname" type="xs:QName" fixed="a:item"/>
 </xs:complexType></xs:element>
 <xs:simpleType name="QNameChoice"><xs:restriction base="xs:QName">
  <xs:enumeration xmlns:a="urn:value" value="a:item"/>
 </xs:restriction></xs:simpleType>
 <xs:element name="choice" type="t:QNameChoice"/>
 <xs:element name="inlineChoice"><xs:simpleType><xs:restriction base="xs:QName">
  <xs:enumeration xmlns:a="urn:value" value="a:item"/>
 </xs:restriction></xs:simpleType></xs:element>
 <xs:simpleType name="QNameList"><xs:list itemType="xs:QName"/></xs:simpleType>
 <xs:simpleType name="QNameListChoice"><xs:restriction base="t:QNameList">
  <xs:enumeration xmlns:a="urn:value" value="a:item a:other"/>
 </xs:restriction></xs:simpleType>
 <xs:element name="listChoice" type="t:QNameListChoice"/>
 <xs:element xmlns:a="urn:value" name="listFixed" type="t:QNameList" fixed="a:item a:other"/>
 <xs:simpleType name="QNameUnion"><xs:union memberTypes="xs:QName xs:string"/></xs:simpleType>
 <xs:simpleType name="QNameUnionChoice"><xs:restriction base="t:QNameUnion">
  <xs:enumeration xmlns:a="urn:value" value="a:item"/>
 </xs:restriction></xs:simpleType>
 <xs:element name="unionChoice" type="t:QNameUnionChoice"/>
 <xs:element xmlns:a="urn:value" name="unionFixed" type="t:QNameUnion" fixed="a:item"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range []string{
		`<hex xmlns="urn:fixed">0A</hex>`,
		`<binary xmlns="urn:fixed">Y Q = =</binary>`,
		`<instant xmlns="urn:fixed">2000-01-01T12:00:00+02:00</instant>`,
		`<duration xmlns="urn:fixed">P12M</duration>`,
		`<qname xmlns="urn:fixed" xmlns:v="urn:value">v:item</qname>`,
		`<record xmlns="urn:fixed" xmlns:v="urn:value" qname="v:item"/>`,
		`<choice xmlns="urn:fixed" xmlns:v="urn:value">v:item</choice>`,
		`<inlineChoice xmlns="urn:fixed" xmlns:v="urn:value">v:item</inlineChoice>`,
		`<listChoice xmlns="urn:fixed" xmlns:v="urn:value">v:item v:other</listChoice>`,
		`<listFixed xmlns="urn:fixed" xmlns:v="urn:value">v:item v:other</listFixed>`,
		`<unionChoice xmlns="urn:fixed" xmlns:v="urn:value">v:item</unionChoice>`,
		`<unionFixed xmlns="urn:fixed" xmlns:v="urn:value">v:item</unionFixed>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || !result.Valid {
			t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
		}
	}
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<choice xmlns="urn:fixed" xmlns:v="urn:value">v:other</choice>`),
	)
	if err != nil || result.Valid || result.Diagnostics[0].Code != "cvc-datatype-valid.1.2.1" {
		t.Fatalf("Validate(invalid QName enumeration) = %#v, %v", result, err)
	}
}

func TestValidatePatternUsesXMLSchemaMatchingSemantics(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:patterns",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:t="urn:patterns" targetNamespace="urn:patterns">
 <xs:simpleType name="Substring"><xs:restriction base="xs:string"><xs:pattern value="ab"/></xs:restriction></xs:simpleType>
 <xs:simpleType name="Anchors"><xs:restriction base="xs:string"><xs:pattern value="^ab$"/></xs:restriction></xs:simpleType>
 <xs:simpleType name="Dot"><xs:restriction base="xs:string"><xs:pattern value="a.b"/></xs:restriction></xs:simpleType>
 <xs:element name="substring" type="t:Substring"/>
 <xs:element name="anchors" type="t:Anchors"/>
 <xs:element name="dot" type="t:Dot"/>
 <xs:element name="fixed" type="t:Substring" fixed="ab"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range []string{
		`<substring xmlns="urn:patterns">ab</substring>`,
		`<anchors xmlns="urn:patterns">^ab$</anchors>`,
		`<dot xmlns="urn:patterns">a b</dot>`,
		`<fixed xmlns="urn:patterns">ab</fixed>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || !result.Valid {
			t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
		}
	}
	for _, instance := range []string{
		`<substring xmlns="urn:patterns">zabz</substring>`,
		`<anchors xmlns="urn:patterns">ab</anchors>`,
		`<anchors xmlns="urn:patterns">x^ab$y</anchors>`,
		"<dot xmlns=\"urn:patterns\">a\rb</dot>",
		"<dot xmlns=\"urn:patterns\">a\nb</dot>",
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || result.Valid || result.Diagnostics[0].Code != "cvc-datatype-valid.1.2.1" {
			t.Fatalf("Validate(%q) = %#v, %v", instance, result, validateErr)
		}
	}
}

func TestValidateAppliesDerivedWhitespaceBeforeOtherFacets(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:whitespace",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:t="urn:whitespace" targetNamespace="urn:whitespace">
 <xs:simpleType name="Replaced"><xs:restriction base="xs:string">
  <xs:whiteSpace value="replace"/>
 </xs:restriction></xs:simpleType>
 <xs:simpleType name="Collapsed"><xs:restriction base="t:Replaced">
  <xs:whiteSpace value="collapse"/>
  <xs:pattern value="[a-z]+ [a-z]+"/>
  <xs:enumeration value="alpha beta"/>
 </xs:restriction></xs:simpleType>
 <xs:element name="value" type="t:Collapsed"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(
		context.Background(),
		[]byte("<value xmlns=\"urn:whitespace\">  alpha\t beta  </value>"),
	)
	if err != nil || !result.Valid {
		t.Fatalf("Validate(collapsed whitespace) = %#v, %v", result, err)
	}
}

func TestValidateCombinesPatternsByDerivationStep(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:pattern-groups",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:t="urn:pattern-groups" targetNamespace="urn:pattern-groups">
 <xs:simpleType name="Base"><xs:restriction base="xs:string">
  <xs:pattern value="a"/><xs:pattern value="b"/>
 </xs:restriction></xs:simpleType>
 <xs:simpleType name="Derived"><xs:restriction base="t:Base">
  <xs:pattern value="a"/><xs:pattern value="c"/>
 </xs:restriction></xs:simpleType>
 <xs:element name="base" type="t:Base"/>
 <xs:element name="derived" type="t:Derived"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range []string{
		`<base xmlns="urn:pattern-groups">a</base>`,
		`<base xmlns="urn:pattern-groups">b</base>`,
		`<derived xmlns="urn:pattern-groups">a</derived>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || !result.Valid {
			t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
		}
	}
	for _, instance := range []string{
		`<base xmlns="urn:pattern-groups">c</base>`,
		`<derived xmlns="urn:pattern-groups">b</derived>`,
		`<derived xmlns="urn:pattern-groups">c</derived>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || result.Valid {
			t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
		}
	}
}

func TestValidateAttributeFixedValueAndGlobalReference(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/attributes.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:attributes" targetNamespace="urn:attributes">
 <xs:attribute name="mode" type="xs:string" fixed="X"/>
 <xs:element name="root" type="tns:Root"/>
 <xs:complexType name="Root"><xs:attribute ref="tns:mode"/></xs:complexType>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:attributes" xmlns:a="urn:attributes" a:mode="X"/>`),
	)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !result.Valid {
		t.Fatalf("valid Result = %#v", result)
	}

	result, err = validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:attributes" xmlns:a="urn:attributes" a:mode="Y"/>`),
	)
	if err != nil {
		t.Fatalf("Validate(fixed) error = %v", err)
	}
	if result.Valid || result.Diagnostics[0].Code != "cvc-complex-type.3.1" {
		t.Fatalf("fixed Result = %#v", result)
	}
}

func TestValidateRestrictionListAndUnionSimpleTypes(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/simple-types.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:types" targetNamespace="urn:types">
 <xs:simpleType name="Code"><xs:restriction base="xs:token">
  <xs:minLength value="2"/><xs:maxLength value="3"/>
  <xs:enumeration value="AB"/><xs:enumeration value="XYZ"/>
 </xs:restriction></xs:simpleType>
 <xs:simpleType name="Codes"><xs:list itemType="tns:Code"/></xs:simpleType>
 <xs:simpleType name="Choice"><xs:union memberTypes="xs:decimal tns:Code"/></xs:simpleType>
 <xs:element name="code" type="tns:Code"/>
 <xs:element name="codes" type="tns:Codes"/>
 <xs:element name="choice" type="tns:Choice"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, instance := range []string{
		`<code xmlns="urn:types">  AB  </code>`,
		`<codes xmlns="urn:types">AB   XYZ</codes>`,
		`<choice xmlns="urn:types">1.25</choice>`,
		`<choice xmlns="urn:types">XYZ</choice>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil {
			t.Fatalf("Validate(%s) error = %v", instance, validateErr)
		}
		if !result.Valid {
			t.Fatalf("Validate(%s) = %#v", instance, result)
		}
	}

	for _, instance := range []string{
		`<code xmlns="urn:types">A</code>`,
		`<codes xmlns="urn:types">AB BADX</codes>`,
		`<choice xmlns="urn:types">bad</choice>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil {
			t.Fatalf("Validate(%s) error = %v", instance, validateErr)
		}
		if result.Valid || result.Diagnostics[0].Code != "cvc-datatype-valid.1.2.1" {
			t.Fatalf("Validate(%s) = %#v", instance, result)
		}
	}
}

func TestValidateQNameValuesRequireBoundPrefixes(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/qname.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="urn:qname"
 targetNamespace="urn:qname">
 <xs:element name="name" type="xs:QName"/>
 <xs:simpleType name="Names"><xs:list itemType="xs:QName"/></xs:simpleType>
 <xs:element name="names" type="tns:Names"/>
 <xs:element name="record"><xs:complexType>
  <xs:attribute name="name" type="xs:QName"/>
  <xs:attribute name="names"><xs:simpleType><xs:list itemType="xs:QName"/></xs:simpleType></xs:attribute>
 </xs:complexType></xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range []string{
		`<name xmlns="urn:qname" xmlns:p="urn:value">p:item</name>`,
		`<names xmlns="urn:qname" xmlns:p="urn:value">p:first p:second</names>`,
		`<record xmlns="urn:qname" xmlns:p="urn:value" name="p:item" names="p:first p:second"/>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || !result.Valid {
			t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
		}
	}
	for _, instance := range []string{
		`<name xmlns="urn:qname">p:item</name>`,
		`<names xmlns="urn:qname">p:first p:second</names>`,
		`<record xmlns="urn:qname" name="item" names="p:first p:second"/>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil {
			t.Fatal(validateErr)
		}
		if result.Valid {
			t.Fatalf("Validate(%s) = %#v", instance, result)
		}
	}
}

func TestValidateBuiltInLexicalSpaces(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/built-ins.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:types"><xs:element name="language" type="xs:language"/>
 <xs:element name="binary" type="xs:hexBinary"/>
 <xs:element name="identifier" type="xs:ID"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, instance := range []string{
		`<language xmlns="urn:types">en-US</language>`,
		`<binary xmlns="urn:types">0aFE</binary>`,
		`<identifier xmlns="urn:types">customer-id</identifier>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || !result.Valid {
			t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
		}
	}
	for _, instance := range []string{
		`<language xmlns="urn:types">en_US</language>`,
		`<binary xmlns="urn:types">abc</binary>`,
		`<identifier xmlns="urn:types">1customer</identifier>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil {
			t.Fatalf("Validate(%s) error = %v", instance, validateErr)
		}
		if result.Valid || result.Diagnostics[0].Code != "cvc-datatype-valid.1.2.1" {
			t.Fatalf("Validate(%s) = %#v", instance, result)
		}
	}
}

func TestValidateComplexContentExtensionIncludesBaseContent(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/extension.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:extension" targetNamespace="urn:extension"
 elementFormDefault="qualified">
 <xs:complexType name="Base"><xs:sequence>
  <xs:element name="id" type="xs:string"/>
 </xs:sequence></xs:complexType>
 <xs:complexType name="Derived"><xs:complexContent>
  <xs:extension base="tns:Base"><xs:sequence>
   <xs:element name="extra" type="xs:decimal"/>
  </xs:sequence><xs:attribute name="mode" type="xs:string" use="required"/>
  </xs:extension>
 </xs:complexContent></xs:complexType>
 <xs:element name="root" type="tns:Derived"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:extension" mode="x"><id>A</id><extra>1.5</extra></root>`),
	)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !result.Valid {
		t.Fatalf("Result = %#v", result)
	}

	typeDefinition, ok := set.ComplexType(xsd.QName{Namespace: "urn:extension", Local: "Derived"})
	if !ok || typeDefinition.Derivation != xsd.DerivationExtension ||
		typeDefinition.Base != (xsd.QName{Namespace: "urn:extension", Local: "Base"}) {
		t.Fatalf("Derived = %#v", typeDefinition)
	}
}

func TestValidateExpandsModelAndAttributeGroups(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/groups.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:groups" targetNamespace="urn:groups"
 elementFormDefault="qualified">
 <xs:group name="Common"><xs:sequence>
  <xs:element name="id" type="xs:string"/>
 </xs:sequence></xs:group>
 <xs:attributeGroup name="Metadata">
  <xs:attribute name="status" type="xs:string" use="required"/>
 </xs:attributeGroup>
 <xs:complexType name="Root"><xs:sequence>
  <xs:group ref="tns:Common"/>
  <xs:element name="extra" type="xs:decimal"/>
 </xs:sequence><xs:attributeGroup ref="tns:Metadata"/></xs:complexType>
 <xs:element name="root" type="tns:Root"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:groups" status="ok"><id>A</id><extra>1.5</extra></root>`),
	)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !result.Valid {
		t.Fatalf("Result = %#v", result)
	}
}

func TestValidateElementAndAttributeWildcards(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/wildcards.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:wildcards" targetNamespace="urn:wildcards" elementFormDefault="qualified">
 <xs:complexType name="Root"><xs:sequence>
  <xs:any namespace="##other" processContents="lax" minOccurs="0" maxOccurs="unbounded"/>
 </xs:sequence><xs:anyAttribute namespace="##other" processContents="skip"/></xs:complexType>
	 <xs:element name="root" type="tns:Root"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := validator.Validate(context.Background(), []byte(
		`<root xmlns="urn:wildcards" xmlns:o="urn:other" o:status="ok"><o:item/></root>`,
	))
	if err != nil || !result.Valid {
		t.Fatalf("Validate(wildcards) = %#v, %v", result, err)
	}
	result, err = validator.Validate(context.Background(), []byte(
		`<root xmlns="urn:wildcards"><item/></root>`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatalf("target-namespace wildcard match = %#v", result)
	}
}

func TestValidateKeyUniqueAndKeyrefConstraints(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/identity.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:identity" targetNamespace="urn:identity" elementFormDefault="qualified">
 <xs:complexType name="Root"><xs:sequence>
  <xs:element name="item" minOccurs="0" maxOccurs="unbounded"><xs:complexType>
   <xs:attribute name="id" type="xs:string" use="required"/>
  </xs:complexType></xs:element>
  <xs:element name="reference" minOccurs="0" maxOccurs="unbounded"><xs:complexType>
   <xs:attribute name="target" type="xs:string" use="required"/>
  </xs:complexType></xs:element>
 </xs:sequence></xs:complexType>
 <xs:element name="root" type="tns:Root">
  <xs:key name="itemKey"><xs:selector xpath="tns:item"/><xs:field xpath="@id"/></xs:key>
  <xs:keyref name="itemRef" refer="tns:itemKey">
   <xs:selector xpath="tns:reference"/><xs:field xpath="@target"/>
  </xs:keyref>
 </xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, instance := range []string{
		`<root xmlns="urn:identity"><item id="A"/><reference target="A"/></root>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || !result.Valid {
			t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
		}
	}
	for _, instance := range []string{
		`<root xmlns="urn:identity"><item id="A"/><item id="A"/></root>`,
		`<root xmlns="urn:identity"><item id="A"/><reference target="B"/></root>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil {
			t.Fatalf("Validate(%s) error = %v", instance, validateErr)
		}
		if result.Valid || result.Diagnostics[0].Code != "cvc-identity-constraint" {
			t.Fatalf("Validate(%s) = %#v", instance, result)
		}
	}
	limited, err := validate.New(set, validate.Options{Limits: validate.Limits{
		MaxIdentityValues: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = limited.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:identity"><item id="A"/><item id="B"/></root>`),
	)
	if !errors.Is(err, validate.ErrLimitExceeded) {
		t.Fatalf("Validate(identity limit) error = %v, want ErrLimitExceeded", err)
	}
}

func TestValidateKeyrefUsesPropagatedDescendantKeyTables(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/propagated-identity.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:identity" targetNamespace="urn:identity" elementFormDefault="qualified">
 <xs:element name="root"><xs:complexType><xs:sequence>
  <xs:element name="keys" minOccurs="0" maxOccurs="unbounded"><xs:complexType>
   <xs:attribute name="id" type="xs:string" use="required"/>
  </xs:complexType><xs:key name="descendantKey">
   <xs:selector xpath="."/><xs:field xpath="@id"/>
  </xs:key></xs:element>
  <xs:element name="reference" minOccurs="0" maxOccurs="unbounded"><xs:complexType>
   <xs:attribute name="target" type="xs:string" use="required"/>
  </xs:complexType></xs:element>
 </xs:sequence></xs:complexType><xs:keyref name="rootReference" refer="tns:descendantKey">
  <xs:selector xpath="tns:reference"/><xs:field xpath="@target"/>
 </xs:keyref></xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name     string
		instance string
		valid    bool
	}{
		{
			name: "descendant key",
			instance: `<root xmlns="urn:identity"><keys id="A"/>` +
				`<reference target="A"/></root>`,
			valid: true,
		},
		{
			name: "missing descendant key",
			instance: `<root xmlns="urn:identity"><keys id="A"/>` +
				`<reference target="B"/></root>`,
		},
		{
			name: "conflicting descendant scopes",
			instance: `<root xmlns="urn:identity"><keys id="A"/><keys id="A"/><keys id="A"/>` +
				`<reference target="A"/></root>`,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, validateErr := validator.Validate(
				context.Background(),
				[]byte(test.instance),
			)
			if validateErr != nil {
				t.Fatal(validateErr)
			}
			if result.Valid != test.valid {
				t.Fatalf("Validate() = %#v, want valid %t", result, test.valid)
			}
		})
	}
}

func TestValidateRejectsNillableElementKeyFields(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/nillable-key.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:identity" targetNamespace="urn:identity" elementFormDefault="qualified">
 <xs:element name="root"><xs:complexType><xs:sequence>
  <xs:element name="code" type="xs:string" nillable="true"/>
 </xs:sequence></xs:complexType><xs:key name="codeKey">
  <xs:selector xpath="."/><xs:field xpath="tns:code"/>
 </xs:key></xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:identity"><code>A</code></root>`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || result.Diagnostics[0].Code != "cvc-identity-constraint" {
		t.Fatalf("Validate() = %#v", result)
	}
}

func TestValidateIdentityPrefixWildcardsAndDescendantFields(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/identity-paths.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:t="urn:identity-paths" targetNamespace="urn:identity-paths" elementFormDefault="qualified">
 <xs:element name="root"><xs:complexType><xs:sequence>
  <xs:element name="item" minOccurs="0" maxOccurs="unbounded"><xs:complexType><xs:sequence>
   <xs:element name="box"><xs:complexType><xs:sequence><xs:element name="value" type="xs:string"/></xs:sequence></xs:complexType></xs:element>
  </xs:sequence><xs:attribute name="id" type="xs:string" use="required"/></xs:complexType></xs:element>
 </xs:sequence></xs:complexType>
 <xs:unique name="ids"><xs:selector xpath=" t:* "/><xs:field xpath=" @ id "/></xs:unique>
	<xs:unique name="axisIds"><xs:selector xpath="child::t:item"/><xs:field xpath="attribute::id"/></xs:unique>
 <xs:unique name="values"><xs:selector xpath="t:item"/><xs:field xpath=". // t:value"/></xs:unique>
 </xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range []string{
		`<root xmlns="urn:identity-paths"><item id="A"><box><value>one</value></box></item><item id="B"><box><value>two</value></box></item></root>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || !result.Valid {
			t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
		}
	}
	for _, instance := range []string{
		`<root xmlns="urn:identity-paths"><item id="A"><box><value>one</value></box></item><item id="A"><box><value>two</value></box></item></root>`,
		`<root xmlns="urn:identity-paths"><item id="A"><box><value>one</value></box></item><item id="B"><box><value>one</value></box></item></root>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || result.Valid || result.Diagnostics[0].Code != "cvc-identity-constraint" {
			t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
		}
	}
}

func TestValidateIdentityConstraintsUseBuiltInValueEquality(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		typeName    string
		declaration string
		first       string
		second      string
		namespaces  string
	}{
		{name: "boolean", typeName: "xs:boolean", first: "1", second: "true"},
		{name: "float", typeName: "xs:double", first: "1", second: "1.0"},
		{name: "hex", typeName: "xs:hexBinary", first: "0A", second: "0a"},
		{name: "base64", typeName: "xs:base64Binary", first: "YQ==", second: "YQ=="},
		{name: "duration", typeName: "xs:duration", first: "P1Y", second: "P12M"},
		{name: "dateTime", typeName: "xs:dateTime", first: "2000-01-01T00:00:00Z", second: "1999-12-31T19:00:00-05:00"},
		{name: "QName", typeName: "xs:QName", first: "a:item", second: "b:item", namespaces: ` xmlns:a="urn:value" xmlns:b="urn:value"`},
		{name: "list", typeName: "t:List", declaration: `<xs:simpleType name="List"><xs:list itemType="xs:integer"/></xs:simpleType>`, first: "01 2", second: "1 02"},
		{name: "union", typeName: "t:Union", declaration: `<xs:simpleType name="Union"><xs:union memberTypes="xs:decimal xs:string"/></xs:simpleType>`, first: "01", second: "1"},
		{name: "simple content", typeName: "t:SimpleContent", declaration: `<xs:complexType name="SimpleContent"><xs:simpleContent><xs:extension base="xs:integer"/></xs:simpleContent></xs:complexType>`, first: "01", second: "1"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			compiler, err := compile.New(compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			set, err := compiler.Compile(context.Background(), compile.Source{
				URI: "https://example.test/identity-values.xsd",
				Content: []byte(fmt.Sprintf(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:t="urn:identity-values" targetNamespace="urn:identity-values" elementFormDefault="qualified">
 %s
 <xs:element name="root"><xs:complexType><xs:sequence>
  <xs:element name="value" type="%s" maxOccurs="unbounded"/>
 </xs:sequence></xs:complexType><xs:unique name="values">
  <xs:selector xpath="t:value"/><xs:field xpath="."/>
 </xs:unique></xs:element></xs:schema>`, test.declaration, test.typeName)),
			})
			if err != nil {
				t.Fatal(err)
			}
			validator, err := validate.New(set, validate.Options{})
			if err != nil {
				t.Fatal(err)
			}
			instance := fmt.Sprintf(
				`<root xmlns="urn:identity-values"%s><value>%s</value><value>%s</value></root>`,
				test.namespaces,
				test.first,
				test.second,
			)
			result, err := validator.Validate(context.Background(), []byte(instance))
			if err != nil || result.Valid || result.Diagnostics[0].Code != "cvc-identity-constraint" {
				t.Fatalf("Validate(%s) = %#v, %v", instance, result, err)
			}
		})
	}
}

func TestValidateAnonymousElementTypes(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/anonymous.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:anonymous" elementFormDefault="qualified">
 <xs:element name="root"><xs:complexType><xs:sequence>
  <xs:element name="code"><xs:simpleType><xs:restriction base="xs:string">
   <xs:minLength value="2"/>
  </xs:restriction></xs:simpleType></xs:element>
 </xs:sequence><xs:attribute name="status" use="required"><xs:simpleType>
  <xs:restriction base="xs:string"><xs:enumeration value="ok"/></xs:restriction>
 </xs:simpleType></xs:attribute>
 </xs:complexType></xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:anonymous" status="ok"><code>AB</code></root>`),
	)
	if err != nil || !result.Valid {
		t.Fatalf("Validate(anonymous) = %#v, %v", result, err)
	}
	result, err = validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:anonymous"><code>A</code></root>`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatalf("Validate(invalid anonymous) = %#v", result)
	}
	result, err = validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:anonymous" status="bad"><code>AB</code></root>`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatalf("Validate(invalid anonymous attribute) = %#v", result)
	}
}

func TestValidateSubstitutionGroupMembers(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/substitution.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:substitution" targetNamespace="urn:substitution" elementFormDefault="qualified">
 <xs:element name="head" type="xs:string" abstract="true"/>
 <xs:element name="member" type="xs:normalizedString" substitutionGroup="tns:head"/>
 <xs:complexType name="Container"><xs:sequence><xs:element ref="tns:head"/></xs:sequence></xs:complexType>
 <xs:element name="root" type="tns:Container"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:substitution"><member>ok</member></root>`),
	)
	if err != nil || !result.Valid {
		t.Fatalf("Validate(substitution) = %#v, %v", result, err)
	}
}

func TestSubstitutionGroupMemberInheritsHeadType(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/inherited-substitution.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:substitution" targetNamespace="urn:substitution">
 <xs:element name="head" type="xs:boolean"/>
 <xs:element name="member" substitutionGroup="tns:head"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<member xmlns="urn:substitution">not-a-boolean</member>`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatalf("Validate() = %#v", result)
	}
}

func TestValidateBlockedSubstitutionGroupDerivation(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/blocked-substitution.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:blocked" targetNamespace="urn:blocked" elementFormDefault="qualified">
 <xs:element name="head" type="xs:string" block="restriction"/>
 <xs:element name="member" type="xs:normalizedString" substitutionGroup="tns:head"/>
 <xs:complexType name="Container"><xs:sequence><xs:element ref="tns:head"/></xs:sequence></xs:complexType>
 <xs:element name="root" type="tns:Container"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:blocked"><member>value</member></root>`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatalf("Validate(blocked substitution) = %#v", result)
	}
}

func TestValidateIDAndIDREFSemantics(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/id.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:id" elementFormDefault="qualified">
 <xs:element name="root"><xs:complexType><xs:sequence maxOccurs="unbounded">
  <xs:element name="item"><xs:complexType>
   <xs:attribute name="id" type="xs:ID"/>
   <xs:attribute name="ref" type="xs:IDREFS"/>
  </xs:complexType></xs:element>
 </xs:sequence></xs:complexType></xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, instance := range []string{
		`<root xmlns="urn:id"><item id="a"/><item id="a"/></root>`,
		`<root xmlns="urn:id"><item id="a" ref="missing"/></root>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil {
			t.Fatal(validateErr)
		}
		if result.Valid {
			t.Fatalf("Validate(%s) = valid, want invalid ID semantics", instance)
		}
	}
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:id"><item ref="b"/><item id="b"/></root>`),
	)
	if err != nil || !result.Valid {
		t.Fatalf("Validate(forward IDREF) = %#v, %v", result, err)
	}
}

func TestValidateSimpleContentDerivedFromComplexType(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/simple-content.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns="urn:simple-content" targetNamespace="urn:simple-content">
 <xs:complexType name="A"><xs:simpleContent><xs:extension base="xs:string">
  <xs:attribute name="a" type="xs:int" use="required"/>
 </xs:extension></xs:simpleContent></xs:complexType>
 <xs:complexType name="B"><xs:simpleContent><xs:extension base="A">
  <xs:attribute name="b" type="xs:boolean" use="required"/>
 </xs:extension></xs:simpleContent></xs:complexType>
 <xs:element name="root" type="B"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<root xmlns="urn:simple-content" a="1" b="true">value</root>`),
	)
	if err != nil || !result.Valid {
		t.Fatalf("Validate(simple content extension) = %#v, %v", result, err)
	}
}

func TestValidateAnyTypes(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/any-types.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:any" elementFormDefault="qualified">
 <xs:element name="simple" type="xs:anySimpleType"/>
 <xs:element name="complex" type="xs:anyType"/>
 <xs:element name="item" type="xs:boolean"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range []string{
		`<simple xmlns="urn:any" xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="xs:int">1</simple>`,
		`<complex xmlns="urn:any" extra="yes"><child xmlns="urn:other"/></complex>`,
		`<undeclared xmlns="urn:any" xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="xs:int">1</undeclared>`,
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || !result.Valid {
			t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
		}
	}
	result, err := validator.Validate(
		context.Background(),
		[]byte(`<complex xmlns="urn:any"><item>not-boolean</item></complex>`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatalf("Validate(anyType declared child) = %#v", result)
	}
}

func TestValidateInlineListAndUnionMembers(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/inline-members.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:simpleType name="List"><xs:list><xs:simpleType>
  <xs:restriction base="xs:int"><xs:minInclusive value="1"/></xs:restriction>
 </xs:simpleType></xs:list></xs:simpleType>
 <xs:simpleType name="Union"><xs:union><xs:simpleType>
  <xs:restriction base="xs:string"><xs:pattern value="[A-Z]+"/></xs:restriction>
 </xs:simpleType><xs:simpleType><xs:restriction base="xs:int"/></xs:simpleType>
 </xs:union></xs:simpleType>
 <xs:element name="list" type="List"/><xs:element name="union" type="Union"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range []string{`<list>1 2</list>`, `<union>ABC</union>`, `<union>3</union>`} {
		result, validateErr := validator.Validate(context.Background(), []byte(instance))
		if validateErr != nil || !result.Valid {
			t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
		}
	}
}

func TestValidateSimpleContentRestrictionFacets(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/simple-content-restriction.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:complexType name="Base"><xs:simpleContent><xs:extension base="xs:string"/>
 </xs:simpleContent></xs:complexType>
 <xs:complexType name="Restricted"><xs:simpleContent><xs:restriction base="Base">
  <xs:simpleType><xs:restriction base="xs:string"><xs:minLength value="2"/>
  </xs:restriction></xs:simpleType><xs:maxLength value="3"/>
  <xs:pattern value="[A-Z]+"/>
 </xs:restriction></xs:simpleContent></xs:complexType>
 <xs:element name="value" type="Restricted"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		instance string
		valid    bool
	}{
		{instance: `<value>AB</value>`, valid: true},
		{instance: `<value>A</value>`},
		{instance: `<value>ABCD</value>`},
		{instance: `<value>ab</value>`},
	} {
		result, validateErr := validator.Validate(
			context.Background(),
			[]byte(test.instance),
		)
		if validateErr != nil {
			t.Fatalf("Validate(%s) error = %v", test.instance, validateErr)
		}
		if result.Valid != test.valid {
			t.Fatalf("Validate(%s) = %#v, want valid %t", test.instance, result, test.valid)
		}
	}
}

func TestValidateSimpleContentRestrictionOfEmptiableMixedContent(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/mixed-simple-content.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:complexType name="Mixed" mixed="true"><xs:sequence>
  <xs:element name="optional" minOccurs="0"/>
 </xs:sequence></xs:complexType>
 <xs:complexType name="Restricted"><xs:simpleContent><xs:restriction base="Mixed">
  <xs:simpleType><xs:restriction base="xs:string">
   <xs:pattern value="[A-Z]+"/>
  </xs:restriction></xs:simpleType>
 </xs:restriction></xs:simpleContent></xs:complexType>
 <xs:element name="value" type="Restricted"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		instance string
		valid    bool
	}{
		{instance: `<value>ABC</value>`, valid: true},
		{instance: `<value>abc</value>`},
	} {
		result, validateErr := validator.Validate(context.Background(), []byte(test.instance))
		if validateErr != nil || result.Valid != test.valid {
			t.Fatalf("Validate(%s) = %#v, %v", test.instance, result, validateErr)
		}
	}
}

func TestValidateFixedValuesForRestrictedSimpleContent(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/fixed-simple-content.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:complexType name="Extended"><xs:simpleContent>
  <xs:extension base="xs:boolean"/>
 </xs:simpleContent></xs:complexType>
 <xs:complexType name="Restricted"><xs:simpleContent>
  <xs:restriction base="Extended"><xs:enumeration value="true"/>
  </xs:restriction>
 </xs:simpleContent></xs:complexType>
 <xs:element name="extended" type="Extended" fixed="true"/>
 <xs:element name="restricted" type="Restricted" fixed="true"/>
 <xs:element name="anonymous" fixed="true"><xs:complexType>
  <xs:simpleContent><xs:restriction base="Extended">
   <xs:enumeration value="true"/>
  </xs:restriction></xs:simpleContent>
 </xs:complexType></xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"extended", "restricted", "anonymous"} {
		for _, test := range []struct {
			value string
			valid bool
		}{
			{value: "1", valid: true},
			{value: "false"},
		} {
			instance := fmt.Sprintf("<%s>%s</%s>", name, test.value, name)
			result, validateErr := validator.Validate(context.Background(), []byte(instance))
			if validateErr != nil || result.Valid != test.valid {
				t.Fatalf("Validate(%s) = %#v, %v", instance, result, validateErr)
			}
		}
	}
}

func newValidator(t *testing.T) *validate.Validator {
	t.Helper()
	validator, err := validate.New(validatorSet(t), validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return validator
}

func validatorSet(t *testing.T) *compile.Set {
	t.Helper()
	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/order.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:order"
 targetNamespace="urn:order">
 <xs:element name="amount" type="xs:decimal"/>
 <xs:element name="template" type="xs:string" abstract="true"/>
 <xs:element name="complex" type="tns:Complex"/>
 <xs:complexType name="Complex"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func complexValidator(t *testing.T) *validate.Validator {
	t.Helper()
	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/complex.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:order" targetNamespace="urn:order"
 elementFormDefault="qualified">
 <xs:element name="note" type="xs:string"/>
 <xs:element name="order" type="tns:Order"/>
 <xs:complexType name="Order">
  <xs:sequence>
   <xs:element name="id" type="xs:string"/>
   <xs:choice minOccurs="0" maxOccurs="unbounded">
    <xs:element name="price" type="xs:decimal"/>
    <xs:element ref="tns:note"/>
   </xs:choice>
  </xs:sequence>
  <xs:attribute name="status" type="xs:string" use="required"/>
 </xs:complexType>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return validator
}
