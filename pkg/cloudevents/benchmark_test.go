package cloudevents

import (
	"encoding/json"
	"testing"

	sdkevent "github.com/cloudevents/sdk-go/v2/event"
)

func BenchmarkJSONEquivalentEvent(b *testing.B) {
	payload := map[string]any{"order": "A-123", "amount": 42}
	data, err := json.Marshal(payload)
	if err != nil {
		b.Fatal(err)
	}
	golibData, err := NewJSONData(data)
	if err != nil {
		b.Fatal(err)
	}
	golibEvent, err := NewEvent(Attributes{
		ID: "1", Source: "/orders", Type: "com.example.order", DataContentType: "application/json",
	}, golibData)
	if err != nil {
		b.Fatal(err)
	}
	sdkEvent := sdkevent.New()
	sdkEvent.SetID("1")
	sdkEvent.SetSource("/orders")
	sdkEvent.SetType("com.example.order")
	if err := sdkEvent.SetData("application/json", payload); err != nil {
		b.Fatal(err)
	}
	golibEncoded, err := EncodeJSON(golibEvent)
	if err != nil {
		b.Fatal(err)
	}
	sdkEncoded, err := json.Marshal(sdkEvent)
	if err != nil {
		b.Fatal(err)
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
			if _, err := DecodeJSON(golibEncoded, DefaultLimits()); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("decode/sdk-go-v2.16.2", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var event sdkevent.Event
			if err := json.Unmarshal(sdkEncoded, &event); err != nil {
				b.Fatal(err)
			}
		}
	})
}
