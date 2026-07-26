package compose_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/compose"
)

func TestMergeCombinesRegistriesInStableSourceOrder(t *testing.T) {
	t.Parallel()

	first := composeDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{}}}},
		"components":{"schemas":{"Pet":{"type":"object"}}},
		"x-owner":"platform"
	}`)
	second := composeDocument(t, `{
		"openapi":"3.2.0",
		"info":{"version":"1","title":"API"},
		"paths":{"/orders":{"post":{"responses":{}}}},
		"webhooks":{"order.created":{"post":{"responses":{}}}},
		"components":{
			"schemas":{"Order":{"type":"object"}},
			"parameters":{"Trace":{"name":"trace","in":"header"}}
		},
		"x-owner":"platform"
	}`)

	result, err := compose.Merge(
		context.Background(),
		[]openapi.Document{first, second},
		compose.DefaultMergeOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result.Document().Raw())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{"/pets":{"get":{"responses":{}}},"/orders":{"post":{"responses":{}}}},"components":{"schemas":{"Pet":{"type":"object"},"Order":{"type":"object"}},"parameters":{"Trace":{"name":"trace","in":"header"}}},"x-owner":"platform","webhooks":{"order.created":{"post":{"responses":{}}}}}`
	if string(encoded) != want {
		t.Fatalf("merged document = %s\nwant = %s", encoded, want)
	}

	contributions := result.Contributions()
	wantPointers := []string{
		"/paths/~1orders",
		"/webhooks",
		"/components/schemas/Order",
		"/components/parameters",
	}
	if len(contributions) != len(wantPointers) {
		t.Fatalf("contributions = %#v", contributions)
	}
	for index, pointer := range wantPointers {
		if got := contributions[index].TargetPointer(); got != pointer {
			t.Fatalf("contribution %d pointer = %q, want %q", index, got, pointer)
		}
		if contributions[index].DocumentIndex() != 1 {
			t.Fatalf("contribution %d document = %d", index, contributions[index].DocumentIndex())
		}
		if contributions[index].SourcePointer() != pointer {
			t.Fatalf("contribution %d source = %q", index, contributions[index].SourcePointer())
		}
	}
	contributions[0] = compose.Contribution{}
	if result.Contributions()[0].TargetPointer() == "" {
		t.Fatal("result exposed mutable contribution storage")
	}
}

func TestMergeReportsRegistryConflictsWithProvenance(t *testing.T) {
	t.Parallel()

	first := composeDocument(t, `{"openapi":"3.1.2","paths":{"/pets":{"get":{}}}}`)
	second := composeDocument(t, `{"openapi":"3.1.2","paths":{"/pets":{"post":{}}}}`)
	_, err := compose.Merge(
		context.Background(),
		[]openapi.Document{first, second},
		compose.DefaultMergeOptions(),
	)
	if !errors.Is(err, compose.ErrConflict) {
		t.Fatalf("Merge() error = %v, want ErrConflict", err)
	}
	var conflict *compose.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Merge() error type = %T", err)
	}
	if conflict.Error() == "" {
		t.Fatal("conflict error message is empty")
	}
	if conflict.Pointer() != "/paths/~1pets" ||
		conflict.ExistingDocumentIndex() != 0 ||
		conflict.IncomingDocumentIndex() != 1 {
		t.Fatalf("conflict = %#v", conflict)
	}
	if conflict.Existing().Kind() == 0 || conflict.Incoming().Kind() == 0 {
		t.Fatal("conflict lost immutable source values")
	}
}

