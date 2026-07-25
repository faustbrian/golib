package compose_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/compose"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
)

func TestRenameComponentsRewritesEveryMatchingReference(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Rename","version":"1"},
		"methods":[{
			"name":"read",
			"params":[{"$ref":"#/components/contentDescriptors/Input"}],
			"result":{"name":"result","schema":{"$ref":"#/components/schemas/Value"}}
		}],
		"components":{
			"schemas":{"Value":{"type":"string"}},
			"contentDescriptors":{"Input":{"name":"input","schema":{"$ref":"#/components/schemas/Value"}}}
		}
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := compose.RenameComponents(
		context.Background(), parsed.Document(),
		map[compose.ComponentKind]map[string]string{
			compose.SchemaComponents:            {"Value": "Payload"},
			compose.ContentDescriptorComponents: {"Input": "Request"},
		},
		compose.DefaultRenameOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := openrpc.MarshalCanonical(renamed)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range [][]byte{
		[]byte(`"Payload":{"type":"string"}`),
		[]byte(`"Request":{"name":"input"`),
		[]byte(`"$ref":"#/components/schemas/Payload"`),
		[]byte(`"$ref":"#/components/contentDescriptors/Request"`),
	} {
		if !bytes.Contains(encoded, expected) {
			t.Fatalf("output omitted %s: %s", expected, encoded)
		}
	}
}

func TestRenameComponentsRejectsCollisionsAndInvalidOptions(t *testing.T) {
	t.Parallel()

	document := testDocument(t)
	options := compose.DefaultRenameOptions()
	options.MaxRenames = 0
	if _, err := compose.RenameComponents(
		context.Background(), document, nil, options,
	); !errors.Is(err, compose.ErrInvalidRename) {
		t.Fatalf("options error = %v", err)
	}

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1","info":{"title":"Rename","version":"1"},"methods":[],
		"components":{"schemas":{"First":true,"Second":false}}
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	_, err = compose.RenameComponents(
		context.Background(), parsed.Document(),
		map[compose.ComponentKind]map[string]string{
			compose.SchemaComponents: {"First": "Second"},
		},
		compose.DefaultRenameOptions(),
	)
	if !errors.Is(err, compose.ErrRenameConflict) {
		t.Fatalf("collision error = %v", err)
	}
}
