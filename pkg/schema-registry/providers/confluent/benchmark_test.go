package confluent_test

import (
	"context"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	"github.com/faustbrian/golib/pkg/schema-registry/providers/confluent"
	"github.com/twmb/franz-go/pkg/sr"
)

func BenchmarkClassicFrame(b *testing.B) {
	framer, err := confluent.NewClassicFramer("bench", 4096)
	if err != nil {
		b.Fatalf("NewClassicFramer() error = %v", err)
	}
	id := schemaregistry.ProviderID{Provider: confluent.ProviderName, Scope: "bench", Value: "1"}
	payload := make([]byte, 1024)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := framer.Frame(context.Background(), id, payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFranzGoClassicFrame(b *testing.B) {
	payload := make([]byte, 1024)
	var header sr.ConfluentHeader
	b.ReportAllocs()
	for b.Loop() {
		frame, err := header.AppendEncode(nil, 1, nil)
		if err != nil {
			b.Fatal(err)
		}
		frame = append(frame, payload...)
		if len(frame) != len(payload)+5 {
			b.Fatal("unexpected frame size")
		}
	}
}

func BenchmarkProtobufFrame(b *testing.B) {
	payload := make([]byte, 1024)
	framer, err := confluent.NewProtobufFramer("bench", len(payload), 1)
	if err != nil {
		b.Fatal(err)
	}
	id := schemaregistry.ProviderID{Provider: confluent.ProviderName, Scope: "bench", Value: "1"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := framer.FrameMessage(context.Background(), id, []int{0}, payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFranzGoProtobufFrame(b *testing.B) {
	payload := make([]byte, 1024)
	var header sr.ConfluentHeader
	b.ReportAllocs()
	for b.Loop() {
		frame, err := header.AppendEncode(nil, 1, []int{0})
		if err != nil {
			b.Fatal(err)
		}
		frame = append(frame, payload...)
		if len(frame) != len(payload)+6 {
			b.Fatal("unexpected frame size")
		}
	}
}
