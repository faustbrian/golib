package reference_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestBundleComponentsLocalizesExternalGraphsAndCycles(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/openapi.json",
		Root: bundleValue(t, `{
			"openapi":"3.2.0",
			"paths":{"/pets":{"get":{"responses":{"200":{
				"description":"ok","content":{"application/json":{"schema":{
					"$ref":"models.json#/components/schemas/Pet"
				}}}
			}}}}},
			"components":{"schemas":{
				"Pet":{"type":"string"},
				"Local":{"$ref":"#/components/schemas/Pet"}
			}}
		}`),
	}
	resolverCalls := 0
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		resolverCalls++
		if identifier != "https://api.example.test/models.json" {
			t.Fatalf("resolver identifier = %q", identifier)
		}
		return reference.Resource{
			RetrievalURI: identifier,
			Root: bundleValue(t, `{
				"components":{"schemas":{
					"Pet":{"type":"object","properties":{"owner":{
						"$ref":"#/components/schemas/Owner"
					}}},
					"Owner":{"type":"object","properties":{"pet":{
						"$ref":"#/components/schemas/Pet"
					}}}
				}}
			}`),
		}, nil
	})

	result, err := reference.BundleComponents(
		context.Background(), base, resolver, reference.DefaultBundleOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolverCalls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolverCalls)
	}
	encoded, _ := json.Marshal(result.Document().Raw())
	for _, fragment := range []string{
		`"$ref":"#/components/schemas/Pet_bundled"`,
		`"Pet_bundled":{"type":"object"`,
		`"$ref":"#/components/schemas/Owner"`,
		`"Owner":{"type":"object"`,
		`"Local":{"$ref":"#/components/schemas/Pet"}`,
	} {
		if !strings.Contains(string(encoded), fragment) {
			t.Fatalf("bundled document = %s, want %s", encoded, fragment)
		}
	}
	entries := result.Entries()
	if len(entries) != 3 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].RawReference() != "models.json#/components/schemas/Pet" ||
		entries[0].LocalReference() != "#/components/schemas/Pet_bundled" ||
		entries[0].SourceResource() == "" || entries[0].SourcePointer() == "" ||
		entries[0].TargetResource() == "" || entries[0].TargetPointer() == "" {
		t.Fatalf("first entry = %#v", entries[0])
	}
	entries[0] = reference.BundleEntry{}
	if result.Entries()[0].LocalReference() == "" {
		t.Fatal("result exposed mutable entry storage")
	}
	original, _ := json.Marshal(base.Root)
	if strings.Contains(string(original), "Pet_bundled") {
		t.Fatal("bundle mutated the base resource")
	}
}

func TestBundleComponentsCreatesMissingContainers(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root: bundleValue(t, `{
			"openapi":"3.0.4","paths":{"/pets":{"get":{"responses":{"200":{
				"description":"ok","content":{"application/json":{"schema":{
					"$ref":"models.json#/components/schemas/Pet"
				}}}
			}}}}}
		}`),
	}
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         bundleValue(t, `{"components":{"schemas":{"Pet":{"type":"object"}}}}`),
		}, nil
	})
	result, err := reference.BundleComponents(
		context.Background(), base, resolver, reference.DefaultBundleOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result.Document().Raw())
	if !strings.Contains(string(encoded), `"components":{"schemas":{"Pet":{"type":"object"}}}`) {
		t.Fatalf("bundled document = %s", encoded)
	}
}

func TestBundleComponentsInfersArbitraryPointerTargetType(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root: bundleValue(t, `{
			"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{"/pets":{"get":{"responses":{"200":{
				"$ref":"models.json#/arbitrary/Result"}}}}}
		}`),
	}
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         bundleValue(t, `{"arbitrary":{"Result":{"description":"OK"}}}`),
		}, nil
	})
	result, err := reference.BundleComponents(
		context.Background(), base, resolver, reference.DefaultBundleOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result.Document().Raw())
	for _, fragment := range []string{
		`"$ref":"#/components/responses/Result"`,
		`"responses":{"Result":{"description":"OK"}}`,
	} {
		if !strings.Contains(string(encoded), fragment) {
			t.Fatalf("bundled document = %s, want %s", encoded, fragment)
		}
	}
}

