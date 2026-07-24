package validate

import (
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestPortableOperationID(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		identifier string
		portable   bool
	}{
		{identifier: "readPet2", portable: true},
		{identifier: "_read_pet", portable: true},
		{identifier: "", portable: false},
		{identifier: "2readPet", portable: false},
		{identifier: "read-pet", portable: false},
		{identifier: "ÅluePet", portable: false},
	} {
		t.Run(test.identifier, func(t *testing.T) {
			t.Parallel()
			if got := portableOperationID(test.identifier); got != test.portable {
				t.Fatalf("portableOperationID(%q) = %t", test.identifier, got)
			}
		})
	}
}

func TestPathTemplateAndNameExactBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		path       string
		normalized string
		repeated   bool
		valid      bool
	}{
		{path: "/pets", normalized: "/pets", valid: true},
		{path: "/pets/{id}", normalized: "/pets/{}", valid: true},
		{path: "/{id}/{id}", normalized: "/{}/{}", repeated: true, valid: true},
		{path: "/{"},
		{path: "/}"},
		{path: "/{}"},
		{path: "/{a/b}"},
	} {
		names, normalized, repeated, err := parsePathTemplate(test.path)
		if (err == nil) != test.valid || normalized != test.normalized ||
			repeated != test.repeated {
			t.Errorf("parsePathTemplate(%q) = %#v, %q, %t, %v", test.path, names, normalized, repeated, err)
		}
	}
	for _, test := range []struct {
		name  string
		valid bool
	}{
		{name: "value", valid: true},
		{name: " ", valid: true},
		{name: string(rune(0x1f))},
		{name: string(rune(0x20)), valid: true},
		{name: string(rune(0x7e)), valid: true},
		{name: string(rune(0x7f))},
		{name: "{"},
		{name: "}"},
		{name: "/"},
		{name: string([]byte{0xff})},
	} {
		if got := validTemplateName(test.name); got != test.valid {
			t.Errorf("validTemplateName(%q) = %t", test.name, got)
		}
	}
}

func TestOperationsAtUsesExactDialectMethodSets(t *testing.T) {
	t.Parallel()

	pathItem := testValidationValue(t, `{
		"get":{},"trace":{},"query":{},
		"additionalOperations":{"SEARCH":{},"invalid":false}
	}`)
	for _, test := range []struct {
		dialect specversion.Dialect
		methods []string
	}{
		{dialect: specversion.DialectSwagger20, methods: []string{"get"}},
		{dialect: specversion.DialectOAS30, methods: []string{"get", "trace"}},
		{dialect: specversion.DialectOAS31, methods: []string{"get", "trace"}},
		{dialect: specversion.DialectOAS32, methods: []string{"get", "trace", "query", "SEARCH"}},
	} {
		operations := operationsAt(pathItem, "/path", test.dialect)
		if len(operations) != len(test.methods) {
			t.Errorf("dialect %q operations = %#v", test.dialect, operations)
			continue
		}
		for index, method := range test.methods {
			if operations[index].method != method ||
				operations[index].value.Kind() != jsonvalue.ObjectKind {
				t.Errorf("dialect %q operation %d = %#v", test.dialect, index, operations[index])
			}
		}
	}
}

func TestSafeValueTruncatesOnlyBeyondExactLimit(t *testing.T) {
	t.Parallel()

	exact := safeValue(strings.Repeat("a", 80))
	if strings.Contains(exact, "...") {
		t.Fatalf("exact safe value was truncated: %s", exact)
	}
	beyond := safeValue(strings.Repeat("a", 81))
	if !strings.Contains(beyond, "...") {
		t.Fatalf("long safe value was not truncated: %s", beyond)
	}
}