func TestMergeAppliesExplicitConflictDecisions(t *testing.T) {
	t.Parallel()

	first := composeDocument(t, `{"swagger":"2.0","paths":{},"definitions":{"Pet":{"type":"string"}}}`)
	second := composeDocument(t, `{"swagger":"2.0","paths":{},"definitions":{"Pet":{"type":"integer"}}}`)

	tests := []struct {
		name     string
		decision compose.ConflictDecision
		wantType string
		replaced bool
	}{
		{name: "keep existing", decision: compose.KeepExisting, wantType: "string"},
		{name: "use incoming", decision: compose.UseIncoming, wantType: "integer", replaced: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := compose.DefaultMergeOptions()
			options.ResolveConflict = func(conflict compose.Conflict) (compose.ConflictDecision, error) {
				if conflict.Pointer() != "/definitions/Pet" {
					t.Fatalf("conflict pointer = %q", conflict.Pointer())
				}
				return test.decision, nil
			}
			result, err := compose.Merge(
				context.Background(), []openapi.Document{first, second}, options,
			)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(result.Document().Raw())
			want := `"type":"` + test.wantType + `"`
			if !jsonContains(string(encoded), want) {
				t.Fatalf("merged document = %s, want %s", encoded, want)
			}
			contributions := result.Contributions()
			if len(contributions) != 1 || contributions[0].Replaced() != test.replaced {
				t.Fatalf("contributions = %#v", contributions)
			}
		})
	}
}

func TestMergeRejectsInvalidInputsVersionsPoliciesAndLimits(t *testing.T) {
	t.Parallel()

	document := composeDocument(t, `{"openapi":"3.2.0","paths":{}}`)
	otherVersion := composeDocument(t, `{"openapi":"3.1.2","paths":{}}`)
	//lint:ignore SA1012 This assertion verifies the nil-context contract.
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := compose.Merge(nil, []openapi.Document{document}, compose.DefaultMergeOptions()); !errors.Is(err, compose.ErrInvalidInput) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := compose.Merge(context.Background(), nil, compose.DefaultMergeOptions()); !errors.Is(err, compose.ErrInvalidInput) {
		t.Fatalf("empty documents error = %v", err)
	}
	if _, err := compose.Merge(context.Background(), []openapi.Document{nil}, compose.DefaultMergeOptions()); !errors.Is(err, compose.ErrInvalidInput) {
		t.Fatalf("nil document error = %v", err)
	}
	if _, err := compose.Merge(context.Background(), []openapi.Document{document}, compose.MergeOptions{}); err != nil {
		t.Fatalf("zero options error = %v", err)
	}
	if _, err := compose.Merge(context.Background(), []openapi.Document{document, otherVersion}, compose.DefaultMergeOptions()); !errors.Is(err, compose.ErrVersionMismatch) {
		t.Fatalf("version mismatch error = %v", err)
	}

	options := compose.DefaultMergeOptions()
	options.MaxDocuments = -1
	if _, err := compose.Merge(context.Background(), []openapi.Document{document}, options); !errors.Is(err, compose.ErrInvalidOptions) {
		t.Fatalf("negative document limit error = %v", err)
	}
	options = compose.DefaultMergeOptions()
	options.MaxEntries = -1
	if _, err := compose.Merge(context.Background(), []openapi.Document{document}, options); !errors.Is(err, compose.ErrInvalidOptions) {
		t.Fatalf("negative entry limit error = %v", err)
	}
	options = compose.DefaultMergeOptions()
	options.MaxDepth = -1
	if _, err := compose.Merge(context.Background(), []openapi.Document{document}, options); !errors.Is(err, compose.ErrInvalidOptions) {
		t.Fatalf("negative depth limit error = %v", err)
	}
	options = compose.DefaultMergeOptions()
	options.MaxValueNodes = -1
	if _, err := compose.Merge(context.Background(), []openapi.Document{document}, options); !errors.Is(err, compose.ErrInvalidOptions) {
		t.Fatalf("negative value-node limit error = %v", err)
	}
	options = compose.DefaultMergeOptions()
	options.MaxDocuments = 1
	if _, err := compose.Merge(context.Background(), []openapi.Document{document, document}, options); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Fatalf("document limit error = %v", err)
	}
	options = compose.DefaultMergeOptions()
	options.MaxEntries = 1
	withEntries := composeDocument(t, `{"openapi":"3.2.0","paths":{"/a":{},"/b":{}}}`)
	if _, err := compose.Merge(context.Background(), []openapi.Document{document, withEntries}, options); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Fatalf("entry limit error = %v", err)
	}
	options = compose.DefaultMergeOptions()
	options.MaxEntries = 1
	firstLimited := composeDocument(t, `{"openapi":"3.2.0","paths":{},"x-owner":"first"}`)
	secondLimited := composeDocument(t, `{"openapi":"3.2.0","paths":{},"x-new":true,"x-owner":"second"}`)
	if _, err := compose.Merge(context.Background(), []openapi.Document{firstLimited, secondLimited}, options); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Fatalf("conflict entry limit error = %v", err)
	}
	deepFirst := composeDocument(t, `{"openapi":"3.2.0","paths":{},"x-value":[[true]]}`)
	deepSecond := composeDocument(t, `{"openapi":"3.2.0","paths":{},"x-value":[[true]]}`)
	options = compose.DefaultMergeOptions()
	options.MaxDepth = 1
	if _, err := compose.Merge(context.Background(), []openapi.Document{deepFirst, deepSecond}, options); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Fatalf("semantic depth limit error = %v", err)
	}
	options = compose.DefaultMergeOptions()
	options.MaxValueNodes = 2
	if _, err := compose.Merge(context.Background(), []openapi.Document{deepFirst, deepSecond}, options); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Fatalf("semantic node limit error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := compose.Merge(canceled, []openapi.Document{document}, compose.DefaultMergeOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}

	injected := errors.New("resolver failed")
	options = compose.DefaultMergeOptions()
	options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		return compose.RejectConflict, injected
	}
	conflicting := composeDocument(t, `{"openapi":"3.2.0","paths":{},"x-owner":"other"}`)
	owned := composeDocument(t, `{"openapi":"3.2.0","paths":{},"x-owner":"first"}`)
	if _, err := compose.Merge(context.Background(), []openapi.Document{owned, conflicting}, options); !errors.Is(err, injected) {
		t.Fatalf("resolver error = %v", err)
	}

	options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		return compose.ConflictDecision(255), nil
	}
	if _, err := compose.Merge(context.Background(), []openapi.Document{owned, conflicting}, options); !errors.Is(err, compose.ErrInvalidOptions) {
		t.Fatalf("invalid decision error = %v", err)
	}
}

