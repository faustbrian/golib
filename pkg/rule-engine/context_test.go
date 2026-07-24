package ruleengine_test

import (
	"math"
	"testing"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

func TestContextDistinguishesMissingNullAndValues(t *testing.T) {
	t.Parallel()

	name := ruleengine.MustPath("shipment", "recipient", "name")
	note := ruleengine.MustPath("shipment", "note")
	missing := ruleengine.MustPath("shipment", "recipient", "phone")

	context, err := ruleengine.NewContext(
		ruleengine.Fact{Path: name, Value: ruleengine.String("Ada"), Owner: ruleengine.OwnerSubject},
		ruleengine.Fact{Path: note, Value: ruleengine.Null(), Owner: ruleengine.OwnerSubject},
	)
	if err != nil {
		t.Fatalf("NewContext() error = %v", err)
	}

	assertValue(t, context.Lookup(name), ruleengine.KindString, "Ada")
	assertValue(t, context.Lookup(note), ruleengine.KindNull, nil)
	assertValue(t, context.Lookup(missing), ruleengine.KindMissing, nil)
}

func TestContextCopiesMutableInputAndOutput(t *testing.T) {
	t.Parallel()

	path := ruleengine.MustPath("shipment", "labels")
	labels := []ruleengine.Value{ruleengine.String("fragile")}
	context, err := ruleengine.NewContext(ruleengine.Fact{
		Path:  path,
		Value: ruleengine.List(labels...),
		Owner: ruleengine.OwnerResource,
	})
	if err != nil {
		t.Fatalf("NewContext() error = %v", err)
	}

	labels[0] = ruleengine.String("mutated")
	first := context.Lookup(path)
	items, ok := first.ListValue()
	if !ok {
		t.Fatal("ListValue() did not return a list")
	}
	items[0] = ruleengine.String("also-mutated")

	second, _ := context.Lookup(path).ListValue()
	if got, _ := second[0].StringValue(); got != "fragile" {
		t.Fatalf("stored label = %q, want fragile", got)
	}
}

func TestContextRejectsInvalidFacts(t *testing.T) {
	t.Parallel()

	path := ruleengine.MustPath("shipment", "weight")
	for _, test := range []struct {
		name  string
		facts []ruleengine.Fact
	}{
		{name: "duplicate path", facts: []ruleengine.Fact{{Path: path, Value: ruleengine.Int(1)}, {Path: path, Value: ruleengine.Int(2)}}},
		{name: "missing value", facts: []ruleengine.Fact{{Path: path, Value: ruleengine.Missing()}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ruleengine.NewContext(test.facts...); err == nil {
				t.Fatal("NewContext() error = nil, want error")
			}
		})
	}
}

func TestPathValidation(t *testing.T) {
	t.Parallel()

	limits := ruleengine.DefaultLimits()
	if _, err := ruleengine.NewPath(limits, "shipment", "", "name"); err == nil {
		t.Fatal("NewPath() accepted an empty segment")
	}
	if _, err := ruleengine.NewPath(limits, "shipment", "..", "name"); err == nil {
		t.Fatal("NewPath() accepted traversal")
	}
	if _, err := ruleengine.NewPath(limits, "shipment.recipient", "name"); err == nil {
		t.Fatal("NewPath() accepted an ambiguous dotted segment")
	}
	if _, err := ruleengine.NewPath(limits, "shipment", "line\nfeed"); err == nil {
		t.Fatal("NewPath() accepted a control character")
	}
	if _, err := ruleengine.NewPath(limits, "shipment", string(make([]byte, limits.MaxPathBytes))); err == nil {
		t.Fatal("NewPath() accepted an oversized path")
	}
}

func TestContextRejectsUnknownOwnersAndNonFiniteCollections(t *testing.T) {
	t.Parallel()

	path := ruleengine.MustPath("shipment", "value")
	tests := []ruleengine.Fact{
		{Path: path, Value: ruleengine.Int(1), Owner: ruleengine.Owner(255)},
		{Path: path, Value: ruleengine.List(ruleengine.Float(math.Inf(-1)))},
	}
	for _, fact := range tests {
		if _, err := ruleengine.NewContext(fact); !ruleengine.IsCode(err, ruleengine.CodeInvalidFact) {
			t.Fatalf("NewContext() error = %v, want invalid fact", err)
		}
	}
}

func TestContextRejectsInvalidUTF8Text(t *testing.T) {
	t.Parallel()

	path := ruleengine.MustPath("shipment", "note")
	invalid := string([]byte{0xff})
	for _, value := range []ruleengine.Value{
		ruleengine.String(invalid),
		ruleengine.List(ruleengine.String(invalid)),
	} {
		if _, err := ruleengine.NewContext(ruleengine.Fact{Path: path, Value: value}); !ruleengine.IsCode(err, ruleengine.CodeInvalidFact) {
			t.Fatalf("NewContext() error = %v, want invalid fact", err)
		}
	}
}

func assertValue(t *testing.T, value ruleengine.Value, kind ruleengine.Kind, want any) {
	t.Helper()
	if value.Kind() != kind {
		t.Fatalf("Kind() = %v, want %v", value.Kind(), kind)
	}
	if value.Interface() != want {
		t.Fatalf("Interface() = %#v, want %#v", value.Interface(), want)
	}
}
