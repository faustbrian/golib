package reference

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

type cancelAfterFirstReferenceCheck struct {
	context.Context
	calls int
}

func (ctx *cancelAfterFirstReferenceCheck) Err() error {
	ctx.calls++
	if ctx.calls > 1 {
		return context.Canceled
	}
	return nil
}

func TestBundleArrayTraversalAndElementFailures(t *testing.T) {
	t.Parallel()

	array, _ := jsonvalue.Array([]jsonvalue.Value{jsonvalue.Boolean(true)})
	bundler := componentBundler{
		ctx: context.Background(),
		options: BundleOptions{
			MaxDepth: 2,
			MaxNodes: 10,
		},
	}
	result, err := bundler.rewriteValue(Resource{}, array, "/array", 1, "")
	if err != nil || result.Kind() != jsonvalue.ArrayKind {
		t.Fatalf("rewriteValue() result = %#v, error = %v", result, err)
	}
	bundler.options.MaxDepth = 1
	bundler.nodes = 0
	if _, err := bundler.rewriteValue(
		Resource{}, array, "/array", 1, "",
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("rewriteValue() error = %v", err)
	}
	if _, err := bundler.rewriteValue(
		Resource{}, jsonvalue.Null(), "/scalar", 2, "",
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("scalar depth error = %v", err)
	}
	bundler.options.MaxDepth = 2
	bundler.options.MaxNodes = 1
	bundler.nodes = 1
	if _, err := bundler.rewriteValue(
		Resource{}, jsonvalue.Null(), "/scalar", 1, "",
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("scalar node error = %v", err)
	}
	bundler.ctx = &cancelAfterFirstReferenceCheck{Context: context.Background()}
	bundler.options.MaxNodes = 10
	bundler.nodes = 0
	if _, err := bundler.rewriteValue(
		Resource{}, array, "/array", 1, "",
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("array child cancellation = %v", err)
	}
}

func TestBundleTraversalAdvancesArrayAndObjectDepth(t *testing.T) {
	t.Parallel()

	leafArray, _ := jsonvalue.Array([]jsonvalue.Value{jsonvalue.Null()})
	nestedArray, _ := jsonvalue.Array([]jsonvalue.Value{leafArray})
	nestedObject, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "nested", Value: leafArray},
	})
	for _, value := range []jsonvalue.Value{nestedArray, nestedObject} {
		bundler := componentBundler{
			ctx: context.Background(),
			options: BundleOptions{
				MaxDepth: 1,
				MaxNodes: 10,
			},
		}
		if _, err := bundler.rewriteValue(
			Resource{}, value, "", 0, "",
		); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("nested traversal error = %v", err)
		}
	}
}

func TestBundleMinimumAndExactWorkLimits(t *testing.T) {
	t.Parallel()

	options := DefaultBundleOptions()
	options.ReferenceLimits = Limits{
		MaxTraversalDepth: 1, MaxTraversalNodes: 1, MaxReferenceDepth: 1,
	}
	options.MaxReferences = 1
	options.MaxComponents = 1
	options.MaxNodes = 1
	options.MaxDepth = 1
	options.MaxComponentNameBytes = 1
	if err := options.validate(); err != nil {
		t.Fatalf("minimum bundle options error = %v", err)
	}

	registry, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "Existing", Value: jsonvalue.Null()},
	})
	container, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "schemas", Value: registry},
	})
	bundler := componentBundler{
		ctx: context.Background(), options: options,
		occupied: make(map[string]map[string]bool),
	}
	if err := bundler.inventoryRegistry(container, "schemas"); err != nil {
		t.Fatalf("exact component inventory error = %v", err)
	}
	bundler.components = 0
	if got, err := bundler.rewriteValue(
		Resource{}, jsonvalue.Null(), "", 1, "",
	); err != nil || got.Kind() != jsonvalue.NullKind {
		t.Fatalf("exact node rewrite = %#v, %v", got, err)
	}
}

func TestBundleHelpersRejectImpossibleDestinationStates(t *testing.T) {
	t.Parallel()

	value, _ := jsonvalue.String("value")
	registry, _ := jsonvalue.Object([]jsonvalue.Member{{Name: "Pet", Value: value}})
	container, _ := jsonvalue.Object([]jsonvalue.Member{{Name: "schemas", Value: registry}})
	if _, err := appendBundleEntry(
		container, "schemas", "Pet", value,
	); !errors.Is(err, ErrBundleConflict) {
		t.Fatalf("occupied entry error = %v", err)
	}
	if _, err := replaceOrAppendBundleMember(
		jsonvalue.Boolean(false), "schemas", registry,
	); !errors.Is(err, ErrBundleConflict) {
		t.Fatalf("non-object destination error = %v", err)
	}
	if _, err := localFragmentReference(Fragment{kind: FragmentKind(255)}); !errors.Is(err, ErrInvalidFragment) {
		t.Fatalf("invalid fragment error = %v", err)
	}
	if !bundleExtensionMember("invalid", "x-test") {
		t.Fatal("extension at an invalid internal pointer was not preserved")
	}
	if bundleExtensionMember("/components/schemas", "X-test") {
		t.Fatal("component map key was treated as an extension")
	}
	if bundleExtensionMember("/paths", "x") {
		t.Fatal("one-byte name was treated as an extension")
	}
	if !bundleExtensionMember("/paths", "x-") {
		t.Fatal("minimum extension name was not recognized")
	}
	if tokens := mustBundlePointerTokens("invalid"); tokens != nil {
		t.Fatalf("invalid pointer tokens = %#v", tokens)
	}
}