func TestBundleComponentsRejectsMismatchedTargetRegistry(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.0.4", "3.1.1", "3.1.2", "3.2.0"} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			base := reference.Resource{
				RetrievalURI: "https://api.example.test/root.json",
				Root: bundleValue(t, `{
					"openapi":"`+version+`","info":{"title":"API","version":"1"},
					"paths":{"/pets":{"get":{"responses":{"200":{
						"$ref":"models.json#/components/parameters/Limit"}}}}}
				}`),
			}
			resolver := reference.ResolverFunc(func(
				_ context.Context,
				identifier string,
			) (reference.Resource, error) {
				return reference.Resource{
					RetrievalURI: identifier,
					Root: bundleValue(t, `{"components":{"parameters":{"Limit":{
						"name":"limit","in":"query","schema":{"type":"integer"}
					}}}}`),
				}, nil
			})
			if _, err := reference.BundleComponents(
				context.Background(), base, resolver, reference.DefaultBundleOptions(),
			); !errors.Is(err, reference.ErrUnsupportedBundleTarget) {
				t.Fatalf("mismatched target error = %v", err)
			}
		})
	}
}

func TestBundleComponentsDeduplicatesExternalTargets(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root: bundleValue(t, `{
			"openapi":"3.1.2","paths":{},
			"components":{"schemas":{
				"One":{"$ref":"models.json#/components/schemas/Pet"},
				"Two":{"$ref":"models.json#/components/schemas/Pet"}
			}}
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
			Root:         bundleValue(t, `{"components":{"schemas":{"Pet":{"type":"object"}}}}`),
		}, nil
	})
	result, err := reference.BundleComponents(
		context.Background(), base, resolver, reference.DefaultBundleOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(result.Entries()) != 2 {
		t.Fatalf("calls = %d entries = %#v", calls, result.Entries())
	}
	encoded, _ := json.Marshal(result.Document().Raw())
	if count := strings.Count(string(encoded), `"Pet":{"type":"object"}`); count != 1 {
		t.Fatalf("bundled target count = %d: %s", count, encoded)
	}
}

func TestBundleComponentsSupportsSwaggerDefinitions(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/swagger.json",
		Root: bundleValue(t, `{
			"swagger":"2.0","paths":{},
			"definitions":{"Local":{"$ref":"models.json#/definitions/Pet"}}
		}`),
	}
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         bundleValue(t, `{"definitions":{"Pet":{"type":"string"}}}`),
		}, nil
	})
	result, err := reference.BundleComponents(
		context.Background(), base, resolver, reference.DefaultBundleOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result.Document().Raw())
	if !strings.Contains(string(encoded), `"Local":{"$ref":"#/definitions/Pet"}`) ||
		!strings.Contains(string(encoded), `"Pet":{"type":"string"}`) {
		t.Fatalf("bundled Swagger document = %s", encoded)
	}
}

func TestBundleComponentsRejectsUnsupportedTargetsAndInvalidInputs(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root: bundleValue(t, `{
			"openapi":"3.2.0","paths":{},
			"components":{"schemas":{"Pet":{"$ref":"models.json#Pet"}}}
		}`),
	}
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         bundleValue(t, `{"$anchor":"Pet","type":"object"}`),
		}, nil
	})
	result, err := reference.BundleComponents(
		context.Background(), base, resolver, reference.DefaultBundleOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result.Document().Raw())
	if !strings.Contains(string(encoded),
		`"Pet_bundled":{"$anchor":"Pet","type":"object"}`) ||
		!strings.Contains(string(encoded),
			`"$ref":"#/components/schemas/Pet_bundled"`) {
		t.Fatalf("anchor target was not bundled: %s", encoded)
	}

	external := base
	external.Root = bundleValue(t, `{
		"openapi":"3.2.0","paths":{},
		"components":{"schemas":{"Pet":{
			"$ref":"models.json#/components/schemas/Pet"
		}}}
	}`)
	if _, err := reference.BundleComponents(
		context.Background(), external, nil, reference.DefaultBundleOptions(),
	); !errors.Is(err, reference.ErrExternalResolutionDisabled) {
		t.Fatalf("disabled resolver error = %v", err)
	}
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := reference.BundleComponents(
		//lint:ignore SA1012 This assertion verifies the nil-context contract.
		nil, external, resolver, reference.DefaultBundleOptions(),
	); err == nil {
		t.Fatal("nil context was accepted")
	}
	invalid := reference.Resource{Root: bundleValue(t, `{"not":"openapi"}`)}
	if _, err := reference.BundleComponents(
		context.Background(), invalid, resolver, reference.DefaultBundleOptions(),
	); !errors.Is(err, openapi.ErrInvalidDocument) {
		t.Fatalf("invalid document error = %v", err)
	}
	invalidLimits := reference.DefaultBundleOptions()
	invalidLimits.ReferenceLimits.MaxReferenceDepth = 0
	if _, err := reference.BundleComponents(
		context.Background(), external, resolver, invalidLimits,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("reference limit error = %v", err)
	}
	invalidComponents := external
	invalidComponents.Root = bundleValue(t, `{"openapi":"3.2.0","paths":{},"components":false}`)
	if _, err := reference.BundleComponents(
		context.Background(), invalidComponents, resolver, reference.DefaultBundleOptions(),
	); !errors.Is(err, reference.ErrBundleConflict) {
		t.Fatalf("invalid components error = %v", err)
	}
}

func TestBundleComponentsEnforcesLimitsCancellationAndResolverErrors(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root: bundleValue(t, `{
			"openapi":"3.2.0","paths":{},
			"components":{"schemas":{"Pet":{
				"$ref":"models.json#/components/schemas/Pet"
			}}}
		}`),
	}
	external := reference.Resource{
		RetrievalURI: "https://api.example.test/models.json",
		Root: bundleValue(t, `{"components":{"schemas":{
			"Pet":{"type":"object","properties":{"id":{"type":"integer"}}}
		}}}`),
	}
	resolver := reference.ResolverFunc(func(context.Context, string) (reference.Resource, error) {
		return external, nil
	})
	tests := []struct {
		name   string
		mutate func(*reference.BundleOptions)
	}{
		{name: "references", mutate: func(options *reference.BundleOptions) { options.MaxReferences = 0 }},
		{name: "components", mutate: func(options *reference.BundleOptions) { options.MaxComponents = 0 }},
		{name: "nodes", mutate: func(options *reference.BundleOptions) { options.MaxNodes = 1 }},
		{name: "depth", mutate: func(options *reference.BundleOptions) { options.MaxDepth = 1 }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			options := reference.DefaultBundleOptions()
			test.mutate(&options)
			if _, err := reference.BundleComponents(
				context.Background(), base, resolver, options,
			); !errors.Is(err, reference.ErrLimitExceeded) {
				t.Fatalf("limit error = %v", err)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reference.BundleComponents(
		ctx, base, resolver, reference.DefaultBundleOptions(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	injected := errors.New("resolver failed")
	failing := reference.ResolverFunc(func(context.Context, string) (reference.Resource, error) {
		return reference.Resource{}, injected
	})
	if _, err := reference.BundleComponents(
		context.Background(), base, failing, reference.DefaultBundleOptions(),
	); !errors.Is(err, injected) {
		t.Fatalf("resolver error = %v", err)
	}

	twoReferences := base
	twoReferences.Root = bundleValue(t, `{
		"openapi":"3.2.0","paths":{},"components":{"schemas":{
			"One":{"$ref":"models.json#/components/schemas/Pet"},
			"Two":{"$ref":"models.json#/components/schemas/Pet"}
		}}
	}`)
	limitedReferences := reference.DefaultBundleOptions()
	limitedReferences.MaxReferences = 1
	if _, err := reference.BundleComponents(
		context.Background(), twoReferences, resolver, limitedReferences,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("runtime reference limit error = %v", err)
	}

	cancelCtx, cancelDuringResolve := context.WithCancel(context.Background())
	canceling := reference.ResolverFunc(func(
		context.Context,
		string,
	) (reference.Resource, error) {
		cancelDuringResolve()
		return external, nil
	})
	if _, err := reference.BundleComponents(
		cancelCtx, base, canceling, reference.DefaultBundleOptions(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("target rewrite cancellation error = %v", err)
	}
}

func TestBundleComponentsRewritesExternalReferencesBackToBase(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		CanonicalURI: "https://canonical.example.test/root.json",
		Root: bundleValue(t, `{
			"openapi":"3.2.0","paths":{},"components":{"schemas":{
				"Local":{"$anchor":"local","type":"string"},
				"Imported":{"$ref":"https://api.example.test/models.json#/components/schemas/Pet"}
			}}
		}`),
	}
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		switch identifier {
		case "https://api.example.test/models.json":
			return reference.Resource{
				RetrievalURI: identifier,
				Root: bundleValue(t, `{"components":{"schemas":{"Pet":{
					"pointer":{"$ref":"https://canonical.example.test/root.json#/components/schemas/Local"},
					"anchor":{"$ref":"https://canonical.example.test/root.json#local"},
					"root":{"$ref":"https://canonical.example.test/root.json#"}
				}}}}`),
			}, nil
		default:
			t.Fatalf("unexpected resolver identifier %q", identifier)
			return reference.Resource{}, nil
		}
	})
	result, err := reference.BundleComponents(
		context.Background(), base, resolver, reference.DefaultBundleOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result.Document().Raw())
	for _, local := range []string{
		`"$ref":"#/components/schemas/Local"`,
		`"$ref":"#local"`,
		`"$ref":"#"`,
	} {
		if !strings.Contains(string(encoded), local) {
			t.Fatalf("bundled document = %s, want %s", encoded, local)
		}
	}
}

func TestBundleComponentsRejectsRegistryAndNameBoundaries(t *testing.T) {
	t.Parallel()

	baseFor := func(referenceText string, schemas string) reference.Resource {
		if schemas != "" {
			schemas = "," + schemas
		}
		return reference.Resource{
			RetrievalURI: "https://api.example.test/root.json",
			Root: bundleValue(t, `{"openapi":"3.2.0","paths":{},
				"components":{"schemas":{"Alias":{"$ref":"`+
				referenceText+`"}`+schemas+`}}}`),
		}
	}
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{
			RetrievalURI: identifier,
			Root: bundleValue(t, `{
				"components":{
					"schemas":{
						"Pet":{"type":"object"},
						"Pet_bundled":{"type":"object"},
						"Bad/Name":{"type":"object"}
					},
					"parameters":{"Pet":{"name":"id","in":"query"}},
					"unknown":{"Pet":{}}
				}
			}`),
		}, nil
	})

	unsupported := baseFor(
		"models.json#/components/parameters/Pet", "",
	)
	if _, err := reference.BundleComponents(
		context.Background(), unsupported, resolver, reference.DefaultBundleOptions(),
	); !errors.Is(err, reference.ErrUnsupportedBundleTarget) {
		t.Fatalf("unsupported registry error = %v", err)
	}
	badName := baseFor(
		"models.json#/components/schemas/Bad~1Name", "",
	)
	if _, err := reference.BundleComponents(
		context.Background(), badName, resolver, reference.DefaultBundleOptions(),
	); !errors.Is(err, reference.ErrUnsupportedBundleTarget) {
		t.Fatalf("invalid name error = %v", err)
	}

	tooLong := reference.DefaultBundleOptions()
	tooLong.MaxComponentNameBytes = 2
	if _, err := reference.BundleComponents(
		context.Background(), baseFor("models.json#/components/schemas/Pet", ""), resolver, tooLong,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("source name limit error = %v", err)
	}

	componentLimit := reference.DefaultBundleOptions()
	componentLimit.MaxComponents = 1
	graphBase := baseFor("models.json#/components/schemas/Pet", "")
	graphResolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{
			RetrievalURI: identifier,
			Root: bundleValue(t, `{"components":{"schemas":{
				"Pet":{"$ref":"#/components/schemas/Pet_bundled"},
				"Pet_bundled":{"type":"object"}
			}}}`),
		}, nil
	})
	if _, err := reference.BundleComponents(
		context.Background(), graphBase, graphResolver, componentLimit,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("runtime component limit error = %v", err)
	}

	collisionBase := baseFor(
		"models.json#/components/schemas/Pet",
		`"Pet":{},"Pet_bundled":{}`,
	)
	result, err := reference.BundleComponents(
		context.Background(), collisionBase, resolver, reference.DefaultBundleOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result.Document().Raw())
	if !strings.Contains(string(encoded), `"$ref":"#/components/schemas/Pet_bundled_2"`) {
		t.Fatalf("collision result = %s", encoded)
	}

	generatedLimit := reference.DefaultBundleOptions()
	generatedLimit.MaxComponentNameBytes = len("Pet_bundled")
	if _, err := reference.BundleComponents(
		context.Background(), collisionBase, resolver, generatedLimit,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("generated name limit error = %v", err)
	}
}

