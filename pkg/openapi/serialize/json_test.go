package serialize_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/serialize"
)

func TestJSONPreservesOrderAndExactNumbers(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"x-number":-0.0e+2,
		"info":{"version":"1","title":"API"},
		"paths":{}
	}`)
	var output bytes.Buffer
	if err := serialize.JSON(
		context.Background(),
		&output,
		document,
		serialize.DefaultOptions(),
	); err != nil {
		t.Fatal(err)
	}
	if output.String() != `{"openapi":"3.2.0","x-number":-0.0e+2,"info":{"version":"1","title":"API"},"paths":{}}` {
		t.Fatalf("preserving JSON = %s", output.String())
	}
}

func TestJSONCanonicalizesObjectOrderRecursively(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"paths":{},
		"info":{"version":"1","title":"API"},
		"openapi":"3.2.0"
	}`)
	options := serialize.DefaultOptions()
	options.Mode = serialize.Canonical
	var output bytes.Buffer
	if err := serialize.JSON(context.Background(), &output, document, options); err != nil {
		t.Fatal(err)
	}
	if output.String() != `{"info":{"title":"API","version":"1"},"openapi":"3.2.0","paths":{}}` {
		t.Fatalf("canonical JSON = %s", output.String())
	}
}

func TestJSONEnforcesOutputLimitAndCancellation(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	options := serialize.DefaultOptions()
	options.MaxBytes = 8
	var output bytes.Buffer
	if err := serialize.JSON(
		context.Background(), &output, document, options,
	); !errors.Is(err, serialize.ErrLimitExceeded) {
		t.Fatalf("limit error = %v", err)
	}
	if output.Len() > options.MaxBytes {
		t.Fatalf("wrote %d bytes past limit %d", output.Len(), options.MaxBytes)
	}
	options = serialize.DefaultOptions()
	options.MaxNodes = 1
	if err := serialize.JSON(
		context.Background(), &bytes.Buffer{}, document, options,
	); !errors.Is(err, serialize.ErrLimitExceeded) {
		t.Fatalf("node limit error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serialize.JSON(ctx, &bytes.Buffer{}, document, serialize.DefaultOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestJSONRejectsInvalidInputsAndPropagatesWriterErrors(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if err := serialize.JSON(
		//lint:ignore SA1012 This assertion verifies the nil-context contract.
		nil, &bytes.Buffer{}, document, serialize.DefaultOptions(),
	); err == nil {
		t.Fatal("nil context was accepted")
	}
	if err := serialize.JSON(
		context.Background(), nil, document, serialize.DefaultOptions(),
	); err == nil {
		t.Fatal("nil writer was accepted")
	}
	if err := serialize.JSON(
		context.Background(), &bytes.Buffer{}, nil, serialize.DefaultOptions(),
	); err == nil {
		t.Fatal("nil source was accepted")
	}
	sentinel := errors.New("writer failed")
	if err := serialize.JSON(
		context.Background(),
		failingWriter{err: sentinel},
		document,
		serialize.DefaultOptions(),
	); !errors.Is(err, sentinel) {
		t.Fatalf("writer error = %v", err)
	}
}

type failingWriter struct {
	err error
}

func (writer failingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

func mustDocument(t *testing.T, raw string) openapi.Document {
	t.Helper()
	document, err := openapi.ParseJSON(
		context.Background(),
		strings.NewReader(raw),
		parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return document
}
