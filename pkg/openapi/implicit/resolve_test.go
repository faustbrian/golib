package implicit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/implicit"
	"github.com/faustbrian/golib/pkg/openapi/parse"
)

func TestResolveComponentDefaultsToEntryDocument(t *testing.T) {
	t.Parallel()

	entry := document(t, "https://example.test/openapi.json", `{
		"components":{"securitySchemes":{"Shared":{"type":"apiKey"}}}
	}`)
	current := document(t, "https://example.test/paths.json", `{
		"components":{"securitySchemes":{"Shared":{"type":"http"}}}
	}`)

	match, err := implicit.ResolveComponent(
		entry, current, implicit.SecuritySchemes, "Shared",
		implicit.ComponentOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if match.DocumentURI != entry.URI ||
		match.Pointer != "/components/securitySchemes/Shared" {
		t.Fatalf("match = %#v", match)
	}
	typeValue, _ := match.Value.Lookup("type")
	typeName, _ := typeValue.Text()
	if typeName != "apiKey" {
		t.Fatalf("resolved type = %q", typeName)
	}
}

func TestResolveComponentCanExplicitlyUseCurrentDocument(t *testing.T) {
	t.Parallel()

	entry := document(t, "entry", `{"components":{"schemas":{"Pet":{}}}}`)
	current := document(t, "current", `{"components":{"schemas":{"Pet":{"title":"local"}}}}`)
	match, err := implicit.ResolveComponent(
		entry, current, implicit.Schemas, "Pet",
		implicit.ComponentOptions{Scope: implicit.CurrentDocument},
	)
	if err != nil || match.DocumentURI != "current" {
		t.Fatalf("match = %#v, error = %v", match, err)
	}
}

func TestResolveTagDefaultsToEntryDocument(t *testing.T) {
	t.Parallel()

	entry := document(t, "entry", `{"tags":[{"name":"pets","description":"entry"}]}`)
	current := document(t, "current", `{"tags":[{"name":"pets","description":"current"}]}`)
	match, err := implicit.ResolveTag(
		entry, current, "pets", implicit.NameOptions{},
	)
	if err != nil || match.DocumentURI != "entry" || match.Pointer != "/tags/0" {
		t.Fatalf("match = %#v, error = %v", match, err)
	}
}

func TestResolveOperationIDConsidersEveryParsedDocument(t *testing.T) {
	t.Parallel()

	documents := []implicit.Document{
		document(t, "entry", `{"paths":{"/pets":{"get":{"operationId":"listPets"}}}}`),
		document(t, "events", `{"webhooks":{"newPet":{"post":{"operationId":"receivePet"}}}}`),
	}
	match, err := implicit.ResolveOperationID(
		documents, "receivePet", implicit.OperationOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if match.DocumentURI != "events" ||
		match.Pointer != "/webhooks/newPet/post" {
		t.Fatalf("match = %#v", match)
	}
}

func TestResolveOperationIDReportsAmbiguousMatches(t *testing.T) {
	t.Parallel()

	documents := []implicit.Document{
		document(t, "one", `{"paths":{"/one":{"get":{"operationId":"shared"}}}}`),
		document(t, "two", `{"paths":{"/two":{"post":{"operationId":"shared"}}}}`),
	}
	_, err := implicit.ResolveOperationID(documents, "shared", implicit.OperationOptions{})
	if !errors.Is(err, implicit.ErrAmbiguous) {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveOperationIDTraversesCallbacksAndComponentCallbacks(t *testing.T) {
	t.Parallel()

	document := document(t, "entry", `{
		"paths":{"/~hooks":{"parameters":[],"post":{
			"operationId":"start",
			"callbacks":{"done":{"{$request.body#/url}":{
				"patch":{"operationId":"finish"}
			}}}
		}}},
		"components":{"callbacks":{
			"Retry":{"{$request.body#/retry}":{
				"put":{"operationId":"retry"}
			}},
			"External":{"$ref":"other.json#/callback"}
		},"pathItems":{"Status":{"get":{"operationId":"status"}}}}
	}`)
	for _, test := range []struct {
		id      string
		pointer string
	}{
		{id: "finish", pointer: "/paths/~1~0hooks/post/callbacks/done/{$request.body#~1url}/patch"},
		{id: "retry", pointer: "/components/callbacks/Retry/{$request.body#~1retry}/put"},
		{id: "status", pointer: "/components/pathItems/Status/get"},
	} {
		match, err := implicit.ResolveOperationID(
			[]implicit.Document{document}, test.id, implicit.OperationOptions{},
		)
		if err != nil || match.Pointer != test.pointer {
			t.Fatalf("%s match = %#v, error = %v", test.id, match, err)
		}
	}
}

func TestResolveTagAndComponentApplyExactBounds(t *testing.T) {
	t.Parallel()

	document := document(t, "entry", `{
		"tags":[{"name":"one"},{"name":"two"}],
		"components":{"schemas":{"One":{},"Two":{}}}
	}`)
	limits := implicit.Limits{MaxNames: 2}
	if _, err := implicit.ResolveTag(
		document, document, "two", implicit.NameOptions{Limits: limits},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := implicit.ResolveComponent(
		document, document, implicit.Schemas, "Two",
		implicit.ComponentOptions{Limits: limits},
	); err != nil {
		t.Fatal(err)
	}
}

func TestImplicitResolutionRejectsMalformedDocuments(t *testing.T) {
	t.Parallel()

	valid := document(t, "entry", `{}`)
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{name: "component root", run: func() error {
			_, err := implicit.ResolveComponent(document(t, "bad", `[]`), valid,
				implicit.Schemas, "Pet", implicit.ComponentOptions{})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "components object", run: func() error {
			_, err := implicit.ResolveComponent(document(t, "bad", `{"components":1}`), valid,
				implicit.Schemas, "Pet", implicit.ComponentOptions{})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "component section missing", run: func() error {
			_, err := implicit.ResolveComponent(document(t, "bad", `{"components":{}}`), valid,
				implicit.Schemas, "Pet", implicit.ComponentOptions{})
			return err
		}, want: implicit.ErrNotFound},
		{name: "component section object", run: func() error {
			_, err := implicit.ResolveComponent(document(t, "bad", `{"components":{"schemas":1}}`), valid,
				implicit.Schemas, "Pet", implicit.ComponentOptions{})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "component missing", run: func() error {
			_, err := implicit.ResolveComponent(document(t, "bad", `{"components":{"schemas":{}}}`), valid,
				implicit.Schemas, "Pet", implicit.ComponentOptions{})
			return err
		}, want: implicit.ErrNotFound},
		{name: "component limit", run: func() error {
			_, err := implicit.ResolveComponent(
				document(t, "bad", `{"components":{"schemas":{"One":{},"Two":{}}}}`), valid,
				implicit.Schemas, "Pet", implicit.ComponentOptions{
					Limits: implicit.Limits{MaxNames: 1},
				})
			return err
		}, want: implicit.ErrLimitExceeded},
		{name: "component invalid limits", run: func() error {
			_, err := implicit.ResolveComponent(valid, valid, implicit.Schemas, "Pet",
				implicit.ComponentOptions{Limits: implicit.Limits{MaxNames: -1}})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "tag scope", run: func() error {
			_, err := implicit.ResolveTag(valid, valid, "tag", implicit.NameOptions{Scope: 99})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "tag root", run: func() error {
			_, err := implicit.ResolveTag(document(t, "bad", `[]`), valid, "tag", implicit.NameOptions{})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "tags missing", run: func() error {
			_, err := implicit.ResolveTag(valid, valid, "tag", implicit.NameOptions{})
			return err
		}, want: implicit.ErrNotFound},
		{name: "tags array", run: func() error {
			_, err := implicit.ResolveTag(document(t, "bad", `{"tags":{}}`), valid, "tag", implicit.NameOptions{})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "tag object", run: func() error {
			_, err := implicit.ResolveTag(document(t, "bad", `{"tags":[1]}`), valid, "tag", implicit.NameOptions{})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "tag missing name", run: func() error {
			_, err := implicit.ResolveTag(document(t, "bad", `{"tags":[{}]}`), valid, "tag", implicit.NameOptions{})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "tag name string", run: func() error {
			_, err := implicit.ResolveTag(document(t, "bad", `{"tags":[{"name":1}]}`), valid, "tag", implicit.NameOptions{})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "tag missing", run: func() error {
			_, err := implicit.ResolveTag(document(t, "bad", `{"tags":[{"name":"other"}]}`), valid, "tag", implicit.NameOptions{})
			return err
		}, want: implicit.ErrNotFound},
		{name: "tag limit", run: func() error {
			_, err := implicit.ResolveTag(document(t, "bad", `{"tags":[{"name":"one"},{"name":"two"}]}`), valid,
				"tag", implicit.NameOptions{Limits: implicit.Limits{MaxNames: 1}})
			return err
		}, want: implicit.ErrLimitExceeded},
		{name: "tag invalid limits", run: func() error {
			_, err := implicit.ResolveTag(valid, valid, "tag",
				implicit.NameOptions{Limits: implicit.Limits{MaxNames: -1}})
			return err
		}, want: implicit.ErrInvalidInput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.run(); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestResolveOperationIDRejectsMalformedOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want error
	}{
		{name: "root", raw: `[]`, want: implicit.ErrInvalidInput},
		{name: "paths map", raw: `{"paths":[]}`, want: implicit.ErrInvalidInput},
		{name: "path item", raw: `{"paths":{"/pets":1}}`, want: implicit.ErrInvalidInput},
		{name: "operation object", raw: `{"paths":{"/pets":{"get":1}}}`, want: implicit.ErrInvalidInput},
		{name: "operation id", raw: `{"paths":{"/pets":{"get":{"operationId":1}}}}`, want: implicit.ErrInvalidInput},
		{name: "callbacks object", raw: `{"paths":{"/pets":{"get":{"callbacks":1}}}}`, want: implicit.ErrInvalidInput},
		{name: "callback object", raw: `{"paths":{"/pets":{"get":{"callbacks":{"done":1}}}}}`, want: implicit.ErrInvalidInput},
		{name: "components object", raw: `{"components":1}`, want: implicit.ErrInvalidInput},
		{name: "components without callbacks", raw: `{"components":{}}`, want: implicit.ErrNotFound},
		{name: "component callbacks map", raw: `{"components":{"callbacks":1}}`, want: implicit.ErrInvalidInput},
		{name: "component callback object", raw: `{"components":{"callbacks":{"Bad":1}}}`, want: implicit.ErrInvalidInput},
		{name: "component path items map", raw: `{"components":{"pathItems":1}}`, want: implicit.ErrInvalidInput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := implicit.ResolveOperationID(
				[]implicit.Document{document(t, "bad", test.raw)},
				"missing", implicit.OperationOptions{},
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestResolveOperationIDAppliesOperationAndDepthLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		raw    string
		limits implicit.Limits
	}{
		{
			name:   "operations",
			raw:    `{"paths":{"/one":{"get":{}},"/two":{"get":{}}}}`,
			limits: implicit.Limits{MaxOperations: 1},
		},
		{
			name:   "depth",
			raw:    `{"paths":{"/one":{"get":{"callbacks":{"done":{"url":{"get":{}}}}}}}}`,
			limits: implicit.Limits{MaxDepth: 1},
		},
	}
	for _, test := range tests {
		_, err := implicit.ResolveOperationID(
			[]implicit.Document{document(t, "entry", test.raw)},
			"missing", implicit.OperationOptions{Limits: test.limits},
		)
		if !errors.Is(err, implicit.ErrLimitExceeded) {
			t.Fatalf("%s error = %v", test.name, err)
		}
	}
}

func TestResolveOperationIDAcceptsEveryExactLimit(t *testing.T) {
	t.Parallel()

	documents := []implicit.Document{
		document(t, "one", `{"paths":{"/one":{"get":{"operationId":"one"}}}}`),
		document(t, "two", `{"paths":{"/two":{"get":{"operationId":"two"}}}}`),
	}
	match, err := implicit.ResolveOperationID(
		documents,
		"two",
		implicit.OperationOptions{Limits: implicit.Limits{
			MaxDocuments: 2, MaxNames: 2, MaxOperations: 2, MaxDepth: 1,
		}},
	)
	if err != nil || match.DocumentURI != "two" {
		t.Fatalf("match = %#v, error = %v", match, err)
	}
}

func TestResolveComponentAcceptsLastComponentKind(t *testing.T) {
	t.Parallel()

	document := document(t, "entry", `{
		"components":{"pathItems":{"Shared":{"get":{}}}}
	}`)
	match, err := implicit.ResolveComponent(
		document, document, implicit.PathItems, "Shared",
		implicit.ComponentOptions{},
	)
	if err != nil || match.Pointer != "/components/pathItems/Shared" {
		t.Fatalf("match = %#v, error = %v", match, err)
	}
}

func TestImplicitResolutionRejectsInvalidLimitAxes(t *testing.T) {
	t.Parallel()

	valid := document(t, "entry", `{}`)
	for _, limits := range []implicit.Limits{
		{MaxDocuments: -1},
		{MaxNames: -1},
		{MaxOperations: -1},
		{MaxDepth: -1},
	} {
		if _, err := implicit.ResolveOperationID(
			[]implicit.Document{valid}, "read", implicit.OperationOptions{Limits: limits},
		); !errors.Is(err, implicit.ErrInvalidInput) {
			t.Fatalf("limits %#v error = %v", limits, err)
		}
	}
}

func TestImplicitResolutionRejectsMalformedInputAndLimits(t *testing.T) {
	t.Parallel()

	valid := document(t, "entry", `{}`)
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{name: "invalid component kind", run: func() error {
			_, err := implicit.ResolveComponent(valid, valid, 99, "Pet", implicit.ComponentOptions{})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "component kind boundary", run: func() error {
			_, err := implicit.ResolveComponent(
				valid, valid, implicit.PathItems+1, "Pet", implicit.ComponentOptions{},
			)
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "invalid scope", run: func() error {
			_, err := implicit.ResolveComponent(valid, valid, implicit.Schemas, "Pet",
				implicit.ComponentOptions{Scope: 99})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "empty name", run: func() error {
			_, err := implicit.ResolveTag(valid, valid, "", implicit.NameOptions{})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "empty operation id", run: func() error {
			_, err := implicit.ResolveOperationID([]implicit.Document{valid}, "", implicit.OperationOptions{})
			return err
		}, want: implicit.ErrInvalidInput},
		{name: "no documents", run: func() error {
			_, err := implicit.ResolveOperationID(nil, "read", implicit.OperationOptions{})
			return err
		}, want: implicit.ErrNotFound},
		{name: "operation not found", run: func() error {
			_, err := implicit.ResolveOperationID([]implicit.Document{valid}, "read", implicit.OperationOptions{})
			return err
		}, want: implicit.ErrNotFound},
		{name: "not found", run: func() error {
			_, err := implicit.ResolveComponent(valid, valid, implicit.Schemas, "Pet",
				implicit.ComponentOptions{})
			return err
		}, want: implicit.ErrNotFound},
		{name: "document limit", run: func() error {
			_, err := implicit.ResolveOperationID([]implicit.Document{valid, valid}, "read",
				implicit.OperationOptions{Limits: implicit.Limits{MaxDocuments: 1, MaxNames: 1}})
			return err
		}, want: implicit.ErrLimitExceeded},
		{name: "name limit", run: func() error {
			document := document(t, "entry", `{"paths":{"/one":{"get":{"operationId":"one"}},"/two":{"get":{"operationId":"two"}}}}`)
			_, err := implicit.ResolveOperationID([]implicit.Document{document}, "missing",
				implicit.OperationOptions{Limits: implicit.Limits{MaxDocuments: 1, MaxNames: 1}})
			return err
		}, want: implicit.ErrLimitExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.run(); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func document(t *testing.T, uri, raw string) implicit.Document {
	t.Helper()
	value, err := parse.JSON(
		context.Background(), strings.NewReader(raw), parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return implicit.Document{URI: uri, Root: value}
}