func TestMergeHandlesDialectRegistriesAndMalformedRegistryValues(t *testing.T) {
	t.Parallel()

	t.Run("OpenAPI 3.1 webhooks", func(t *testing.T) {
		first := composeDocument(t, `{"openapi":"3.1.2","paths":{},"webhooks":{"one":{}}}`)
		second := composeDocument(t, `{"openapi":"3.1.2","paths":{},"webhooks":{"two":{}}}`)
		result, err := compose.Merge(context.Background(), []openapi.Document{first, second}, compose.DefaultMergeOptions())
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(result.Document().Raw())
		if !jsonContains(string(encoded), `"webhooks":{"one":{},"two":{}}`) {
			t.Fatalf("merged document = %s", encoded)
		}
	})

	t.Run("Swagger registries", func(t *testing.T) {
		for _, registry := range []string{"definitions", "parameters", "responses", "securityDefinitions"} {
			registry := registry
			t.Run(registry, func(t *testing.T) {
				first := composeDocument(t, `{"swagger":"2.0","paths":{},"`+registry+`":{"one":{}}}`)
				second := composeDocument(t, `{"swagger":"2.0","paths":{},"`+registry+`":{"two":{}}}`)
				if _, err := compose.Merge(context.Background(), []openapi.Document{first, second}, compose.DefaultMergeOptions()); err != nil {
					t.Fatal(err)
				}
			})
		}
	})

	t.Run("Swagger non-registry conflict", func(t *testing.T) {
		first := composeDocument(t, `{"swagger":"2.0","paths":{},"x-owner":"one"}`)
		second := composeDocument(t, `{"swagger":"2.0","paths":{},"x-owner":"two"}`)
		options := compose.DefaultMergeOptions()
		options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
			return compose.KeepExisting, nil
		}
		if _, err := compose.Merge(context.Background(), []openapi.Document{first, second}, options); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("component extension conflict", func(t *testing.T) {
		first := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"x-owner":"one"}}`)
		second := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"x-owner":"two"}}`)
		options := compose.DefaultMergeOptions()
		options.ResolveConflict = func(conflict compose.Conflict) (compose.ConflictDecision, error) {
			if conflict.Pointer() != "/components/x-owner" {
				t.Fatalf("pointer = %q", conflict.Pointer())
			}
			return compose.KeepExisting, nil
		}
		if _, err := compose.Merge(context.Background(), []openapi.Document{first, second}, options); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("non-object paths", func(t *testing.T) {
		first := composeDocument(t, `{"openapi":"3.2.0","paths":false}`)
		second := composeDocument(t, `{"openapi":"3.2.0","paths":true}`)
		options := compose.DefaultMergeOptions()
		options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
			return compose.UseIncoming, nil
		}
		if _, err := compose.Merge(context.Background(), []openapi.Document{first, second}, options); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMergeObservesCancellationDuringAndAfterPolicy(t *testing.T) {
	t.Parallel()

	first := composeDocument(t, `{"openapi":"3.2.0","paths":{},"x-a":1,"x-b":1}`)
	second := composeDocument(t, `{"openapi":"3.2.0","paths":{},"x-a":2,"x-b":2}`)
	ctx, cancel := context.WithCancel(context.Background())
	options := compose.DefaultMergeOptions()
	options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		cancel()
		return compose.UseIncoming, nil
	}
	if _, err := compose.Merge(ctx, []openapi.Document{first, second}, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}

	finalFirst := composeDocument(t, `{"openapi":"3.2.0","paths":{},"x-a":1}`)
	finalSecond := composeDocument(t, `{"openapi":"3.2.0","paths":{},"x-a":2}`)
	finalCtx, finalCancel := context.WithCancel(context.Background())
	options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		finalCancel()
		return compose.UseIncoming, nil
	}
	if _, err := compose.Merge(finalCtx, []openapi.Document{finalFirst, finalSecond}, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("final cancellation error = %v", err)
	}
}

