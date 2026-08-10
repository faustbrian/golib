package confluent_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/schema-registry/providers/confluent"
)

func FuzzWireFrames(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 1, 0})
	f.Add([]byte{1})
	classic, err := confluent.NewClassicFramer("fuzz", 4096)
	if err != nil {
		f.Fatalf("NewClassicFramer() error = %v", err)
	}
	protobuf, err := confluent.NewProtobufFramer("fuzz", 4096, 32)
	if err != nil {
		f.Fatalf("NewProtobufFramer() error = %v", err)
	}
	f.Fuzz(func(t *testing.T, framed []byte) {
		if len(framed) > 8192 {
			t.Skip()
		}
		_, _, _ = classic.Unframe(context.Background(), framed)
		_, _, _, _ = protobuf.UnframeMessage(context.Background(), framed)
	})
}
