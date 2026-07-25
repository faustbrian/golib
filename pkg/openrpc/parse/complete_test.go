package parse_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	"github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

func TestCompleteObjectFieldRoundTrip(t *testing.T) {
	t.Parallel()

	input, err := os.ReadFile("testdata/complete-openrpc.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parse.Decode(input, parse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsonvalue.Parse(input, jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if report := validate.MetaSchema(context.Background(), raw, 1_000); !report.Valid() {
		t.Fatalf("meta-schema issues = %#v, error = %v", report.Issues(), report.Err())
	}
	if report := validate.Document(context.Background(), parsed.Document(), validate.DefaultOptions()); !report.Valid() {
		t.Fatalf("semantic diagnostics = %#v", report.Diagnostics())
	}
	canonical, err := openrpc.MarshalCanonical(parsed.Document())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parse.Decode(canonical, parse.DefaultOptions()); err != nil {
		t.Fatalf("reparse canonical document: %v", err)
	}
	if !reflect.DeepEqual(decodeJSON(t, input), decodeJSON(t, canonical)) {
		t.Fatalf("round trip changed semantics:\n%s", canonical)
	}
	if bytes.Equal(input, canonical) {
		t.Fatal("fixture unexpectedly used canonical formatting")
	}
}

func TestObjectFieldRequirednessAndNullabilityMatrix(t *testing.T) {
	t.Parallel()

	input, err := os.ReadFile("testdata/complete-openrpc.json")
	if err != nil {
		t.Fatal(err)
	}
	matrix, err := os.Open("../specification/conformance/object-fields.tsv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = matrix.Close() })
	reader := csv.NewReader(matrix)
	reader.Comma = '\t'
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows[1:] {
		objectPath, found := completeObjectPaths[row[0]]
		if !found {
			t.Fatalf("no complete fixture object for %s", row[0])
		}
		required, err := strconv.ParseBool(row[3])
		if err != nil {
			t.Fatal(err)
		}
		nullable, err := strconv.ParseBool(row[4])
		if err != nil {
			t.Fatal(err)
		}
		name := row[0] + "/" + row[1]
		t.Run(name+"/presence", func(t *testing.T) {
			document := decodeJSON(t, input)
			object := objectAt(t, document, objectPath)
			delete(object, row[1])
			valid := structurallyValid(t, document)
			if valid == required {
				t.Fatalf("valid after removal = %t, required = %t", valid, required)
			}
		})
		t.Run(name+"/null", func(t *testing.T) {
			document := decodeJSON(t, input)
			objectAt(t, document, objectPath)[row[1]] = nil
			if valid := structurallyValid(t, document); valid != nullable {
				t.Fatalf("valid with null = %t, nullable = %t", valid, nullable)
			}
		})
		t.Run(name+"/wrong-type", func(t *testing.T) {
			document := decodeJSON(t, input)
			object := objectAt(t, document, objectPath)
			object[row[1]] = incompatibleValue(object[row[1]])
			encoded, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = parse.Decode(encoded, parse.DefaultOptions())
		})
		t.Run(name+"/unknown-field", func(t *testing.T) {
			document := decodeJSON(t, input)
			objectAt(t, document, objectPath)["futureField"] = true
			encoded, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = parse.Decode(encoded, parse.DefaultOptions())
		})
	}
}

func incompatibleValue(value any) any {
	switch value.(type) {
	case map[string]any:
		return []any{}
	case []any:
		return false
	case string:
		return false
	case bool:
		return []any{}
	default:
		return false
	}
}

var completeObjectPaths = map[string][]any{
	"#":                                         {},
	"#/definitions/contactObject":               {"info", "contact"},
	"#/definitions/contentDescriptorObject":     {"methods", 0, "params", 0},
	"#/definitions/errorObject":                 {"methods", 0, "errors", 0},
	"#/definitions/exampleObject":               {"methods", 0, "examples", 0, "params", 0},
	"#/definitions/examplePairingObject":        {"methods", 0, "examples", 0},
	"#/definitions/externalDocumentationObject": {"externalDocs"},
	"#/definitions/infoObject":                  {"info"},
	"#/definitions/licenseObject":               {"info", "license"},
	"#/definitions/linkObject":                  {"methods", 0, "links", 0},
	"#/definitions/methodObject":                {"methods", 0},
	"#/definitions/referenceObject":             {"components", "examplePairings", "Read", "params", 0},
	"#/definitions/serverObject":                {"servers", 0},
	"#/definitions/serverObject/properties/variables/patternProperties/[0-z]+": {"servers", 0, "variables", "region"},
	"#/definitions/tagObject": {"methods", 0, "tags", 0},
	"#/properties/components": {"components"},
}

func objectAt(t *testing.T, value any, path []any) map[string]any {
	t.Helper()
	current := value
	for _, segment := range path {
		switch typed := segment.(type) {
		case string:
			current = current.(map[string]any)[typed]
		case int:
			current = current.([]any)[typed]
		default:
			t.Fatalf("unsupported fixture path segment %#v", segment)
		}
	}
	object, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("fixture path %#v is %T", path, current)
	}
	return object
}

func structurallyValid(t *testing.T, document any) bool {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonvalue.Parse(encoded, jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	metaSchemaValid := validate.MetaSchema(context.Background(), value, 100).Valid()
	_, err = parse.Decode(encoded, parse.DefaultOptions())
	return metaSchemaValid && err == nil
}

func decodeJSON(t *testing.T, input []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