func TestMergeRenamesComponentsAndRewritesInternalReferences(t *testing.T) {
	t.Parallel()

	first := composeDocument(t, `{
		"openapi":"3.2.0","paths":{},
		"components":{"schemas":{"Pet":{"type":"string"}}}
	}`)
	second := composeDocument(t, `{
		"openapi":"3.2.0",
		"x-list":[{"$ref":"#/components/schemas/Pet"},{"$ref":42}],
		"paths":{"/pets":{"get":{"responses":{"200":{
			"description":"ok",
			"content":{"application/json":{"schema":{
				"$ref":"#/components/schemas/Pet/properties/id"
			}}}
		}}}}},
		"components":{"schemas":{
			"Pet":{"type":"object","properties":{"id":{"type":"integer"}}},
			"Alias":{"$ref":"#/components/schemas/Pet"},
			"External":{"$ref":"https://example.test/openapi.json#/components/schemas/Pet"}
		}}
	}`)
	calls := 0
	options := compose.DefaultMergeOptions()
	options.ResolveConflict = func(conflict compose.Conflict) (compose.ConflictDecision, error) {
		calls++
		if conflict.Pointer() != "/components/schemas/Pet" {
			t.Fatalf("conflict pointer = %q", conflict.Pointer())
		}
		return compose.RenameIncoming, nil
	}
	result, err := compose.Merge(
		context.Background(), []openapi.Document{first, second}, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("conflict resolver calls = %d, want 1", calls)
	}
	encoded, _ := json.Marshal(result.Document().Raw())
	wantFragments := []string{
		`"Pet":{"type":"string"}`,
		`"Pet_2":{"type":"object"`,
		`"$ref":"#/components/schemas/Pet_2/properties/id"`,
		`"Alias":{"$ref":"#/components/schemas/Pet_2"}`,
		`"x-list":[{"$ref":"#/components/schemas/Pet_2"},{"$ref":42}]`,
		`"$ref":"https://example.test/openapi.json#/components/schemas/Pet"`,
	}
	for _, fragment := range wantFragments {
		if !jsonContains(string(encoded), fragment) {
			t.Fatalf("merged document = %s, want %s", encoded, fragment)
		}
	}
	foundRename := false
	for _, contribution := range result.Contributions() {
		if contribution.TargetPointer() == "/components/schemas/Pet_2" {
			foundRename = true
			if contribution.SourcePointer() != "/components/schemas/Pet" {
				t.Fatalf("rename source = %q", contribution.SourcePointer())
			}
		}
	}
	if !foundRename {
		t.Fatalf("contributions = %#v", result.Contributions())
	}
	original, _ := json.Marshal(second.Raw())
	if jsonContains(string(original), "Pet_2") {
		t.Fatal("merge mutated the incoming document")
	}
}

