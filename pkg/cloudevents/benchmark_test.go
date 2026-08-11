package cloudevents

import (
	"bytes"
	"encoding/json"
	"testing"

	sdkevent "github.com/cloudevents/sdk-go/v2/event"
)

func BenchmarkJSONEquivalentEvent(b *testing.B) {
	canonical := []byte(`{"specversion":"1.0","id":"1","source":"/orders","type":"com.example.order","datacontenttype":"application/json","data":{"amount":42,"order":"A-123"}}`)
	golibEvent, err := DecodeJSON(canonical, DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	var sdkEvent sdkevent.Event
	if err := json.Unmarshal(canonical, &sdkEvent); err != nil {
		b.Fatal(err)
	}
	if err := sdkEvent.Validate(); err != nil {
		b.Fatalf("SDK rejected shared canonical corpus: %v", err)
	}
	golibContentType, present := golibEvent.DataContentType()
	if golibEvent.SpecVersion() != sdkEvent.SpecVersion() || golibEvent.ID() != sdkEvent.ID() ||
		golibEvent.Source() != sdkEvent.Source() || golibEvent.Type() != sdkEvent.Type() ||
		!present || golibContentType != sdkEvent.DataContentType() ||
		!bytes.Equal(golibEvent.Data().Bytes(), sdkEvent.Data()) {
		b.Fatalf("implementations decoded different events from the shared canonical corpus")
	}
	b.Run("encode/golib", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := EncodeJSON(golibEvent); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("encode/sdk-go-v2.16.2", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := json.Marshal(sdkEvent); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("decode/golib", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := DecodeJSON(canonical, DefaultLimits()); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("decode/sdk-go-v2.16.2", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var event sdkevent.Event
			if err := json.Unmarshal(canonical, &event); err != nil {
				b.Fatal(err)
			}
		}
	})
}
