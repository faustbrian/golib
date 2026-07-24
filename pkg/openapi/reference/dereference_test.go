package reference_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestDereferenceObjectsInlinesArbitraryResponseTargets(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/openapi.json",
		Root: bundleValue(t, `{
			"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{"/pets":{"get":{"responses":{"200":{
				"$ref":"responses.json#/Result"}}}}}
		}`),
	}
	calls := 0
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		calls++
		return reference.Resource{
			RetrievalURI: identifier,
			Root: bundleValue(t, `{"Result":{"description":"OK","content":{
				"application/json":{"schema":{"$ref":"schemas.json#/Pet"}}
			}}}`),
		}, nil
	})
	result, err := reference.DereferenceObjects(
		context.Background(), base, resolver, reference.DefaultDereferenceOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	response := bundleMemberAt(
		t, result.Document().Raw(), "paths", "/pets", "get", "responses", "200",
	)
	if description, _ := bundleMemberAt(t, response, "description").Text(); description != "OK" {
		t.Fatalf("response = %#v", response)
	}
	schemaReference, _ := bundleMemberAt(
		t, response, "content", "application/json", "schema", "$ref",
	).Text()
	if schemaReference != "schemas.json#/Pet" {
		t.Fatalf("schema reference = %q", schemaReference)
	}
	if calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls)
	}
	entries := result.Entries()
	if len(entries) != 1 || entries[0].SourcePointer() !=
		"/paths/~1pets/get/responses/200/$ref" ||
		entries[0].TargetPointer() != "/Result" ||
		entries[0].SourceResource() != base.RetrievalURI ||
		entries[0].TargetResource() !=
			"https://api.example.test/responses.json" ||
		entries[0].RawReference() != "responses.json#/Result" {
		t.Fatalf("entries = %#v", entries)
	}
	entries[0] = reference.DereferenceEntry{}
	if len(result.Entries()) != 1 {
		t.Fatal("result exposed mutable entries")
	}
}

func TestDereferenceObjectsCachesResourcesAndBoundsWork(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/openapi.json",
		Root: bundleValue(t, `{
			"openapi":"3.0.4","info":{"title":"API","version":"1"},
			"paths":{"/pets":{"get":{"responses":{
				"200":{"$ref":"responses.json#/Result"},
				"201":{"$ref":"responses.json#/Result"}
			}}}}
		}`),
	}
	calls := 0
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		calls++
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         bundleValue(t, `{"Result":{"description":"OK"}}`),
		}, nil
	})
	result, err := reference.DereferenceObjects(
		context.Background(), base, resolver, reference.DefaultDereferenceOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(result.Entries()) != 2 {
		t.Fatalf("calls = %d, entries = %d", calls, len(result.Entries()))
	}

	for name, mutate := range map[string]func(*reference.DereferenceOptions){
		"references": func(options *reference.DereferenceOptions) {
			options.MaxReferences = 1
		},
		"nodes": func(options *reference.DereferenceOptions) {
			options.MaxNodes = 1
		},
		"depth": func(options *reference.DereferenceOptions) {
			options.MaxDepth = 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			options := reference.DefaultDereferenceOptions()
			mutate(&options)
			if _, err := reference.DereferenceObjects(
				context.Background(), base, resolver, options,
			); !errors.Is(err, reference.ErrLimitExceeded) {
				t.Fatalf("limit error = %v", err)
			}
		})
	}
}

