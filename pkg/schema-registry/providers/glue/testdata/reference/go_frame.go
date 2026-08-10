package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryglue "github.com/faustbrian/golib/pkg/schema-registry/providers/glue"
)

func main() {
	if len(os.Args) != 3 {
		panic("expected schema UUID and payload hex")
	}
	payload, err := hex.DecodeString(os.Args[2])
	if err != nil {
		panic(err)
	}
	framer, err := registryglue.NewUncompressedFramer("reference", len(payload))
	if err != nil {
		panic(err)
	}
	frame, err := framer.Frame(context.Background(), schemaregistry.ProviderID{
		Provider: registryglue.ProviderName,
		Scope:    "reference",
		Value:    os.Args[1],
	}, payload)
	if err != nil {
		panic(err)
	}
	fmt.Print(hex.EncodeToString(frame))
}
