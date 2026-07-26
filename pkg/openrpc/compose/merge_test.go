package compose_test

import (
	"context"
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/compose"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestMergeCombinesMethodsDeterministically(t *testing.T) {
	t.Parallel()

	merged, err := compose.Merge(context.Background(), []openrpc.Document{
		testDocument(t, "zulu"),
		testDocument(t, "alpha"),
	}, compose.DefaultMergeOptions())
	if err != nil {
		t.Fatal(err)
	}
	methods := merged.Methods()
	first, _ := methods[0].Method()
	second, _ := methods[1].Method()
	if first.Name() != "alpha" || second.Name() != "zulu" {
		t.Fatalf("methods = %q, %q", first.Name(), second.Name())
	}
}

func TestMergeAppliesExplicitConflictPolicy(t *testing.T) {
	t.Parallel()

	first := testDocument(t, "same")
	second := testDocument(t, "same")
	if _, err := compose.Merge(context.Background(), []openrpc.Document{first, second}, compose.DefaultMergeOptions()); !errors.Is(err, compose.ErrMergeConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	options := compose.DefaultMergeOptions()
	options.Conflict = compose.KeepLast
	if _, err := compose.Merge(context.Background(), []openrpc.Document{first, second}, options); err != nil {
		t.Fatal(err)
	}
}

func TestMergeDetectsComponentCollisionsAndBounds(t *testing.T) {
	t.Parallel()

	withSchema := func(source string) openrpc.Document {
		schema, err := jsonschema.Parse([]byte(source), jsonvalue.DefaultPolicy())
		if err != nil {
			t.Fatal(err)
		}
		components, err := openrpc.NewComponents(openrpc.ComponentsInput{Schemas: map[string]jsonschema.Schema{"Value": schema}})
		if err != nil {
			t.Fatal(err)
		}
		document := testDocument(t)
		info := document.Info()
		document, err = openrpc.NewDocument(openrpc.DocumentInput{Version: document.Version(), Info: &info, Methods: []openrpc.MethodOrReference{}, Components: &components})
		if err != nil {
			t.Fatal(err)
		}
		return document
	}
	if _, err := compose.Merge(context.Background(), []openrpc.Document{withSchema(`true`), withSchema(`false`)}, compose.DefaultMergeOptions()); !errors.Is(err, compose.ErrMergeConflict) {
		t.Fatalf("component conflict error = %v", err)
	}
	options := compose.DefaultMergeOptions()
	options.MaxDocuments = 1
	if _, err := compose.Merge(context.Background(), []openrpc.Document{testDocument(t), testDocument(t)}, options); !errors.Is(err, compose.ErrMergeLimit) {
		t.Fatalf("limit error = %v", err)
	}
	if _, err := compose.Merge(context.Background(), nil, compose.DefaultMergeOptions()); !errors.Is(err, compose.ErrInvalidMerge) {
		t.Fatalf("empty error = %v", err)
	}
}

func TestMergePreservesExplicitEmptyComponentMaps(t *testing.T) {
	t.Parallel()

	components, err := openrpc.NewComponents(openrpc.ComponentsInput{
		Schemas: map[string]jsonschema.Schema{},
	})
	if err != nil {
		t.Fatal(err)
	}
	document := testDocument(t)
	info := document.Info()
	document, err = openrpc.NewDocument(openrpc.DocumentInput{
		Version: document.Version(), Info: &info,
		Methods: []openrpc.MethodOrReference{}, Components: &components,
	})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := compose.Merge(
		context.Background(), []openrpc.Document{document},
		compose.DefaultMergeOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	mergedComponents, present := merged.Components()
	if !present {
		t.Fatal("components reported absent")
	}
	schemas, present := mergedComponents.Schemas()
	if !present || schemas == nil || len(schemas) != 0 {
		t.Fatalf("Schemas() = (%#v, %t)", schemas, present)
	}
}

func TestMergeCombinesEveryComponentRegistry(t *testing.T) {
	t.Parallel()

	first := documentWithComponents(t, completeComponents(t, "first"))
	second := documentWithComponents(t, completeComponents(t, "second"))
	merged, err := compose.Merge(context.Background(), []openrpc.Document{first, second}, compose.DefaultMergeOptions())
	if err != nil {
		t.Fatal(err)
	}
	components, present := merged.Components()
	if !present {
		t.Fatal("components reported absent")
	}
	assertRegistrySize := func(name string, size int, present bool) {
		t.Helper()
		if !present || size != 2 {
			t.Fatalf("%s size = %d, present = %t", name, size, present)
		}
	}
	schemas, present := components.Schemas()
	assertRegistrySize("schemas", len(schemas), present)
	links, present := components.Links()
	assertRegistrySize("links", len(links), present)
	errorsRegistry, present := components.Errors()
	assertRegistrySize("errors", len(errorsRegistry), present)
	examples, present := components.Examples()
	assertRegistrySize("examples", len(examples), present)
	pairings, present := components.ExamplePairings()
	assertRegistrySize("example pairings", len(pairings), present)
	descriptors, present := components.ContentDescriptors()
	assertRegistrySize("content descriptors", len(descriptors), present)
	tags, present := components.Tags()
	assertRegistrySize("tags", len(tags), present)
}

func TestMergeRejectsInvalidOptionsAndResourceLimits(t *testing.T) {
	t.Parallel()

	document := testDocument(t, "one", "two")
	invalidOptions := []compose.MergeOptions{
		{},
		{Conflict: compose.ConflictPolicy(255), MaxDocuments: 1, MaxMethods: 1, MaxComponents: 1},
		{MaxDocuments: 1, MaxMethods: 1, MaxComponents: 0},
	}
	for _, options := range invalidOptions {
		if _, err := compose.Merge(context.Background(), []openrpc.Document{document}, options); !errors.Is(err, compose.ErrInvalidMerge) {
			t.Fatalf("invalid options error = %v", err)
		}
	}
	options := compose.DefaultMergeOptions()
	options.MaxMethods = 1
	if _, err := compose.Merge(context.Background(), []openrpc.Document{document}, options); !errors.Is(err, compose.ErrMergeLimit) {
		t.Fatalf("method limit error = %v", err)
	}
	options = compose.DefaultMergeOptions()
	options.MaxComponents = 1
	withComponents := documentWithComponents(t, completeComponents(t, "limited"))
	if _, err := compose.Merge(context.Background(), []openrpc.Document{withComponents}, options); !errors.Is(err, compose.ErrMergeLimit) {
		t.Fatalf("component limit error = %v", err)
	}
}

func TestMergeRejectsVersionMismatchAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	first := testDocument(t)
	version, err := openrpc.ParseVersion("1.4.0")
	if err != nil {
		t.Fatal(err)
	}
	info := first.Info()
	second, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version: version, Info: &info, Methods: []openrpc.MethodOrReference{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compose.Merge(context.Background(), []openrpc.Document{first, second}, compose.DefaultMergeOptions()); !errors.Is(err, compose.ErrMergeConflict) {
		t.Fatalf("version mismatch error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := compose.Merge(ctx, []openrpc.Document{first}, compose.DefaultMergeOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func completeComponents(t *testing.T, suffix string) openrpc.Components {
	t.Helper()
	schema, err := jsonschema.Parse([]byte(`true`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	link, err := openrpc.NewLink(openrpc.LinkInput{})
	if err != nil {
		t.Fatal(err)
	}
	code, err := openrpc.ParseInteger("-32000")
	if err != nil {
		t.Fatal(err)
	}
	errorValue, err := openrpc.NewError(openrpc.ErrorInput{Code: code, Message: "failure"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonvalue.Parse([]byte(`null`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	example, err := openrpc.NewExample(openrpc.ExampleInput{Name: suffix, Value: value})
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := openrpc.NewExamplePairing(openrpc.ExamplePairingInput{Name: suffix, Params: []openrpc.ExampleOrReference{}})
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := openrpc.NewContentDescriptor(openrpc.ContentDescriptorInput{Name: suffix, Schema: &schema})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := openrpc.NewTag(openrpc.TagInput{Name: suffix})
	if err != nil {
		t.Fatal(err)
	}
	components, err := openrpc.NewComponents(openrpc.ComponentsInput{
		Schemas:            map[string]jsonschema.Schema{suffix: schema},
		Links:              map[string]openrpc.Link{suffix: link},
		Errors:             map[string]openrpc.Error{suffix: errorValue},
		Examples:           map[string]openrpc.Example{suffix: example},
		ExamplePairings:    map[string]openrpc.ExamplePairing{suffix: pairing},
		ContentDescriptors: map[string]openrpc.ContentDescriptor{suffix: descriptor},
		Tags:               map[string]openrpc.Tag{suffix: tag},
	})
	if err != nil {
		t.Fatal(err)
	}
	return components
}

func documentWithComponents(t *testing.T, components openrpc.Components) openrpc.Document {
	t.Helper()
	document := testDocument(t)
	info := document.Info()
	merged, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version: document.Version(), Info: &info,
		Methods: []openrpc.MethodOrReference{}, Components: &components,
	})
	if err != nil {
		t.Fatal(err)
	}
	return merged
}