func TestBundleComponentsRejectsInvalidReferencesAndRegistries(t *testing.T) {
	t.Parallel()

	invalidReference := reference.Resource{
		Root: bundleValue(t, `{
			"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"$ref":false}}}
		}`),
	}
	if _, err := reference.BundleComponents(
		context.Background(), invalidReference, nil, reference.DefaultBundleOptions(),
	); !errors.Is(err, reference.ErrInvalidReference) {
		t.Fatalf("invalid reference error = %v", err)
	}

	invalidRegistry := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root: bundleValue(t, `{
			"openapi":"3.2.0","paths":{"/pets":{"get":{"responses":{
				"200":{"description":"OK","content":{"application/json":{
					"schema":{"$ref":"models.json#/components/schemas/Pet"}
				}}}}}}},
			"components":{"schemas":false}
		}`),
	}
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         bundleValue(t, `{"components":{"schemas":{"Pet":{}}}}`),
		}, nil
	})
	if _, err := reference.BundleComponents(
		context.Background(), invalidRegistry, resolver, reference.DefaultBundleOptions(),
	); !errors.Is(err, reference.ErrBundleConflict) {
		t.Fatalf("invalid registry error = %v", err)
	}
}

func TestBundleComponentsPreservesReferenceShapedData(t *testing.T) {
	t.Parallel()

	base := reference.Resource{Root: bundleValue(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"examples":{"Payload":{"value":{
			"headers":{"trace":{"$ref":"data.json#/literal"}}
		}},"DataPayload":{"dataValue":{"$ref":"data-value.json#/literal"}}
		},"schemas":{"Payload":{"type":"object",
			"default":{"$ref":"default.json#/literal"},
			"const":{"$ref":"const.json#/literal"},
			"examples":[{"$ref":"example.json#/literal"}]
		}},"parameters":{"Payload":{"name":"payload","in":"query",
			"example":{"$ref":"parameter.json#/literal"},"schema":{}}
		},"links":{"Payload":{"operationId":"receive",
			"requestBody":{"$ref":"body.json#/literal"}
		}}},
		"x-payload":{"responses":{"200":{"$ref":"other.json#/literal"}}}
	}`)}
	calls := 0
	resolver := reference.ResolverFunc(func(
		context.Context,
		string,
	) (reference.Resource, error) {
		calls++
		return reference.Resource{}, errors.New("unexpected resolver call")
	})
	result, err := reference.BundleComponents(
		context.Background(), base, resolver, reference.DefaultBundleOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := base.Root.MarshalJSON()
	after, _ := result.Document().Raw().MarshalJSON()
	if string(after) != string(before) {
		t.Fatalf("reference-shaped data changed:\n%s\n%s", before, after)
	}
	if calls != 0 || len(result.Entries()) != 0 {
		t.Fatalf("resolver calls = %d, entries = %#v", calls, result.Entries())
	}
}

func TestBundleComponentsPreservesSwaggerExampleData(t *testing.T) {
	t.Parallel()

	base := reference.Resource{Root: bundleValue(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},"paths":{},
		"responses":{"Payload":{"description":"OK","examples":{
			"application/json":{"$ref":"data.json#/literal"}
		}}}
	}`)}
	calls := 0
	resolver := reference.ResolverFunc(func(
		context.Context,
		string,
	) (reference.Resource, error) {
		calls++
		return reference.Resource{}, errors.New("unexpected resolver call")
	})
	result, err := reference.BundleComponents(
		context.Background(), base, resolver, reference.DefaultBundleOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := base.Root.MarshalJSON()
	after, _ := result.Document().Raw().MarshalJSON()
	if string(after) != string(before) || calls != 0 {
		t.Fatalf("reference-shaped example changed: calls=%d\n%s\n%s",
			calls, before, after)
	}
}

