package wsdl_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	wsdlcompile "github.com/faustbrian/golib/pkg/wsdl/compile"
	xsdresolve "github.com/faustbrian/golib/pkg/xsd/resolve"
)

func TestExternalWSDL11InteroperabilityCorpus(t *testing.T) {
	t.Parallel()

	fixtures := []struct {
		name             string
		path             string
		uri              string
		compile          bool
		schemaResources  map[string]string
		schemaNamespaces map[string]schemaFixture
	}{
		{
			name:    "SoapUI",
			path:    "testdata/interoperability/soapui/geocoder.wsdl",
			uri:     "https://fixtures.example/soapui/geocoder.wsdl",
			compile: true,
			schemaNamespaces: map[string]schemaFixture{
				"http://schemas.xmlsoap.org/soap/encoding/": {
					uri:  "https://schemas.xmlsoap.org/soap/encoding/",
					path: "testdata/interoperability/official/soap-encoding.xsd",
				},
			},
		},
		{
			name:    "dotnet-svcutil",
			path:    "testdata/interoperability/dotnet/simple.wsdl",
			uri:     "https://fixtures.example/dotnet/simple.wsdl",
			compile: true,
		},
		{
			name:    "Apache-CXF",
			path:    "testdata/interoperability/java/customer-service.wsdl",
			uri:     "https://fixtures.example/java/customer-service.wsdl",
			compile: true,
		},
		{
			name: "DHL",
			path: "testdata/interoperability/carrier/dhl-bcs-3.3.2/" +
				"geschaeftskundenversand-api-3.3.2.wsdl",
			uri: "https://fixtures.example/carrier/dhl-bcs-3.3.2/" +
				"geschaeftskundenversand-api-3.3.2.wsdl",
			schemaResources: map[string]string{
				"geschaeftskundenversand-api-3.3.2-schema-bcs_base.xsd": "testdata/interoperability/carrier/dhl-bcs-3.3.2/" +
					"geschaeftskundenversand-api-3.3.2-schema-bcs_base.xsd",
				"geschaeftskundenversand-api-3.3.2-schema-cis_base.xsd": "testdata/interoperability/carrier/dhl-bcs-3.3.2/" +
					"geschaeftskundenversand-api-3.3.2-schema-cis_base.xsd",
			},
		},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			exerciseWSDL11Fixture(
				t,
				fixture.path,
				fixture.uri,
				fixture.compile,
				fixture.schemaResources,
				fixture.schemaNamespaces,
			)
		})
	}
}

