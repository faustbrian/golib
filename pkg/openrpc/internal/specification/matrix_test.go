package specification

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormativeSentencesInventoriesStatements(t *testing.T) {
	t.Parallel()

	input := `The key words "MUST" and "MAY" are interpreted as BCP 14.

Values MUST be unique. Tooling SHOULD reject duplicates.

Ordinary descriptive text.`

	statements := normativeSentences("spec.md", input)
	if len(statements) != 2 {
		t.Fatalf("got %d statements, want 2: %#v", len(statements), statements)
	}
	if statements[0].Level != "MUST" || statements[0].Text != "Values MUST be unique." {
		t.Fatalf("unexpected first statement: %#v", statements[0])
	}
	if statements[1].Level != "SHOULD" || statements[1].Line != 3 {
		t.Fatalf("unexpected second statement: %#v", statements[1])
	}
}

func TestNormativeSentencesPreservesLinksCodeAndVersions(t *testing.T) {
	t.Parallel()

	input := "Tooling SHOULD support [the standard](https://example.com/v1.0/spec). " +
		"It MUST preserve `openrpc.json` and `1.4.1`."
	statements := normativeSentences("spec.md", input)
	want := []string{
		"Tooling SHOULD support [the standard](https://example.com/v1.0/spec).",
		"It MUST preserve `openrpc.json` and `1.4.1`.",
	}
	if len(statements) != len(want) {
		t.Fatalf("statements = %#v", statements)
	}
	for index, text := range want {
		if statements[index].Text != text {
			t.Errorf("statements[%d] = %q, want %q", index, statements[index].Text, text)
		}
	}
	if tail := splitSentences("trailing text"); len(tail) != 1 || tail[0] != "trailing text" {
		t.Fatalf("trailing sentence = %#v", tail)
	}
	if leading := splitSentences(". Next"); len(leading) != 2 || leading[0] != "." {
		t.Fatalf("leading punctuation sentences = %#v", leading)
	}
	if tabbed := splitSentences(".\tNext"); len(tabbed) != 2 || tabbed[0] != "." {
		t.Fatalf("tabbed sentences = %#v", tabbed)
	}
}

func TestFieldRowsInventoriesNestedObjectFields(t *testing.T) {
	t.Parallel()

	var schema any
	err := json.Unmarshal([]byte(`{
		"type":"object",
		"required":["name"],
		"properties":{
			"name":{"type":"string"},
			"enabled":{"type":["boolean","null"],"default":false},
			"payload":{}
		},
		"patternProperties":{"^x-":{}},
		"additionalProperties":false
	}`), &schema)
	if err != nil {
		t.Fatal(err)
	}

	rows := fieldRows(schema)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3: %#v", len(rows), rows)
	}
	if !rows[0].Required || rows[0].Field != "name" {
		t.Fatalf("unexpected required field: %#v", rows[0])
	}
	if !rows[1].Nullable || rows[1].Default != "false" {
		t.Fatalf("unexpected optional field: %#v", rows[1])
	}
	if !rows[2].Nullable || rows[2].Field != "payload" {
		t.Fatalf("unexpected unconstrained field: %#v", rows[2])
	}
	if !rows[0].Extensible || rows[0].UnknownFields != "reject" {
		t.Fatalf("object policy missing from row: %#v", rows[0])
	}
}

func TestGenerateMatricesProducesTraceableRows(t *testing.T) {
	t.Parallel()

	specification := "Values MUST be unique."
	schema := []byte(`{
		"type":"object",
		"required":["name"],
		"properties":{"name":{"type":"string","description":"The name SHALL be stable."}}
	}`)

	normative, fields, err := GenerateMatrices(specification, schema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(normative), "ORPC-1.4-0001\tspec-template.md\t1\tMUST\tValues MUST be unique.") {
		t.Fatalf("missing prose requirement:\n%s", normative)
	}
	if !strings.Contains(string(normative), "schema.json#/properties/name/description\t0\tSHALL") {
		t.Fatalf("missing schema requirement:\n%s", normative)
	}
	if !strings.Contains(string(fields), "#\tname\t\"string\"\ttrue\tfalse") {
		t.Fatalf("missing field row:\n%s", fields)
	}
}

