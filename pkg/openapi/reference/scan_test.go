package reference_test

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestScanReturnsReferencesInSourceOrder(t *testing.T) {
	t.Parallel()

	root := mustObject(t, []jsonvalue.Member{
		{Name: "first", Value: mustObject(t, []jsonvalue.Member{
			{Name: "$ref", Value: mustString(t, "#/components/schemas/First")},
		})},
		{Name: "items", Value: mustArray(t, []jsonvalue.Value{
			mustString(t, "literal"),
			mustObject(t, []jsonvalue.Member{
				{Name: "$ref", Value: mustString(t, "other.json#Thing")},
			}),
		})},
	})
	occurrences, err := reference.Scan(
		context.Background(), root, reference.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(occurrences) != 2 {
		t.Fatalf("occurrences = %#v", occurrences)
	}
	if occurrences[0].Pointer().String() != "/first/$ref" ||
		occurrences[0].Raw() != "#/components/schemas/First" ||
		occurrences[1].Pointer().String() != "/items/1/$ref" ||
		occurrences[1].Raw() != "other.json#Thing" {
		t.Fatalf("occurrences = %#v", occurrences)
	}
}

func TestScanRejectsMalformedReferenceAndBoundsTraversal(t *testing.T) {
	t.Parallel()

	invalid := mustObject(t, []jsonvalue.Member{
		{Name: "$ref", Value: jsonvalue.Boolean(true)},
	})
	if _, err := reference.Scan(
		context.Background(), invalid, reference.DefaultLimits(),
	); !errors.Is(err, reference.ErrInvalidReference) {
		t.Fatalf("invalid reference error = %v", err)
	}

	limits := reference.DefaultLimits()
	limits.MaxTraversalNodes = 1
	nested := mustObject(t, []jsonvalue.Member{
		{Name: "nested", Value: mustObject(t, nil)},
	})
	if _, err := reference.Scan(
		context.Background(), nested, limits,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("scan limit error = %v", err)
	}
	limits = reference.DefaultLimits()
	limits.MaxTraversalDepth = 1
	withReference := mustObject(t, []jsonvalue.Member{
		{Name: "nested", Value: mustObject(t, []jsonvalue.Member{
			{Name: "$ref", Value: mustString(t, "#")},
		})},
	})
	if _, err := reference.Scan(
		context.Background(), withReference, limits,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("scan depth error = %v", err)
	}
}

func TestScanAcceptsExactTraversalNodeAndDepthBounds(t *testing.T) {
	t.Parallel()

	limits := reference.DefaultLimits()
	limits.MaxTraversalNodes = 1
	limits.MaxTraversalDepth = 1
	if occurrences, err := reference.Scan(
		context.Background(), jsonvalue.Null(), limits,
	); err != nil || len(occurrences) != 0 {
		t.Fatalf("exact root bounds = %#v, %v", occurrences, err)
	}

	limits.MaxTraversalNodes = 2
	root := mustArray(t, []jsonvalue.Value{jsonvalue.Null()})
	if occurrences, err := reference.Scan(
		context.Background(), root, limits,
	); err != nil || len(occurrences) != 0 {
		t.Fatalf("exact child bounds = %#v, %v", occurrences, err)
	}
	limits.MaxTraversalNodes = 3
	nested := mustArray(t, []jsonvalue.Value{root})
	if _, err := reference.Scan(
		context.Background(), nested, limits,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("array depth limit error = %v", err)
	}
}

func TestScanRejectsWideValuesBeforeCopyingChildren(t *testing.T) {
	elements := make([]jsonvalue.Value, 4096)
	for index := range elements {
		elements[index] = jsonvalue.Null()
	}
	root := mustArray(t, elements)
	limits := reference.DefaultLimits()
	limits.MaxTraversalNodes = 1
	const repetitions = 16
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	for range repetitions {
		if _, err := reference.Scan(
			context.Background(), root, limits,
		); !errors.Is(err, reference.ErrLimitExceeded) {
			t.Fatalf("wide scan error = %v", err)
		}
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if allocated := (after.TotalAlloc - before.TotalAlloc) / repetitions; allocated > 64<<10 {
		t.Fatalf("wide rejected scan allocated %d bytes per operation", allocated)
	}
}

func TestScanFilteredIgnoresUnselectedReferenceShapedData(t *testing.T) {
	t.Parallel()

	root := mustObject(t, []jsonvalue.Member{
		{Name: "data", Value: mustObject(t, []jsonvalue.Member{
			{Name: "$ref", Value: jsonvalue.Boolean(false)},
		})},
		{Name: "schema", Value: mustObject(t, []jsonvalue.Member{
			{Name: "$ref", Value: mustString(t, "#/Target")},
		})},
	})
	occurrences, err := reference.ScanFiltered(
		context.Background(), root, reference.DefaultLimits(),
		func(pointer reference.Pointer, _ jsonvalue.Value) bool {
			return pointer.String() == "/schema/$ref"
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(occurrences) != 1 || occurrences[0].Raw() != "#/Target" {
		t.Fatalf("occurrences = %#v", occurrences)
	}
	if _, err := reference.ScanFiltered(
		context.Background(), root, reference.DefaultLimits(), nil,
	); err == nil {
		t.Fatal("nil occurrence filter succeeded")
	}
}

func TestScanRejectsInvalidSetupAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	limits := reference.DefaultLimits()
	//lint:ignore SA1012 This assertion verifies the nil-context contract.
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := reference.Scan(nil, jsonvalue.Null(), limits); err == nil {
		t.Fatal("nil scan context was accepted")
	}
	limits.MaxReferenceDepth = 0
	if _, err := reference.Scan(
		context.Background(), jsonvalue.Null(), limits,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("invalid scan limits error = %v", err)
	}
	if _, err := reference.Scan(
		context.Background(), jsonvalue.Value{}, reference.DefaultLimits(),
	); err == nil {
		t.Fatal("invalid scan root was accepted")
	}
	ctx := &cancelDuringTraversal{Context: context.Background()}
	if _, err := reference.Scan(
		ctx,
		mustObject(t, []jsonvalue.Member{{Name: "child", Value: jsonvalue.Null()}}),
		reference.DefaultLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan cancellation error = %v", err)
	}
	occurrences, err := reference.Scan(
		context.Background(), jsonvalue.Null(), reference.DefaultLimits(),
	)
	if err != nil || len(occurrences) != 0 {
		t.Fatalf("scalar scan = %#v, %v", occurrences, err)
	}
}
