package specification

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestExtractObjectFieldsHandlesFixedPatternedAndVariantTables(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(`# Specification

## Example Object

### Fixed Fields

| Field Name | Type | Description |
| --- | --- | --- |
| <a name="example-name"></a>name | ` + "`string`" + ` | **REQUIRED**. A name. |
| <a name="self-closing"/>allowEmptyValue | ` + "`boolean`" + ` | An optional flag. |
| value | Any | An optional value. |
| children | Map[` + "`string`" + `, [Child Object](#child-object)] | Child objects. |

### Patterned Fields

Field Pattern | Type | Description
---|:---:|---
^x- | Any | An extension.

### Fixed Fields for a variant

| Field Name | Type | Description |
| --- | --- | --- |
| value | ` + "`string`" + ` | A narrowed variant. |

## Examples

| Field Name | Type | Description |
| --- | --- | --- |
| ignored | string | This table is not an object. |
`)

	fields, err := ExtractObjectFields("3.2.0", "oas/3.2/3.2.0.md", input)
	if err != nil {
		t.Fatalf("ExtractObjectFields() error = %v", err)
	}
	if got, want := len(fields), 6; got != want {
		t.Fatalf("len(fields) = %d, want %d: %#v", got, want, fields)
	}

	checks := []struct {
		index    int
		name     string
		pattern  bool
		required bool
	}{
		{index: 0, name: "name", required: true},
		{index: 1, name: "allowEmptyValue"},
		{index: 2, name: "value"},
		{index: 3, name: "children"},
		{index: 4, name: "^x-", pattern: true},
		{index: 5, name: "value"},
	}
	for _, check := range checks {
		field := fields[check.index]
		if field.Object != "Example Object" || field.Name != check.name ||
			field.Pattern != check.pattern || field.Required != check.required {
			t.Errorf("field %d = %#v", check.index, field)
		}
	}
	if fields[3].Type != "Map[string, Child Object]" {
		t.Fatalf("nested mapped type = %q", fields[3].Type)
	}
	if fields[0].ID != "OAS-3.2.0-F0001" ||
		fields[5].ID != "OAS-3.2.0-F0006" ||
		fields[0].Line != 9 || fields[5].Line != 24 {
		t.Fatalf("field identity or lines = %#v, %#v", fields[0], fields[5])
	}
	if fields[2].Variant != "Fixed Fields" || fields[5].Variant != "Fixed Fields for a variant" {
		t.Fatalf("variants = %q, %q", fields[2].Variant, fields[5].Variant)
	}
}

func TestObjectInventoryValidatesInputsAndMarkdownEdges(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		version string
		source  string
		reader  io.Reader
	}{
		{source: "spec.md", reader: strings.NewReader("")},
		{version: "3.2.0", reader: strings.NewReader("")},
		{version: "3.2.0", source: "spec.md"},
		{version: "3.2.0", source: "spec.md",
			reader: strings.NewReader(strings.Repeat("x", maxSpecificationLineBytes+1))},
	} {
		if _, err := ExtractObjectFields(test.version, test.source, test.reader); err == nil {
			t.Fatalf("ExtractObjectFields(%q, %q) error = nil", test.version, test.source)
		}
	}
	if err := WriteObjectFieldsTSV(nil, nil); err == nil {
		t.Fatal("WriteObjectFieldsTSV accepted a nil writer")
	}
	failure := errors.New("write failed")
	if err := WriteObjectFieldsTSV(errorWriter{failure}, []ObjectField{{ID: "x"}}); !errors.Is(err, failure) {
		t.Fatalf("WriteObjectFieldsTSV error = %v", err)
	}
	edges, err := ExtractObjectFields("3.2.0", "spec.md", strings.NewReader(`
## Edge Object

| Field Name | Type | Description |
| --- | --- |
| skipped | string | invalid separator width |

| Field Name | Type | Description |
| --- | --- | --- |
| <a name="empty"></a> | string | empty normalized name |
`))
	if err != nil || len(edges) != 0 {
		t.Fatalf("edge table fields = %#v, error = %v", edges, err)
	}
	trailing, err := ExtractObjectFields(
		"3.2.0", "spec.md",
		strings.NewReader("## Trailing Object\nField Name | Type | Description"),
	)
	if err != nil || len(trailing) != 0 {
		t.Fatalf("trailing table fields = %#v, error = %v", trailing, err)
	}
	if _, _, ok := markdownHeadingWithLevel("#"); ok {
		t.Fatal("marker-only heading was accepted")
	}
	if got := splitMarkdownTableRow("plain text"); got != nil {
		t.Fatalf("plain row = %#v", got)
	}
	if got := splitMarkdownTableRow("|"); len(got) != 0 {
		t.Fatalf("empty delimited row = %#v", got)
	}
	row := splitMarkdownTableRow("| one\\|two | `three|four` | trailing\\")
	if len(row) != 3 || row[0] != "one|two" || row[1] != "`three|four`" ||
		row[2] != `trailing\` {
		t.Fatalf("escaped row = %#v", row)
	}
	if isTableSeparator([]string{"---", "invalid"}) ||
		!isTableSeparator([]string{" :---: ", "---"}) {
		t.Fatal("table separator classification is incorrect")
	}
	if defaultVariant("Named Fields", false) != "Named Fields" ||
		defaultVariant("", true) != "Patterned Fields" ||
		defaultVariant("", false) != "Fixed Fields" {
		t.Fatal("default field variants are incorrect")
	}
}

func TestWriteObjectFieldsTSVIsDeterministic(t *testing.T) {
	t.Parallel()

	fields := []ObjectField{{
		ID:       "OAS-3.2.0-F0001",
		Version:  "3.2.0",
		Source:   "oas/3.2/3.2.0.md",
		Line:     10,
		Object:   "Info Object",
		Variant:  "Fixed Fields",
		Name:     "title",
		Type:     "string",
		Required: true,
	}}

	var output strings.Builder
	if err := WriteObjectFieldsTSV(&output, fields); err != nil {
		t.Fatalf("WriteObjectFieldsTSV() error = %v", err)
	}
	want := "id\tversion\tsource\tline\tobject\tvariant\tname\ttype\tpattern\trequired\tdescription\n" +
		"OAS-3.2.0-F0001\t3.2.0\toas/3.2/3.2.0.md\t10\tInfo Object\tFixed Fields\ttitle\tstring\tfalse\ttrue\t\n"
	if got := output.String(); got != want {
		t.Fatalf("WriteObjectFieldsTSV() = %q, want %q", got, want)
	}
}