func TestMergeUsesExplicitComponentRenamePolicy(t *testing.T) {
	t.Parallel()

	first := composeDocument(t, `{"openapi":"3.1.2","paths":{},"components":{"schemas":{"Pet":{"type":"string"}}}}`)
	second := composeDocument(t, `{"openapi":"3.1.2","paths":{},"components":{"schemas":{"Pet":{"type":"integer"}}}}`)
	options := compose.DefaultMergeOptions()
	options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		return compose.RenameIncoming, nil
	}
	options.RenameComponent = func(rename compose.ComponentRename) (string, error) {
		if rename.Registry() != "schemas" || rename.OriginalName() != "Pet" ||
			rename.SuggestedName() != "Pet_2" {
			t.Fatalf("rename = %#v", rename)
		}
		if rename.Conflict().Pointer() != "/components/schemas/Pet" {
			t.Fatalf("rename conflict = %#v", rename.Conflict())
		}
		return "ImportedPet", nil
	}
	result, err := compose.Merge(
		context.Background(), []openapi.Document{first, second}, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result.Document().Raw())
	if !jsonContains(string(encoded), `"ImportedPet":{"type":"integer"}`) {
		t.Fatalf("merged document = %s", encoded)
	}
}

func TestMergeRejectsUnsafeComponentRenames(t *testing.T) {
	t.Parallel()

	first := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{},"Taken":{}}}}`)
	second := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"string"}}}}`)
	tests := []struct {
		name   string
		rename func(compose.ComponentRename) (string, error)
		want   error
	}{
		{name: "invalid name", rename: func(compose.ComponentRename) (string, error) { return "bad/name", nil }, want: compose.ErrInvalidComponentName},
		{name: "existing name", rename: func(compose.ComponentRename) (string, error) { return "Taken", nil }, want: compose.ErrRenameConflict},
		{name: "oversized name", rename: func(compose.ComponentRename) (string, error) { return "LongName", nil }, want: compose.ErrLimitExceeded},
		{name: "renamer error", rename: func(compose.ComponentRename) (string, error) { return "", errors.New("rename failed") }, want: errors.New("rename failed")},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			options := compose.DefaultMergeOptions()
			options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
				return compose.RenameIncoming, nil
			}
			options.RenameComponent = test.rename
			if test.name == "oversized name" {
				options.MaxComponentNameBytes = 3
			}
			_, err := compose.Merge(context.Background(), []openapi.Document{first, second}, options)
			if test.name == "renamer error" {
				if err == nil || err.Error() != test.want.Error() {
					t.Fatalf("Merge() error = %v, want %v", err, test.want)
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("Merge() error = %v, want %v", err, test.want)
			}
		})
	}

	exact := compose.DefaultMergeOptions()
	exact.MaxComponentNameBytes = len("Pet_2")
	exact.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		return compose.RenameIncoming, nil
	}
	if _, err := compose.Merge(
		context.Background(), []openapi.Document{first, second}, exact,
	); err != nil {
		t.Fatalf("exact component name limit error = %v", err)
	}

	options := compose.DefaultMergeOptions()
	options.MaxComponentNameBytes = -1
	if _, err := compose.Merge(context.Background(), []openapi.Document{first}, options); !errors.Is(err, compose.ErrInvalidOptions) {
		t.Fatalf("negative component-name limit error = %v", err)
	}

	swaggerFirst := composeDocument(t, `{"swagger":"2.0","paths":{},"definitions":{"Pet":{"type":"string"}}}`)
	swaggerSecond := composeDocument(t, `{"swagger":"2.0","paths":{},"definitions":{"Pet":{"type":"integer"}}}`)
	options = compose.DefaultMergeOptions()
	options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		return compose.RenameIncoming, nil
	}
	if _, err := compose.Merge(context.Background(), []openapi.Document{swaggerFirst, swaggerSecond}, options); !errors.Is(err, compose.ErrInvalidOptions) {
		t.Fatalf("non-component rename error = %v", err)
	}
}