func TestDereferenceObjectsAcceptsExactReferenceLimit(t *testing.T) {
	t.Parallel()

	base := reference.Resource{Root: bundleValue(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{},
		"components":{"responses":{
			"Target":{"description":"OK"},
			"Alias":{"$ref":"#/components/responses/Target"}
		}}
	}`)}
	options := reference.DefaultDereferenceOptions()
	options.MaxReferences = 1
	result, err := reference.DereferenceObjects(
		context.Background(), base, nil, options,
	)
	if err != nil || len(result.Entries()) != 1 {
		t.Fatalf("exact reference limit = %#v, %v", result.Entries(), err)
	}
}

func TestDereferenceObjectsRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	base := reference.Resource{Root: bundleValue(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{"200":{
			"$ref":"other.json#/Result"}}}}}
	}`)}
	if _, err := reference.DereferenceObjects(
		context.Background(), base, nil, reference.DefaultDereferenceOptions(),
	); !errors.Is(err, reference.ErrExternalResolutionDisabled) {
		t.Fatalf("external error = %v", err)
	}
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := reference.DereferenceObjects(
		//lint:ignore SA1012 This assertion verifies the nil-context contract.
		nil, base, nil, reference.DefaultDereferenceOptions(),
	); err == nil {
		t.Fatal("nil context succeeded")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reference.DereferenceObjects(
		canceled, base, nil, reference.DefaultDereferenceOptions(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	if _, err := reference.DereferenceObjects(
		context.Background(), base, nil, reference.DereferenceOptions{},
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("option error = %v", err)
	}
	if _, err := reference.DereferenceObjects(
		context.Background(),
		reference.Resource{Root: jsonvalue.Null()},
		nil,
		reference.DefaultDereferenceOptions(),
	); err == nil {
		t.Fatal("non-document resource was accepted")
	}

	for name, testCase := range map[string]struct {
		raw      string
		expected error
	}{
		"invalid reference": {
			`{"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{},"components":{"responses":{"Alias":{"$ref":false}}}}`,
			reference.ErrInvalidReference,
		},
		"scalar target": {
			`{"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{},"components":{"responses":{
			"Target":false,"Alias":{"$ref":"#/components/responses/Target"}}}}`,
			reference.ErrDereferenceTarget,
		},
		"incompatible target": {
			`{"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{},"components":{
			"parameters":{"Wrong":{"name":"id","in":"query","schema":{}}},
			"responses":{"Alias":{"$ref":"#/components/parameters/Wrong"}}}}`,
			reference.ErrDereferenceTarget,
		},
		"malformed overlay": {
			`{"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{},"components":{"responses":{
			"Target":{"description":"OK"},
			"Alias":{"$ref":"#/components/responses/Target","description":false}}}}`,
			reference.ErrDereferenceSibling,
		},
	} {
		t.Run(name, func(t *testing.T) {
			resource := reference.Resource{Root: bundleValue(t, testCase.raw)}
			if _, err := reference.DereferenceObjects(
				context.Background(), resource, nil,
				reference.DefaultDereferenceOptions(),
			); !errors.Is(err, testCase.expected) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func bundleMemberAt(
	t *testing.T,
	value jsonvalue.Value,
	names ...string,
) jsonvalue.Value {
	t.Helper()
	for _, name := range names {
		var exists bool
		value, exists = value.Lookup(name)
		if !exists {
			t.Fatalf("missing member %q", name)
		}
	}
	return value
}

func TestDereferenceObjectsAppliesReferenceSiblingsAndRejectsCycles(t *testing.T) {
	t.Parallel()

	sibling := reference.Resource{Root: bundleValue(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{},
		"components":{"responses":{
			"Target":{"description":"target"},
			"Alias":{"$ref":"#/components/responses/Target",
				"summary":"ignored","description":"override","x-extra":true}
		},"examples":{
			"Target":{"value":{"ok":true}},
			"Alias":{"$ref":"#/components/examples/Target",
				"summary":"Example","description":"Details"}
		}}
	}`)}
	result, err := reference.DereferenceObjects(
		context.Background(), sibling, nil, reference.DefaultDereferenceOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	alias := bundleMemberAt(
		t, result.Document().Raw(), "components", "responses", "Alias",
	)
	description, _ := bundleMemberAt(t, alias, "description").Text()
	if description != "override" {
		t.Fatalf("description = %q", description)
	}
	if _, exists := alias.Lookup("summary"); exists {
		t.Fatalf("unsupported summary was retained: %#v", alias)
	}
	if _, exists := alias.Lookup("x-extra"); exists {
		t.Fatalf("prohibited extension was retained: %#v", alias)
	}
	example := bundleMemberAt(
		t, result.Document().Raw(), "components", "examples", "Alias",
	)
	for name, want := range map[string]string{
		"summary": "Example", "description": "Details",
	} {
		got, _ := bundleMemberAt(t, example, name).Text()
		if got != want {
			t.Fatalf("%s = %q", name, got)
		}
	}
	cycle := reference.Resource{Root: bundleValue(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{},
		"components":{"responses":{
			"One":{"$ref":"#/components/responses/Two"},
			"Two":{"$ref":"#/components/responses/One"}
		}}
	}`)}
	if _, err := reference.DereferenceObjects(
		context.Background(), cycle, nil, reference.DefaultDereferenceOptions(),
	); !errors.Is(err, reference.ErrDereferenceCycle) {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestDereferenceObjectsAppliesReferenceOverlaysByTargetType(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.1.0", "3.1.1", "3.1.2", "3.2.0"} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			resource := reference.Resource{Root: bundleValue(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/pets":{"$ref":"#/components/pathItems/Alias"}},
				"components":{
					"pathItems":{
						"Target":{"summary":"target summary",
							"description":"target description","get":{}},
						"Alias":{"$ref":"#/components/pathItems/Target",
							"summary":"alias summary",
							"description":"alias description","x-ignored":true}
					},
					"responses":{
						"Target":{"description":"target response"},
						"Alias":{"$ref":"#/components/responses/Target",
							"summary":"alias response"}
					}
				}
			}`)}
			result, err := reference.DereferenceObjects(
				context.Background(), resource, nil,
				reference.DefaultDereferenceOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			pathItem := bundleMemberAt(
				t, result.Document().Raw(), "components", "pathItems", "Alias",
			)
			for name, want := range map[string]string{
				"summary": "alias summary", "description": "alias description",
			} {
				got, _ := bundleMemberAt(t, pathItem, name).Text()
				if got != want {
					t.Fatalf("Path Item %s = %q, want %q", name, got, want)
				}
			}
			if _, exists := pathItem.Lookup("get"); !exists {
				t.Fatalf("Path Item target fields were lost: %#v", pathItem)
			}
			if _, exists := pathItem.Lookup("x-ignored"); exists {
				t.Fatalf("prohibited Reference Object sibling retained: %#v", pathItem)
			}
			usedPathItem := bundleMemberAt(
				t, result.Document().Raw(), "paths", "/pets",
			)
			usedSummary, _ := bundleMemberAt(t, usedPathItem, "summary").Text()
			if usedSummary != "alias summary" {
				t.Fatalf("used Path Item summary = %q", usedSummary)
			}
			response := bundleMemberAt(
				t, result.Document().Raw(), "components", "responses", "Alias",
			)
			summary, hasSummary := response.Lookup("summary")
			if version == "3.2.0" {
				got, _ := summary.Text()
				if !hasSummary || got != "alias response" {
					t.Fatalf("Response summary = %q, %t", got, hasSummary)
				}
			} else if hasSummary {
				t.Fatalf("unsupported Response summary retained: %#v", response)
			}
		})
	}
}

func TestDereferenceObjectsIgnoresReferenceExtensionsAcrossVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			resource := reference.Resource{Root: bundleValue(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{"responses":{
					"Target":{"description":"target"},
					"Alias":{"$ref":"#/components/responses/Target",
						"unexpected":true,"x-extra":true}
				}}
			}`)}
			result, err := reference.DereferenceObjects(
				context.Background(), resource, nil,
				reference.DefaultDereferenceOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			alias := bundleMemberAt(
				t, result.Document().Raw(), "components", "responses", "Alias",
			)
			for _, name := range []string{"unexpected", "x-extra"} {
				if _, exists := alias.Lookup(name); exists {
					t.Fatalf("reference sibling %q was retained: %#v", name, alias)
				}
			}
		})
	}
}

func TestDereferenceObjectsUsesOpenAPI32SelfAsBase(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://retrieval.example.test/openapi.json",
		Root: bundleValue(t, `{
			"openapi":"3.2.0","$self":"https://canonical.example.test/api/root.json",
			"paths":{"/pets":{"get":{"responses":{"200":{
				"$ref":"responses.json#/Result"}}}}}
		}`),
	}
	var requested string
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		requested = identifier
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         bundleValue(t, `{"Result":{"description":"OK"}}`),
		}, nil
	})
	if _, err := reference.DereferenceObjects(
		context.Background(), base, resolver,
		reference.DefaultDereferenceOptions(),
	); err != nil {
		t.Fatal(err)
	}
	if requested != "https://canonical.example.test/api/responses.json" {
		t.Fatalf("requested = %q", requested)
	}
}

