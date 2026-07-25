package jsonschema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

type annotationSuite struct {
	Cases []annotationCase `json:"suite"`
}

type annotationCase struct {
	Description     string                     `json:"description"`
	Compatibility   string                     `json:"compatibility"`
	Schema          json.RawMessage            `json:"schema"`
	ExternalSchemas map[string]json.RawMessage `json:"externalSchemas"`
	Tests           []annotationTest           `json:"tests"`
}

type annotationTest struct {
	Instance   json.RawMessage       `json:"instance"`
	Assertions []annotationAssertion `json:"assertions"`
}

type annotationAssertion struct {
	Location string                     `json:"location"`
	Keyword  string                     `json:"keyword"`
	Expected map[string]json.RawMessage `json:"expected"`
}

func TestOfficialAnnotationFixtures(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join(
		"testdata",
		"official",
		"JSON-Schema-Test-Suite",
		"annotations",
		"tests",
		"*.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	for _, dialect := range []struct {
		name    string
		version int
		value   jsonschema.Dialect
	}{
		{name: "draft3", version: 3, value: jsonschema.Draft3},
		{name: "draft4", version: 4, value: jsonschema.Draft4},
		{name: "draft6", version: 6, value: jsonschema.Draft6},
		{name: "draft7", version: 7, value: jsonschema.Draft7},
		{name: "draft2019-09", version: 2019, value: jsonschema.Draft201909},
		{name: "draft2020-12", version: 2020, value: jsonschema.Draft202012},
	} {
		dialect := dialect
		for _, path := range paths {
			path := path
			t.Run(dialect.name+"/"+filepath.Base(path), func(t *testing.T) {
				t.Parallel()
				runAnnotationFile(t, path, dialect.value, dialect.version)
			})
		}
	}
}

func runAnnotationFile(
	t *testing.T,
	path string,
	dialect jsonschema.Dialect,
	version int,
) {
	t.Helper()
	// #nosec G304 -- path is confined to the pinned fixture tree.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var suite annotationSuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range suite.Cases {
		if !supportsAnnotationCase(testCase.Compatibility, version) ||
			schemaDeclaresDifferentDialect(testCase.Schema, dialect) {
			continue
		}
		t.Run(testCase.Description, func(t *testing.T) {
			t.Parallel()
			loader := jsonschema.ResourceLoaderFunc(
				func(ctx context.Context, identifier string) ([]byte, error) {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					raw, exists := testCase.ExternalSchemas[identifier]
					if !exists {
						return nil, fmt.Errorf("unknown annotation resource %q", identifier)
					}
					return append([]byte(nil), raw...), nil
				},
			)
			compiler, err := jsonschema.NewCompiler(
				jsonschema.WithDialect(dialect),
				jsonschema.WithResourceLoader(loader),
			)
			if err != nil {
				t.Fatal(err)
			}
			schema, err := compiler.Compile(context.Background(), testCase.Schema)
			if err != nil {
				t.Fatal(err)
			}
			for _, test := range testCase.Tests {
				annotations, err := schema.CollectAnnotations(
					context.Background(),
					test.Instance,
				)
				if err != nil {
					t.Fatal(err)
				}
				for _, assertion := range test.Assertions {
					assertAnnotations(t, annotations, assertion)
				}
			}
		})
	}
}

func schemaDeclaresDifferentDialect(
	raw []byte,
	dialect jsonschema.Dialect,
) bool {
	var declaration struct {
		Schema string `json:"$schema"`
	}
	if json.Unmarshal(raw, &declaration) != nil || declaration.Schema == "" {
		return false
	}
	return declaration.Schema != string(dialect)
}

func assertAnnotations(
	t *testing.T,
	annotations []jsonschema.OutputUnit,
	assertion annotationAssertion,
) {
	t.Helper()
	actual := make(map[string]json.RawMessage)
	for _, annotation := range flattenOutputAnnotations(annotations) {
		if annotation.InstanceLocation != assertion.Location ||
			!strings.HasSuffix(annotation.KeywordLocation, "/"+assertion.Keyword) {
			continue
		}
		encoded, err := json.Marshal(annotation.Annotation)
		if err != nil {
			t.Fatal(err)
		}
		actual[annotationSource(annotation)] = encoded
	}
	if len(actual) != len(assertion.Expected) {
		t.Fatalf(
			"%s at %s: got %d annotations %v, want %d %v",
			assertion.Keyword,
			assertion.Location,
			len(actual),
			actual,
			len(assertion.Expected),
			assertion.Expected,
		)
	}
	for source, expected := range assertion.Expected {
		observed := actual[source]
		if observed == nil && len(actual) == 1 && len(assertion.Expected) == 1 {
			for _, value := range actual {
				observed = value
			}
		}
		if !bytes.Equal(compactJSON(t, observed), compactJSON(t, expected)) {
			t.Errorf(
				"%s at %s from %s: got %s, want %s",
				assertion.Keyword,
				assertion.Location,
				source,
				actual[source],
				expected,
			)
		}
	}
}

func flattenOutputAnnotations(units []jsonschema.OutputUnit) []jsonschema.OutputUnit {
	result := make([]jsonschema.OutputUnit, 0)
	for _, unit := range units {
		if unit.Annotation != nil {
			result = append(result, unit)
		}
		result = append(result, flattenOutputAnnotations(unit.Annotations)...)
		result = append(result, flattenOutputAnnotations(unit.Errors)...)
	}
	return result
}

func annotationSource(annotation jsonschema.OutputUnit) string {
	keywordLocation := annotation.KeywordLocation
	if annotation.AbsoluteKeywordLocation != "" {
		absolute, err := url.Parse(annotation.AbsoluteKeywordLocation)
		if err == nil && absolute.Fragment != "" {
			keywordLocation = absolute.Fragment
		}
	}
	separator := strings.LastIndex(keywordLocation, "/")
	location := keywordLocation[:separator]
	if location == "" {
		return "#"
	}
	return "#" + (&url.URL{Fragment: location}).EscapedFragment()
}

func compactJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var result bytes.Buffer
	if err := json.Compact(&result, raw); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func supportsAnnotationCase(compatibility string, version int) bool {
	if compatibility == "" {
		return true
	}
	for _, constraint := range strings.Split(compatibility, ",") {
		constraint = strings.TrimSpace(constraint)
		switch {
		case strings.HasPrefix(constraint, "="):
			expected, err := strconv.Atoi(strings.TrimPrefix(constraint, "="))
			if err != nil || version != expected {
				return false
			}
		case strings.HasPrefix(constraint, "<="):
			maximum, err := strconv.Atoi(strings.TrimPrefix(constraint, "<="))
			if err != nil || version > maximum {
				return false
			}
		default:
			minimum, err := strconv.Atoi(constraint)
			if err != nil || version < minimum {
				return false
			}
		}
	}
	return true
}
