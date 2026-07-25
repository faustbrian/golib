package kafka

import "testing"

func BenchmarkMessageValidation(b *testing.B) {
	message := Message{
		Topic: "track.tracking-event.v1",
		Key:   []byte("tracked-item-1"),
		Value: []byte(`{"event_id":"event-1","schema_version":1}`),
		Headers: []Header{
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "schema-version", Value: []byte("1")},
		},
	}
	limits := DefaultMessageLimits()

	b.ReportAllocs()
	for b.Loop() {
		if err := message.validate(limits); err != nil {
			b.Fatal(err)
		}
	}
}
