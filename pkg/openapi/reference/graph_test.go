package reference_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestResolveAllPreservesOccurrenceAndChainOrder(t *testing.T) {
	t.Parallel()

	root := mustObject(t, []jsonvalue.Member{
		{Name: "schemas", Value: mustObject(t, []jsonvalue.Member{
			{Name: "A", Value: mustObject(t, []jsonvalue.Member{
				{Name: "$ref", Value: mustString(t, "#/schemas/B")},
			})},
			{Name: "B", Value: mustObject(t, nil)},
		})},
	})
	resolutions, err := reference.ResolveAll(
		context.Background(),
		reference.Resource{Root: root},
		nil,
		reference.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolutions) != 1 ||
		resolutions[0].Occurrence().Pointer().String() != "/schemas/A/$ref" ||
		resolutions[0].Occurrence().Raw() != "#/schemas/B" ||
		len(resolutions[0].Chain().Targets()) != 1 {
		t.Fatalf("resolutions = %#v", resolutions)
	}
}

func TestResolveAllReportsSourceOfDisabledExternalReference(t *testing.T) {
	t.Parallel()

	root := mustObject(t, []jsonvalue.Member{
		{Name: "external", Value: mustObject(t, []jsonvalue.Member{
			{Name: "$ref", Value: mustString(t, "other.json")},
		})},
	})
	_, err := reference.ResolveAll(
		context.Background(),
		reference.Resource{
			RetrievalURI: "https://api.example.test/root.json",
			Root:         root,
		},
		nil,
		reference.DefaultLimits(),
	)
	if !errors.Is(err, reference.ErrExternalResolutionDisabled) {
		t.Fatalf("external resolution error = %v", err)
	}
	if _, err := reference.ResolveAll(
		context.Background(), reference.Resource{}, nil,
		reference.DefaultLimits(),
	); err == nil {
		t.Fatal("invalid graph resource was accepted")
	}
}
