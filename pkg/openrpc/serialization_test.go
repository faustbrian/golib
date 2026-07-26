package openrpc_test

import (
	"bytes"
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	"github.com/faustbrian/golib/pkg/openrpc/parse"
)

func TestMarshalCanonicalIsStableAcrossObjectOrderAndWhitespace(t *testing.T) {
	t.Parallel()

	inputs := [][]byte{
		[]byte(`{"methods":[],"info":{"version":"1","x-z":2,"title":"X"},"openrpc":"1.4.1","x-a":1}`),
		[]byte(` {
			"x-a": 1,
			"openrpc": "1.4.1",
			"info": {"title":"X", "version":"1", "x-z":2},
			"methods": []
		} `),
	}
	want := []byte(`{"info":{"title":"X","version":"1","x-z":2},"methods":[],"openrpc":"1.4.1","x-a":1}`)

	for _, input := range inputs {
		result, err := parse.Decode(input, parse.DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := openrpc.MarshalCanonical(result.Document())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(canonical, want) {
			t.Fatalf("MarshalCanonical() = %s, want %s", canonical, want)
		}
		if !bytes.Equal(result.PreservingJSON(), input) {
			t.Fatal("canonical serialization altered preserving source")
		}
	}
}

func TestMarshalCanonicalRejectsPreservedFieldCollision(t *testing.T) {
	t.Parallel()

	value, err := jsonvalue.Parse([]byte(`true`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := openrpc.NewUnknownFields(openrpc.Field{Name: "description", Value: value})
	if err != nil {
		t.Fatal(err)
	}
	description := "standard"
	info, err := openrpc.NewInfo(openrpc.InfoInput{
		Title:         "Collision",
		Version:       "1",
		Description:   &description,
		UnknownFields: unknown,
	})
	if err != nil {
		t.Fatal(err)
	}
	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version: version,
		Info:    &info,
		Methods: []openrpc.MethodOrReference{},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = openrpc.MarshalCanonical(document)
	if !errors.Is(err, openrpc.ErrFieldCollision) {
		t.Fatalf("error = %v, want ErrFieldCollision", err)
	}
}

func TestMarshalCanonicalRejectsInvalidZeroValues(t *testing.T) {
	t.Parallel()

	if _, err := openrpc.MarshalCanonical(openrpc.Document{}); !errors.Is(err, openrpc.ErrMissingRequiredField) {
		t.Fatalf("zero document error = %v", err)
	}

	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "Invalid union", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version: version,
		Info:    &info,
		Methods: []openrpc.MethodOrReference{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openrpc.MarshalCanonical(document); !errors.Is(err, openrpc.ErrInvalidUnion) {
		t.Fatalf("invalid union error = %v", err)
	}
}

func TestCanonicalDocumentParsesAgain(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Complete","version":"1"},
		"servers":[{"url":"https://example.com","variables":{"mode":{"default":"safe","enum":["safe"]}}}],
		"methods":[{
			"name":"sample",
			"params":[{"name":"value","schema":{"type":"integer"}}],
			"paramStructure":"by-name",
			"result":{"name":"result","schema":true},
			"servers":[],
			"tags":[{"name":"sample"}],
			"errors":[{"code":-1,"message":"failed","data":null}],
			"links":[{"name":"next","method":"next","params":{"value":"$params.value"}}],
			"examples":[{"name":"notice","params":[]}],
			"deprecated":false,
			"externalDocs":{"url":"https://example.com/method"}
		}],
		"components":{
			"schemas":{"Value":{"type":"integer"}},
			"links":{"Next":{"method":"next"}},
			"errors":{"Failure":{"code":-1,"message":"failed"}},
			"examples":{"Null":{"name":"null","value":null}},
			"examplePairings":{"Notice":{"name":"notice","params":[]}},
			"contentDescriptors":{"Value":{"name":"value","schema":false}},
			"tags":{"Sample":{"name":"sample"}}
		}
	}`)
	result, err := parse.Decode(input, parse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := openrpc.MarshalCanonical(result.Document())
	if err != nil {
		t.Fatal(err)
	}
	second, err := parse.Decode(canonical, parse.DefaultOptions())
	if err != nil {
		t.Fatalf("canonical document did not parse: %v\n%s", err, canonical)
	}
	again, err := openrpc.MarshalCanonical(second.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, canonical) {
		t.Fatalf("canonicalization is not idempotent:\n%s\n%s", canonical, again)
	}
}