func TestApplyFieldEvidenceRequiresCompleteExactObjectCoverage(t *testing.T) {
	t.Parallel()

	inventory := []byte("object\tfield\tshape\trequired\tnullable\tdefault\textensions\tunknownFields\tmodel\tvalidation\tevidence\tstatus\n#\tname\tstring\ttrue\tfalse\t\tfalse\treject\t\t\t\tunimplemented\n")
	review := []byte("object\tmodel\tvalidation\tevidence\tstatus\n#\tmodel.go:Value\tvalidate.go:Value\tvalue_test.go:TestValue\tcomplete\n")

	completed, err := ApplyFieldEvidence(inventory, review)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(completed), "\tmodel.go:Value\tvalidate.go:Value\tvalue_test.go:TestValue\tcomplete\n") {
		t.Fatalf("completed inventory = %s", completed)
	}
	for _, invalid := range [][]byte{
		[]byte("invalid\n"),
		[]byte("object\tmodel\tvalidation\tevidence\tstatus\nother\ta\tb\tc\tcomplete\n"),
		[]byte("object\tmodel\tvalidation\tevidence\tstatus\n#\ta\tb\tc\tpartial\n"),
	} {
		if _, err := ApplyFieldEvidence(inventory, invalid); err == nil {
			t.Fatalf("ApplyFieldEvidence accepted %q", invalid)
		}
	}
}

func TestApplyNormativeEvidenceRequiresCompleteExactCoverage(t *testing.T) {
	t.Parallel()

	inventory := []byte("id\tsource\tline\tlevel\trequirement\timplementation\tevidence\tstatus\nORPC-1.4-0001\tspec.md\t1\tMUST\tValues MUST be unique.\t\t\tunimplemented\n")
	review := []byte("id\timplementation\tevidence\tstatus\tnotes\nORPC-1.4-0001\tvalue.go:Parse\tvalue_test.go:TestParse\tcomplete\tVerified.\n")

	completed, err := ApplyNormativeEvidence(inventory, review)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(completed), "\tvalue.go:Parse\tvalue_test.go:TestParse\tcomplete\n") {
		t.Fatalf("completed inventory = %s", completed)
	}
	for _, invalid := range [][]byte{
		[]byte("invalid\n"),
		[]byte("id\timplementation\tevidence\tstatus\tnotes\nother\ta\tb\tcomplete\tnote\n"),
		[]byte("id\timplementation\tevidence\tstatus\tnotes\nORPC-1.4-0001\ta\tb\tpartial\tnote\n"),
	} {
		if _, err := ApplyNormativeEvidence(inventory, invalid); err == nil {
			t.Fatalf("ApplyNormativeEvidence accepted %q", invalid)
		}
	}
	duplicate := append(append([]byte(nil), review...), []byte("ORPC-1.4-0001\ta\tb\tcomplete\tnote\n")...)
	if _, err := ApplyNormativeEvidence(inventory, duplicate); err == nil {
		t.Fatal("ApplyNormativeEvidence accepted duplicate evidence")
	}
	if _, err := ApplyNormativeEvidence([]byte("invalid\n"), review); err == nil {
		t.Fatal("ApplyNormativeEvidence accepted invalid inventory header")
	}
	badRow := []byte("id\tsource\tline\tlevel\trequirement\timplementation\tevidence\tstatus\ninvalid\n")
	if _, err := ApplyNormativeEvidence(badRow, review); err == nil {
		t.Fatal("ApplyNormativeEvidence accepted invalid inventory row")
	}
	extra := append(append([]byte(nil), review...), []byte("ORPC-1.4-0002\ta\tb\tcomplete\tnote\n")...)
	if _, err := ApplyNormativeEvidence(inventory, extra); err == nil {
		t.Fatal("ApplyNormativeEvidence accepted evidence for an unknown requirement")
	}
}
