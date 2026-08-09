package ruleengine_test

import (
	"bytes"
	"context"
	"testing"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

func TestCanonicalDefinitionsHaveStableHashes(t *testing.T) {
	t.Parallel()

	country := ruleengine.MustPath("shipment", "country")
	ruleA := ruleengine.Rule{ID: "a", Priority: 10, Tags: []string{"location", "shipping"}, When: ruleengine.True()}
	ruleB := ruleengine.Rule{ID: "b", When: ruleengine.Compare(ruleengine.OpIn,
		ruleengine.Variable(country),
		ruleengine.Literal(ruleengine.List(ruleengine.String("FI"), ruleengine.String("SE")))),
	}
	first := ruleengine.RuleSet{ID: "routing", Namespace: "location", Strategy: ruleengine.CollectAll, Rules: []ruleengine.Rule{ruleB, ruleA}}
	ruleA.Tags = []string{"shipping", "location"}
	second := ruleengine.RuleSet{ID: "routing", Namespace: "location", Strategy: ruleengine.CollectAll, Rules: []ruleengine.Rule{ruleA, ruleB}}

	firstJSON, err := ruleengine.MarshalCanonical(first)
	if err != nil {
		t.Fatalf("MarshalCanonical(first) error = %v", err)
	}
	secondJSON, err := ruleengine.MarshalCanonical(second)
	if err != nil {
		t.Fatalf("MarshalCanonical(second) error = %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("canonical definitions differ:\n%s\n%s", firstJSON, secondJSON)
	}
	firstHash, _ := ruleengine.CanonicalHash(first)
	secondHash, _ := ruleengine.CanonicalHash(second)
	if firstHash != secondHash || len(firstHash) != 64 {
		t.Fatalf("hashes = %q and %q", firstHash, secondHash)
	}
}

func TestJSONDefinitionRoundTripPreservesEvaluation(t *testing.T) {
	t.Parallel()

	path := ruleengine.MustPath("shipment", "weight")
	set := ruleengine.RuleSet{ID: "weight", Rules: []ruleengine.Rule{{
		ID: "heavy",
		When: ruleengine.All(
			ruleengine.Exists(path),
			ruleengine.Compare(ruleengine.OpGreaterThan, ruleengine.Variable(path), ruleengine.Literal(ruleengine.Int(10))),
		),
	}}}
	encoded, err := ruleengine.MarshalCanonical(set)
	if err != nil {
		t.Fatalf("MarshalCanonical() error = %v", err)
	}
	decoded, diagnostics, err := ruleengine.ParseJSON(encoded, ruleengine.DefaultLimits())
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("ParseJSON() diagnostics = %#v, error = %v", diagnostics, err)
	}
	plan, _, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), decoded)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	facts, _ := ruleengine.NewContext(ruleengine.Fact{Path: path, Value: ruleengine.Int(11)})
	if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
		t.Fatalf("Evaluate() = %#v", result)
	}
}

func TestCompilerCanonicalPersistenceUsesCustomOperatorRegistry(t *testing.T) {
	t.Parallel()

	path := ruleengine.MustPath("parcel", "code")
	set := ruleengine.RuleSet{ID: "custom-persistence", Rules: []ruleengine.Rule{{
		ID: "same-length",
		When: ruleengine.Compare(
			"same_length",
			ruleengine.Variable(path),
			ruleengine.Literal(ruleengine.String("ABC")),
		),
	}}}
	if _, err := ruleengine.MarshalCanonical(set); !ruleengine.IsCode(err, ruleengine.CodeUnknownOperator) {
		t.Fatalf("package MarshalCanonical() error = %v", err)
	}
	compiler, err := ruleengine.NewCompilerWithOperators(
		ruleengine.DefaultLimits(), sameLengthOperator{},
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := compiler.MarshalCanonical(set)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := compiler.CanonicalHash(set)
	if err != nil || len(hash) != 64 {
		t.Fatalf("compiler CanonicalHash() = %q, %v", hash, err)
	}
	if _, _, parseErr := ruleengine.ParseJSON(encoded, ruleengine.DefaultLimits()); !ruleengine.IsCode(parseErr, ruleengine.CodeUnknownOperator) {
		t.Fatalf("package ParseJSON() error = %v", parseErr)
	}
	decoded, diagnostics, err := compiler.ParseJSON(encoded)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("compiler ParseJSON() diagnostics = %#v, error = %v", diagnostics, err)
	}
	plan, _, err := compiler.Compile(context.Background(), decoded)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := ruleengine.NewContext(ruleengine.Fact{Path: path, Value: ruleengine.String("XYZ")})
	if err != nil {
		t.Fatal(err)
	}
	if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
		t.Fatalf("round-tripped Evaluate() = %#v", result)
	}
}

func TestJSONDefinitionRejectsUnknownFieldsAndUnsupportedPredicates(t *testing.T) {
	t.Parallel()

	input := []byte(`{"version":"1","id":"rules","strategy":"first_match","rules":[],"secret":"do-not-disclose"}`)
	_, diagnostics, err := ruleengine.ParseJSON(input, ruleengine.DefaultLimits())
	if err == nil || len(diagnostics) == 0 {
		t.Fatalf("ParseJSON() diagnostics = %#v, error = %v", diagnostics, err)
	}
	if bytes.Contains([]byte(diagnostics[0].Message), []byte("do-not-disclose")) {
		t.Fatal("diagnostic disclosed source content")
	}

	set := ruleengine.RuleSet{ID: "custom", Rules: []ruleengine.Rule{{
		ID: "custom",
		When: ruleengine.PredicateFunc(func(context.Context, ruleengine.Context) (bool, error) {
			return true, nil
		}),
	}}}
	if _, err := ruleengine.MarshalCanonical(set); !ruleengine.IsCode(err, ruleengine.CodeNotSerializable) {
		t.Fatalf("MarshalCanonical() error = %v", err)
	}
}

func TestJSONDefinitionRejectsAmbiguousKnownFields(t *testing.T) {
	t.Parallel()

	tests := []string{
		`{"version":"1","id":"rules","strategy":"first_match","rules":[{"id":"r","priority":0,"tags":[],"derive":[],"when":{"kind":"true","path":["ignored"]}}]}`,
		`{"version":"1","id":"rules","strategy":"first_match","rules":[{"id":"r","priority":0,"tags":[],"derive":[],"when":{"kind":"compare","operator":"equal","left":{"kind":"literal","path":["ignored"],"value":{"type":"bool","bool":true}},"right":{"kind":"literal","value":{"type":"bool","bool":true}}}}]}`,
		`{"version":"1","id":"rules","strategy":"first_match","rules":[{"id":"r","priority":0,"tags":[],"when":{"kind":"true"},"derive":[{"path":["derived","x"],"owner":"subject","value":{"type":"bool","bool":true,"int":1}}]}]}`,
	}
	for _, input := range tests {
		if _, _, err := ruleengine.ParseJSON([]byte(input), ruleengine.DefaultLimits()); !ruleengine.IsCode(err, ruleengine.CodeInvalidJSON) {
			t.Fatalf("ParseJSON(%s) error = %v", input, err)
		}
	}
}
