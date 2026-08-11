//go:build ignore

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	"github.com/faustbrian/golib/pkg/schema-registry/providers/confluent"
)

func main() {
	if len(os.Args) != 3 {
		panic("expected schema ID and payload hex")
	}
	id, err := strconv.Atoi(os.Args[1])
	if err != nil {
		panic(err)
	}
	payload, err := hex.DecodeString(os.Args[2])
	if err != nil {
		panic(err)
	}
	providerID := schemaregistry.ProviderID{
		Provider: confluent.ProviderName,
		Scope:    "reference",
		Value:    strconv.Itoa(id),
	}
	classic, err := confluent.NewClassicFramer("reference", len(payload))
	if err != nil {
		panic(err)
	}
	classicFrame, err := classic.Frame(context.Background(), providerID, payload)
	if err != nil {
		panic(err)
	}
	protobuf, err := confluent.NewProtobufFramer("reference", len(payload), 1)
	if err != nil {
		panic(err)
	}
	protobufFrame, err := protobuf.FrameMessage(context.Background(), providerID, []int{0}, payload)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%x\n%x\n", classicFrame, protobufFrame)
}
