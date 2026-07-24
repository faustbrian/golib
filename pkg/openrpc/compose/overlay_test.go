package compose_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/compose"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestApplyOverlaysUsesOrderedRFC7396Semantics(t *testing.T) {
	t.Parallel()

	first, err := compose.NewOverlay([]byte(`{
		"info":{"title":"First"},
		"methods":[{"name":"after","params":[]}],
		"x-overlay":{"enabled":true}
	}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	second, err := compose.NewOverlay(
		[]byte(`{"info":{"title":"Final"}}`), jsonvalue.DefaultPolicy(),
	)
	if err != nil {
		t.Fatal(err)
	}

	document, err := compose.ApplyOverlays(
		context.Background(), testDocument(t, "before"),
		[]compose.Overlay{first, second}, compose.DefaultOverlayOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if document.Info().Title() != "Final" {
		t.Fatalf("title = %q", document.Info().Title())
	}
	method, inline := document.Methods()[0].Method()
	if !inline || method.Name() != "after" {
		t.Fatalf("methods = %#v", document.Methods())
	}
	if document.Extensions().Len() != 1 {
		t.Fatalf("extensions = %d", document.Extensions().Len())
	}
	bytes := first.Bytes()
	bytes[0] = '['
	if first.Bytes()[0] != '{' {
		t.Fatal("Overlay.Bytes exposed mutable storage")
	}
}

func TestApplyOverlaysRejectsInvalidResultsAndBounds(t *testing.T) {
	t.Parallel()

	removeMethods, err := compose.NewOverlay(
		[]byte(`{"methods":null}`), jsonvalue.DefaultPolicy(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compose.ApplyOverlays(
		context.Background(), testDocument(t), []compose.Overlay{removeMethods},
		compose.DefaultOverlayOptions(),
	); !errors.Is(err, compose.ErrOverlayResult) {
		t.Fatalf("result error = %v", err)
	}

	options := compose.DefaultOverlayOptions()
	options.MaxActions = 0
	if _, err := compose.ApplyOverlays(
		context.Background(), testDocument(t), nil, options,
	); !errors.Is(err, compose.ErrInvalidOverlay) {
		t.Fatalf("options error = %v", err)
	}

	options = compose.DefaultOverlayOptions()
	options.MaxOutputBytes = 1
	if _, err := compose.ApplyOverlays(
		context.Background(), testDocument(t), nil, options,
	); !errors.Is(err, compose.ErrOverlayLimit) {
		t.Fatalf("output limit error = %v", err)
	}
}

func TestNewOverlayRejectsNonObjectAndApplyHonorsCancellation(t *testing.T) {
	t.Parallel()

	if _, err := compose.NewOverlay([]byte(`[]`), jsonvalue.DefaultPolicy()); !errors.Is(err, compose.ErrInvalidOverlay) {
		t.Fatalf("NewOverlay error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := compose.ApplyOverlays(
		ctx, testDocument(t), nil, compose.DefaultOverlayOptions(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}