func TestDereferenceObjectsUsesExternalOpenAPI32SelfAsBase(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root: bundleValue(t, `{
			"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{"/pets":{"get":{"responses":{"200":{
				"$ref":"documents.json#/components/responses/Alias"}}}}}
		}`),
	}
	var requests []string
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		requests = append(requests, identifier)
		if len(requests) == 1 {
			return reference.Resource{
				RetrievalURI: identifier,
				Root: bundleValue(t, `{
					"openapi":"3.2.0",
					"$self":"https://canonical.example.test/spec/root.json",
					"components":{"responses":{"Alias":{
						"$ref":"responses.json#/Result"}}}
				}`),
			}, nil
		}
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         bundleValue(t, `{"Result":{"description":"OK"}}`),
		}, nil
	})
	if _, err := reference.DereferenceObjects(
		context.Background(), base, resolver,
		reference.DefaultDereferenceOptions(),
	); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://api.example.test/documents.json",
		"https://canonical.example.test/spec/responses.json",
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %#v", requests)
	}
	for index := range want {
		if requests[index] != want[index] {
			t.Fatalf("requests = %#v", requests)
		}
	}
}

func TestDereferenceObjectsHandlesVersionSpecificReferencePositions(t *testing.T) {
	t.Parallel()

	t.Run("OpenAPI 3.2 media type", func(t *testing.T) {
		base := reference.Resource{Root: bundleValue(t, `{
			"openapi":"3.2.0","tags":[{"name":"pets"}],
			"paths":{"/pets":{"get":{"responses":{"200":{
				"description":"OK","content":{"application/json":{
					"$ref":"#/components/mediaTypes/Shared"}}
			}}}}},
			"components":{"mediaTypes":{"Shared":{
				"schema":{"type":"string"}}}}
		}`)}
		result, err := reference.DereferenceObjects(
			context.Background(), base, nil,
			reference.DefaultDereferenceOptions(),
		)
		if err != nil {
			t.Fatal(err)
		}
		mediaType := bundleMemberAt(
			t, result.Document().Raw(), "paths", "/pets", "get",
			"responses", "200", "content", "application/json",
		)
		if _, exists := mediaType.Lookup("schema"); !exists {
			t.Fatalf("media type = %#v", mediaType)
		}
	})

	t.Run("OpenAPI callback Path Item", func(t *testing.T) {
		base := reference.Resource{Root: bundleValue(t, `{
			"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{"/subscribe":{"post":{
				"callbacks":{"event":{"{$request.body#/url}":{
					"$ref":"#/components/pathItems/Event"}}},
				"responses":{"204":{"description":"done"}}
			}}},
			"components":{"pathItems":{"Event":{"post":{
				"responses":{"200":{"description":"received"}}}}}}
		}`)}
		result, err := reference.DereferenceObjects(
			context.Background(), base, nil,
			reference.DefaultDereferenceOptions(),
		)
		if err != nil {
			t.Fatal(err)
		}
		pathItem := bundleMemberAt(
			t, result.Document().Raw(), "paths", "/subscribe", "post",
			"callbacks", "event", "{$request.body#/url}",
		)
		if _, exists := pathItem.Lookup("post"); !exists {
			t.Fatalf("callback Path Item = %#v", pathItem)
		}
	})

	t.Run("Swagger ignored sibling", func(t *testing.T) {
		base := reference.Resource{Root: bundleValue(t, `{
			"swagger":"2.0","info":{"title":"API","version":"1"},
			"paths":{},"responses":{
				"Target":{"description":"target"},
				"Alias":{"$ref":"#/responses/Target","description":"ignored"}
			}
		}`)}
		result, err := reference.DereferenceObjects(
			context.Background(), base, nil,
			reference.DefaultDereferenceOptions(),
		)
		if err != nil {
			t.Fatal(err)
		}
		description, _ := bundleMemberAt(
			t, result.Document().Raw(), "responses", "Alias", "description",
		).Text()
		if description != "target" {
			t.Fatalf("description = %q", description)
		}
	})

	t.Run("OpenAPI 3.0 Path Item sibling", func(t *testing.T) {
		base := reference.Resource{Root: bundleValue(t, `{
			"openapi":"3.0.4","info":{"title":"API","version":"1"},
			"paths":{"/pets":{"$ref":"#/x-path-item",
				"summary":"ambiguous"}},
			"x-path-item":{}
		}`)}
		if _, err := reference.DereferenceObjects(
			context.Background(), base, nil,
			reference.DefaultDereferenceOptions(),
		); !errors.Is(err, reference.ErrDereferenceSibling) {
			t.Fatalf("Path Item sibling error = %v", err)
		}
	})
}

func TestDereferenceObjectsRecordsAnchorTargets(t *testing.T) {
	t.Parallel()

	base := reference.Resource{Root: bundleValue(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{},"components":{"responses":{
			"Target":{"$anchor":"result","description":"OK"},
			"Alias":{"$ref":"#result"}
		}}
	}`)}
	result, err := reference.DereferenceObjects(
		context.Background(), base, nil,
		reference.DefaultDereferenceOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	entries := result.Entries()
	if len(entries) != 1 || entries[0].TargetPointer() != "#result" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestDereferenceObjectsObservesCancellationDuringResolution(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root: bundleValue(t, `{
			"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{"/pets":{"get":{"responses":{"200":{
				"$ref":"responses.json#/Result"}}}}}
		}`),
	}
	ctx, cancel := context.WithCancel(context.Background())
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		cancel()
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         bundleValue(t, `{"Result":{"description":"OK"}}`),
		}, nil
	})
	if _, err := reference.DereferenceObjects(
		ctx, base, resolver, reference.DefaultDereferenceOptions(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}