func TestLocalFragmentReferenceEscapesURIFragmentData(t *testing.T) {
	t.Parallel()

	pointer, err := ParsePointer("/a b")
	if err != nil {
		t.Fatal(err)
	}
	got, err := localFragmentReference(Fragment{
		kind:    FragmentPointer,
		pointer: pointer,
	})
	if err != nil || got != "#/a%20b" {
		t.Fatalf("pointer reference = %q, error = %v", got, err)
	}
	got, err = localFragmentReference(Fragment{
		kind:   FragmentAnchor,
		anchor: "a b",
	})
	if err != nil || got != "#a%20b" {
		t.Fatalf("anchor reference = %q, error = %v", got, err)
	}
}

func TestBundleRejectsUnsupportedSwaggerRegistry(t *testing.T) {
	t.Parallel()

	pointer, err := ParsePointer("/components/schemas/Pet")
	if err != nil {
		t.Fatal(err)
	}
	bundler := componentBundler{dialect: specversion.DialectSwagger20}
	if _, err := bundler.targetLocation(Fragment{
		kind:    FragmentPointer,
		pointer: pointer,
	}, "", ""); !errors.Is(err, ErrUnsupportedBundleTarget) {
		t.Fatalf("targetLocation() error = %v", err)
	}
	oas := componentBundler{dialect: specversion.DialectOAS32}
	if _, err := oas.targetLocation(Fragment{
		kind: FragmentPointer, pointer: pointer,
	}, "/components/responses/Shared/$ref", ""); !errors.Is(
		err, ErrUnsupportedBundleTarget,
	) {
		t.Fatalf("mismatched target location error = %v", err)
	}
	if _, err := oas.targetLocation(
		Fragment{kind: FragmentKind(255)},
		"/components/schemas/Pet/$ref",
		"",
	); !errors.Is(err, ErrUnsupportedBundleTarget) {
		t.Fatalf("invalid target fragment error = %v", err)
	}
}

func TestBundleSourceRegistryInferenceCoversSupportedLocations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dialect  specversion.Dialect
		pointer  string
		hint     string
		registry string
	}{
		{name: "invalid pointer", dialect: specversion.DialectOAS32,
			pointer: "invalid", registry: ""},
		{name: "short pointer", dialect: specversion.DialectOAS32,
			pointer: "/$ref", registry: ""},
		{name: "minimum definition pointer", dialect: specversion.DialectSwagger20,
			pointer: "/definitions/$ref", registry: "definitions"},
		{name: "swagger definition schema", dialect: specversion.DialectSwagger20,
			pointer: "/definitions/Pet/properties/id/$ref", registry: "definitions"},
		{name: "swagger direct response", dialect: specversion.DialectSwagger20,
			pointer: "/responses/Shared/$ref", registry: "responses"},
		{name: "swagger nested response", dialect: specversion.DialectSwagger20,
			pointer: "/paths/~1pets/get/responses/200/$ref", registry: "responses"},
		{name: "swagger nested response schema", dialect: specversion.DialectSwagger20,
			pointer: "/paths/~1pets/get/responses/200/schema/$ref", registry: "definitions"},
		{name: "swagger unsupported", dialect: specversion.DialectSwagger20,
			pointer: "/paths/~1pets/get/x/$ref", registry: ""},
		{name: "not a reference", dialect: specversion.DialectOAS32,
			pointer: "/components/schemas/Pet/type", registry: ""},
		{name: "oas component schema", dialect: specversion.DialectOAS32,
			pointer: "/components/schemas/Pet/properties/id/$ref", registry: "schemas"},
		{name: "oas direct response", dialect: specversion.DialectOAS32,
			pointer: "/components/responses/Shared/$ref", registry: "responses"},
		{name: "oas path item", dialect: specversion.DialectOAS32,
			pointer: "/paths/~1pets/$ref", registry: "pathItems"},
		{name: "oas nested response", dialect: specversion.DialectOAS32,
			pointer: "/paths/~1pets/get/responses/200/$ref", registry: "responses"},
		{name: "oas request body", dialect: specversion.DialectOAS32,
			pointer: "/paths/~1pets/post/requestBody/$ref", registry: "requestBodies"},
		{name: "schema hint", dialect: specversion.DialectOAS32,
			pointer: "/x/y/$ref", hint: "schemas", registry: "schemas"},
		{name: "unsupported", dialect: specversion.DialectOAS32,
			pointer: "/x/y/$ref", registry: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bundler := componentBundler{dialect: test.dialect}
			if got := bundler.sourceRegistry(test.pointer, test.hint); got != test.registry {
				t.Fatalf("sourceRegistry() = %q, want %q", got, test.registry)
			}
		})
	}
}