func TestMergePreparesComponentConflictDecisionsOnce(t *testing.T) {
	t.Parallel()

	first := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"string"}}}}`)
	second := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"integer"}}}}`)
	for _, decision := range []compose.ConflictDecision{
		compose.RejectConflict,
		compose.KeepExisting,
		compose.UseIncoming,
	} {
		decision := decision
		t.Run(string(rune('0'+decision)), func(t *testing.T) {
			calls := 0
			options := compose.DefaultMergeOptions()
			options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
				calls++
				return decision, nil
			}
			_, err := compose.Merge(context.Background(), []openapi.Document{first, second}, options)
			if decision == compose.RejectConflict && !errors.Is(err, compose.ErrConflict) {
				t.Fatalf("reject error = %v", err)
			}
			if decision != compose.RejectConflict && err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("resolver calls = %d", calls)
			}
		})
	}

	options := compose.DefaultMergeOptions()
	options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		return compose.ConflictDecision(254), nil
	}
	if _, err := compose.Merge(context.Background(), []openapi.Document{first, second}, options); !errors.Is(err, compose.ErrInvalidOptions) {
		t.Fatalf("invalid prepared decision error = %v", err)
	}
}

func TestMergeAllocatesDistinctComponentNames(t *testing.T) {
	t.Parallel()

	first := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{},"Pet_2":{},"Order":{}}}}`)
	second := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"string"},"Order":{"type":"string"}}}}`)
	options := compose.DefaultMergeOptions()
	options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		return compose.RenameIncoming, nil
	}
	result, err := compose.Merge(context.Background(), []openapi.Document{first, second}, options)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result.Document().Raw())
	for _, name := range []string{`"Pet_3":{"type":"string"}`, `"Order_2":{"type":"string"}`} {
		if !jsonContains(string(encoded), name) {
			t.Fatalf("merged document = %s, want %s", encoded, name)
		}
	}
}

func TestMergeBoundsComponentReferenceRewriting(t *testing.T) {
	t.Parallel()

	first := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"string"}}}}`)
	second := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"integer"}}},"x-deep":[[true]]}`)
	base := compose.DefaultMergeOptions()
	base.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		return compose.RenameIncoming, nil
	}

	depth := base
	depth.MaxDepth = 2
	if _, err := compose.Merge(context.Background(), []openapi.Document{first, second}, depth); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Fatalf("rewrite depth error = %v", err)
	}
	nodes := base
	nodes.MaxValueNodes = 4
	if _, err := compose.Merge(context.Background(), []openapi.Document{first, second}, nodes); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Fatalf("rewrite node error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	canceled := base
	canceled.RenameComponent = func(rename compose.ComponentRename) (string, error) {
		cancel()
		return rename.SuggestedName(), nil
	}
	if _, err := compose.Merge(ctx, []openapi.Document{first, second}, canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("rewrite cancellation error = %v", err)
	}
}

func TestMergeBoundsComponentPreparationEntries(t *testing.T) {
	t.Parallel()

	empty := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{}}}`)
	member := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{}}}}`)
	options := compose.DefaultMergeOptions()
	options.MaxEntries = 1
	options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		return compose.KeepExisting, nil
	}
	if _, err := compose.Merge(context.Background(), []openapi.Document{empty, member}, options); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Fatalf("preparation member limit error = %v", err)
	}
	if _, err := compose.Merge(context.Background(), []openapi.Document{empty, empty, empty}, options); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Fatalf("preparation registry limit error = %v", err)
	}
}