func exerciseWSDL11Fixture(
	t *testing.T,
	path string,
	uri string,
	compile bool,
	schemaPaths map[string]string,
	schemaNamespaces map[string]schemaFixture,
) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	document, err := wsdl.Parse(context.Background(), content, wsdl.ParseOptions{SystemID: uri})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	definitions, ok := document.Definitions11()
	if !ok || len(definitions.PortTypes) == 0 || len(definitions.Bindings) == 0 ||
		len(definitions.Services) == 0 {
		t.Fatalf("Definitions11() lacks inspectable service components: %#v", definitions)
	}
	if err = wsdl.Validate(document, wsdl.ValidationOptions{}).Err(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{MaxBytes: 4 << 20})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(round trip) error = %v", err)
	}
	secondPayload, err := wsdl.Marshal(roundTrip, wsdl.MarshalOptions{MaxBytes: 4 << 20})
	if err != nil {
		t.Fatalf("Marshal(round trip) error = %v", err)
	}
	if !bytes.Equal(payload, secondPayload) {
		index := firstDifference(payload, secondPayload)
		start := max(index-80, 0)
		end := min(index+160, len(payload))
		secondEnd := min(index+160, len(secondPayload))
		t.Fatalf(
			"round-trip serialization differs at byte %d:\nfirst:  %q\nsecond: %q",
			index,
			payload[start:end],
			secondPayload[start:secondEnd],
		)
	}
	if !compile {
		return
	}

	baseURI, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", uri, err)
	}
	resources := make(map[string][]byte, len(schemaPaths))
	for name, schemaPath := range schemaPaths {
		schema, readErr := os.ReadFile(schemaPath)
		if readErr != nil {
			t.Fatalf("ReadFile(%q) error = %v", schemaPath, readErr)
		}
		resources[baseURI.ResolveReference(&url.URL{Path: name}).String()] = schema
	}
	resolver, err := xsdresolve.NewMemory(resources)
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}
	compiler, err := wsdlcompile.New(wsdlcompile.Options{SchemaResolver: fixtureSchemaResolver{
		fallback:    resolver,
		byNamespace: schemaNamespaces,
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{URI: uri, Content: content})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(set.Interfaces()) == 0 || len(set.Bindings()) == 0 || len(set.Services()) == 0 {
		t.Fatalf("compiled graph lacks service components: %#v", set.Documents())
	}
}

type schemaFixture struct {
	uri  string
	path string
}

type fixtureSchemaResolver struct {
	fallback    xsdresolve.Resolver
	byNamespace map[string]schemaFixture
}

func (r fixtureSchemaResolver) Resolve(
	ctx context.Context,
	request xsdresolve.Request,
) (xsdresolve.Resource, error) {
	if request.URI == "" {
		if fixture, ok := r.byNamespace[request.Namespace]; ok {
			content, err := os.ReadFile(fixture.path)
			if err != nil {
				return xsdresolve.Resource{}, err
			}
			return xsdresolve.Resource{URI: fixture.uri, Content: content}, nil
		}
	}
	if r.fallback == nil {
		return xsdresolve.Resource{}, fmt.Errorf(
			"%w: %s",
			xsdresolve.ErrNotFound,
			request.URI,
		)
	}
	return r.fallback.Resolve(ctx, request)
}

func firstDifference(left, right []byte) int {
	limit := min(len(left), len(right))
	for index := range limit {
		if left[index] != right[index] {
			return index
		}
	}
	return limit
}

func TestAcceptedW3CWSDL20FixturesParseCompileAndRoundTrip(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob("testdata/w3c/wsdl20/*.wsdl")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("W3C fixture corpus is empty")
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("ReadFile() error = %v", readErr)
			}
			document, parseErr := wsdl.Parse(context.Background(), content, wsdl.ParseOptions{
				SystemID: "https://dev.w3.org/" + filepath.Base(path),
			})
			if parseErr != nil {
				t.Fatalf("Parse() error = %v", parseErr)
			}
			payload, marshalErr := wsdl.Marshal(document, wsdl.MarshalOptions{MaxBytes: 1 << 20})
			if marshalErr != nil {
				t.Fatalf("Marshal() error = %v", marshalErr)
			}
			if _, parseErr = wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{}); parseErr != nil {
				t.Fatalf("Parse(round trip) error = %v", parseErr)
			}
			compiler, newErr := wsdlcompile.New(wsdlcompile.Options{})
			if newErr != nil {
				t.Fatalf("New() error = %v", newErr)
			}
			set, compileErr := compiler.Compile(context.Background(), wsdlcompile.Source{
				URI: "https://dev.w3.org/" + filepath.Base(path), Content: content,
			})
			if compileErr != nil {
				t.Fatalf("Compile() error = %v", compileErr)
			}
			assertWodenSummary(t, path, set)
		})
	}
}

func assertWodenSummary(t *testing.T, path string, set *wsdlcompile.Set) {
	t.Helper()
	file, err := os.Open("testdata/interoperability/woden/expected.tsv")
	if err != nil {
		t.Fatalf("Open(Woden summary) error = %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Errorf("Close(Woden summary) error = %v", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 6 || fields[0] != filepath.Base(path) {
			continue
		}
		interfaces := set.Interfaces()
		if len(interfaces) != 1 || interfaces[0].Name.Namespace != fields[1] ||
			interfaces[0].Name.Local != fields[2] || len(interfaces[0].Operations) != 1 {
			t.Fatalf("compiled interface graph differs from Woden: %#v", interfaces)
		}
		operation := interfaces[0].Operations[0]
		if operation.Name != fields[3] || operation.Pattern != fields[4] ||
			strings.Join(operation.Styles, ",") != fields[5] {
			t.Fatalf("compiled operation differs from Woden: %#v", operation)
		}
		return
	}
	if err = scanner.Err(); err != nil {
		t.Fatalf("Scan(Woden summary) error = %v", err)
	}
	t.Fatalf("Woden summary has no row for %s", filepath.Base(path))
}
