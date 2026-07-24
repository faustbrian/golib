package jsonschema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func FuzzCompileAndValidate(f *testing.F) {
	for _, seed := range []struct {
		dialect  uint8
		schema   string
		instance string
	}{
		{0, `true`, `null`},
		{2, `{"type":"integer"}`, `1e100000`},
		{5, `{"pattern":"(?<=a)b"}`, `"ab"`},
		{5, `{"oneOf":[{"type":"null"},{"type":"array"}]}`, `[]`},
		{5, `{"$defs":{"node":{"$ref":"#/$defs/node"}},"$ref":"#/$defs/node"}`, `0`},
		{5, `{"allOf":[` + strings.Repeat(`{},`, 31) + `{}` + `]}`, `{}`},
		{5, strings.Repeat(`{"properties":{"x":`, 32) + `{}` + strings.Repeat(`}}`, 32), strings.Repeat(`{"x":`, 32) + `0` + strings.Repeat(`}`, 32)},
	} {
		f.Add(seed.dialect, seed.schema, seed.instance)
	}
	addOfficialFuzzSeeds(f)
	f.Fuzz(func(t *testing.T, dialectIndex uint8, schemaJSON string, instanceJSON string) {
		dialects := [...]jsonschema.Dialect{
			jsonschema.Draft3,
			jsonschema.Draft4,
			jsonschema.Draft6,
			jsonschema.Draft7,
			jsonschema.Draft201909,
			jsonschema.Draft202012,
		}
		compiler, err := jsonschema.NewCompiler(
			jsonschema.WithDialect(dialects[int(dialectIndex)%len(dialects)]),
		)
		if err != nil {
			t.Fatal(err)
		}
		schema, err := compiler.Compile(context.Background(), []byte(schemaJSON))
		if err != nil {
			return
		}
		_, _ = schema.Validate(context.Background(), []byte(instanceJSON))
	})
}

func addOfficialFuzzSeeds(f *testing.F) {
	f.Helper()
	for dialectIndex, directory := range []string{
		"draft3", "draft4", "draft6", "draft7", "draft2019-09", "draft2020-12",
	} {
		// #nosec G304 -- directory is selected from the fixed dialect list.
		raw, err := os.ReadFile(filepath.Join(
			"testdata", "official", "JSON-Schema-Test-Suite", "tests", directory, "type.json",
		))
		if err != nil {
			f.Fatal(err)
		}
		var groups []fixtureGroup
		if err := json.Unmarshal(raw, &groups); err != nil {
			f.Fatal(err)
		}
		for _, group := range groups {
			for _, test := range group.Tests {
				f.Add(uint8(dialectIndex), string(group.Schema), string(test.Data))
			}
		}
	}
}

func FuzzValidationOutput(f *testing.F) {
	f.Add(`{"properties":{"name":{"type":"string"}},"required":["name"]}`, `{"name":1}`)
	f.Add(`{"prefixItems":[{"type":"integer"}],"items":false}`, `[1,2]`)
	f.Fuzz(func(t *testing.T, schemaJSON string, instanceJSON string) {
		compiler, err := jsonschema.NewCompiler()
		if err != nil {
			t.Fatal(err)
		}
		schema, err := compiler.Compile(context.Background(), []byte(schemaJSON))
		if err != nil {
			return
		}
		for _, format := range []jsonschema.OutputFormat{
			jsonschema.OutputFlag,
			jsonschema.OutputBasic,
			jsonschema.OutputDetailed,
			jsonschema.OutputVerbose,
		} {
			output, err := schema.ValidateOutput(context.Background(), []byte(instanceJSON), format)
			if err == nil {
				_, _ = json.Marshal(output)
			}
		}
	})
}

func FuzzRawAndValueValidationAgree(f *testing.F) {
	for _, seed := range []string{
		`null`, `true`, `1e1000`, `"text"`, `[1,{"x":2}]`, `{"a":0.3}`,
	} {
		f.Add(seed)
	}
	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		f.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{}`))
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		// Validate rejects invalid UTF-8 while encoding/json deliberately
		// replaces it when constructing a caller-provided Go string.
		if !utf8.ValidString(raw) {
			return
		}
		decoder := json.NewDecoder(bytes.NewBufferString(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return
		}
		rawResult, rawErr := schema.Validate(context.Background(), []byte(raw))
		valueResult, valueErr := schema.ValidateValue(context.Background(), value)
		if (rawErr == nil) != (valueErr == nil) {
			t.Fatalf("raw error %v; value error %v", rawErr, valueErr)
		}
		if rawErr == nil && rawResult.Valid != valueResult.Valid {
			t.Fatalf("raw valid %t; value valid %t", rawResult.Valid, valueResult.Valid)
		}
	})
}

func FuzzReferenceResolution(f *testing.F) {
	f.Add("value", `"ok"`)
	f.Add("a~b/c", `0`)
	f.Fuzz(func(t *testing.T, property string, instance string) {
		schemaDocument, err := json.Marshal(map[string]any{
			"$defs": map[string]any{property: map[string]any{"const": json.Number("0")}},
			"$ref":  "#/$defs/" + escapeJSONPointer(property),
		})
		if err != nil {
			return
		}
		compiler, err := jsonschema.NewCompiler()
		if err != nil {
			t.Fatal(err)
		}
		schema, err := compiler.Compile(context.Background(), schemaDocument)
		if err != nil {
			return
		}
		_, _ = schema.Validate(context.Background(), []byte(instance))
	})
}

func escapeJSONPointer(value string) string {
	var result bytes.Buffer
	for _, character := range value {
		switch character {
		case '~':
			result.WriteString("~0")
		case '/':
			result.WriteString("~1")
		default:
			result.WriteRune(character)
		}
	}
	return result.String()
}