func TestMergeHandlesComponentPreparationBoundaries(t *testing.T) {
	t.Parallel()

	keep := compose.DefaultMergeOptions()
	keep.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		return compose.KeepExisting, nil
	}
	tests := []struct {
		name   string
		first  string
		second string
	}{
		{
			name:   "non-object components",
			first:  `{"openapi":"3.2.0","paths":{},"components":false}`,
			second: `{"openapi":"3.2.0","paths":{},"components":true}`,
		},
		{
			name:   "registry absent from existing",
			first:  `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{}}}}`,
			second: `{"openapi":"3.2.0","paths":{},"components":{"responses":{"OK":{}}}}`,
		},
		{
			name:   "non-object registry",
			first:  `{"openapi":"3.2.0","paths":{},"components":{"schemas":false}}`,
			second: `{"openapi":"3.2.0","paths":{},"components":{"schemas":true}}`,
		},
		{
			name:   "equal component",
			first:  `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"string"}}}}`,
			second: `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"string"}}}}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			first := composeDocument(t, test.first)
			second := composeDocument(t, test.second)
			if _, err := compose.Merge(context.Background(), []openapi.Document{first, second}, keep); err != nil {
				t.Fatal(err)
			}
		})
	}

	first := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"object","properties":{"id":{"type":"string"}}}}}}`)
	second := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"object","properties":{"id":{"type":"integer"}}}}}}`)
	limited := compose.DefaultMergeOptions()
	limited.MaxValueNodes = 1
	limited.ResolveConflict = keep.ResolveConflict
	if _, err := compose.Merge(context.Background(), []openapi.Document{first, second}, limited); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Fatalf("preparation equality limit error = %v", err)
	}

	injected := errors.New("component resolver failed")
	failing := compose.DefaultMergeOptions()
	failing.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		return compose.RejectConflict, injected
	}
	if _, err := compose.Merge(context.Background(), []openapi.Document{first, second}, failing); !errors.Is(err, injected) {
		t.Fatalf("component resolver error = %v", err)
	}
}

func TestMergeRetainsPriorComponentOwnershipAfterContainerContribution(t *testing.T) {
	t.Parallel()

	first := composeDocument(t, `{"openapi":"3.2.0","paths":{}}`)
	second := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"string"}}}}`)
	third := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"integer"}}}}`)
	options := compose.DefaultMergeOptions()
	options.ResolveConflict = func(conflict compose.Conflict) (compose.ConflictDecision, error) {
		if conflict.ExistingDocumentIndex() != 1 {
			t.Fatalf("existing document = %d, want 1", conflict.ExistingDocumentIndex())
		}
		return compose.RenameIncoming, nil
	}
	if _, err := compose.Merge(context.Background(), []openapi.Document{first, second, third}, options); err != nil {
		t.Fatal(err)
	}
}

func TestMergeScopesRenamedSourcePointersToTheirDocument(t *testing.T) {
	t.Parallel()

	first := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"string"}}}}`)
	second := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet":{"type":"integer"}}}}`)
	third := composeDocument(t, `{"openapi":"3.2.0","paths":{},"components":{"schemas":{"Pet_2":{"type":"boolean"}}}}`)
	options := compose.DefaultMergeOptions()
	options.ResolveConflict = func(conflict compose.Conflict) (compose.ConflictDecision, error) {
		if conflict.IncomingDocumentIndex() == 1 {
			return compose.RenameIncoming, nil
		}
		return compose.UseIncoming, nil
	}
	result, err := compose.Merge(
		context.Background(), []openapi.Document{first, second, third}, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	contributions := result.Contributions()
	last := contributions[len(contributions)-1]
	if last.DocumentIndex() != 2 || last.SourcePointer() != "/components/schemas/Pet_2" ||
		last.TargetPointer() != "/components/schemas/Pet_2" {
		t.Fatalf("last contribution = %#v", last)
	}
}

func jsonContains(document string, fragment string) bool {
	for index := 0; index+len(fragment) <= len(document); index++ {
		if document[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