func TestBundleComponentsResolvesXPrefixedMapEntries(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root: bundleValue(t, `{
			"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{},"components":{"schemas":{
				"x-Alias":{"$ref":"models.json#/components/schemas/Pet"},
				"Container":{"type":"object","properties":{
					"x-child":{"$ref":"models.json#/components/schemas/Pet"},
					"example":{"$ref":"models.json#/components/schemas/Pet"},
					"default":{"$ref":"models.json#/components/schemas/Pet"}
				}}
			}}
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
			Root: bundleValue(t, `{"components":{"schemas":{
				"Pet":{"type":"object"}}}}`),
		}, nil
	})
	result, err := reference.BundleComponents(
		context.Background(), base, resolver, reference.DefaultBundleOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(result.Entries()) != 4 {
		t.Fatalf("calls = %d, entries = %#v", calls, result.Entries())
	}
	encoded, _ := result.Document().Raw().MarshalJSON()
	if strings.Count(string(encoded), `"$ref":"#/components/schemas/Pet"`) != 4 {
		t.Fatalf("bundled document = %s", encoded)
	}
}

func TestBundleComponentsBoundsExistingRegistryInventory(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{"openapi":"3.2.0","paths":{},"components":{"schemas":{"One":{},"Two":{}}}}`,
		`{"swagger":"2.0","paths":{},"definitions":{"One":{},"Two":{}}}`,
	} {
		base := reference.Resource{Root: bundleValue(t, raw)}
		options := reference.DefaultBundleOptions()
		options.MaxComponents = 1
		if _, err := reference.BundleComponents(
			context.Background(), base, nil, options,
		); !errors.Is(err, reference.ErrLimitExceeded) {
			t.Fatalf("inventory limit error = %v", err)
		}
	}
}

func bundleValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := parse.JSON(
		context.Background(), strings.NewReader(raw), parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