func TestKnownBundleTargetLocationRequiresExactRegistryPointers(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		dialect specversion.Dialect
		pointer string
		known   bool
	}{
		{dialect: specversion.DialectSwagger20,
			pointer: "/responses/Shared", known: true},
		{dialect: specversion.DialectSwagger20,
			pointer: "/responses", known: false},
		{dialect: specversion.DialectSwagger20,
			pointer: "/unknown/Shared", known: false},
		{dialect: specversion.DialectOAS32,
			pointer: "/components/responses/Shared", known: true},
		{dialect: specversion.DialectOAS32,
			pointer: "/ordinary/responses/Shared", known: false},
		{dialect: specversion.DialectOAS32,
			pointer: "/components/responses", known: false},
	} {
		pointer, err := ParsePointer(test.pointer)
		if err != nil {
			t.Fatal(err)
		}
		_, known := knownBundleTargetLocation(test.dialect, Fragment{
			kind: FragmentPointer, pointer: pointer,
		})
		if known != test.known {
			t.Fatalf("pointer %q known = %t", test.pointer, known)
		}
	}
}

func TestDerivedBundleNamesCoverEveryFragmentForm(t *testing.T) {
	t.Parallel()

	root, _ := ParseFragment("")
	anchor := Fragment{kind: FragmentAnchor, anchor: "named anchor"}
	emptyPointer := Fragment{kind: FragmentPointer, pointer: Pointer{}}
	pointer, _ := ParseFragment("/components/schemas/Pet%20Record")
	endpoints := Fragment{kind: FragmentAnchor, anchor: "aAzZ09._-@"}
	for _, test := range []struct {
		fragment Fragment
		want     string
	}{
		{fragment: root, want: "root"},
		{fragment: anchor, want: "named_anchor"},
		{fragment: emptyPointer, want: "root"},
		{fragment: pointer, want: "Pet_Record"},
		{fragment: endpoints, want: "aAzZ09._-_"},
		{fragment: Fragment{kind: FragmentAnchor}, want: "bundled"},
		{fragment: Fragment{kind: FragmentKind(255)}, want: ""},
	} {
		if got := derivedBundleName(test.fragment); got != test.want {
			t.Fatalf("derivedBundleName(%#v) = %q, want %q",
				test.fragment, got, test.want)
		}
	}
}

func TestAllocateBundleNameTracksExactComponentAndNameLimits(t *testing.T) {
	t.Parallel()

	bundler := componentBundler{
		options: BundleOptions{
			MaxComponents: 3, MaxComponentNameBytes: len("Pet_bundled_2"),
		},
		occupied: map[string]map[string]bool{
			"schemas": {"Pet": true, "Pet_bundled": true},
		},
		components: 2,
	}
	name, err := bundler.allocateName("schemas", "Pet")
	if err != nil || name != "Pet_bundled_2" || bundler.components != 3 {
		t.Fatalf("allocated name = %q, components = %d, error = %v",
			name, bundler.components, err)
	}
	if _, err := bundler.allocateName("schemas", "New"); !errors.Is(
		err, ErrLimitExceeded,
	) {
		t.Fatalf("exact component limit error = %v", err)
	}
	exactName := componentBundler{
		options:  BundleOptions{MaxComponents: 1, MaxComponentNameBytes: 3},
		occupied: make(map[string]map[string]bool),
	}
	if got, err := exactName.allocateName("schemas", "Pet"); err != nil ||
		got != "Pet" {
		t.Fatalf("exact source name = %q, %v", got, err)
	}
	exhausted := componentBundler{
		options: BundleOptions{
			MaxComponents: 1, MaxComponentNameBytes: len("Pet_bundled_2"),
		},
		occupied: map[string]map[string]bool{
			"schemas": {
				"Pet": true, "Pet_bundled": true, "Pet_bundled_2": true,
			},
		},
	}
	if _, err := exhausted.allocateName("schemas", "Pet"); !errors.Is(
		err, ErrLimitExceeded,
	) {
		t.Fatalf("exhausted name space error = %v", err)
	}
}

func TestBundleReferenceAcceptsExactReferenceLimit(t *testing.T) {
	t.Parallel()

	base := Resource{Root: jsonvalue.Null()}
	bundler := componentBundler{
		ctx: context.Background(), base: base,
		options: BundleOptions{
			ReferenceLimits: DefaultLimits(), MaxReferences: 1,
		},
	}
	got, err := bundler.bundleReference(base, "#", "/value/$ref", "")
	if err != nil || got != "#" || bundler.references != 1 {
		t.Fatalf("exact reference limit = %q, %d, %v",
			got, bundler.references, err)
	}
}
