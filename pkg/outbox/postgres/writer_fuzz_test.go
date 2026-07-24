package postgres

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
)

func FuzzWriterIdentifiers(f *testing.F) {
	f.Add("public", "outbox_messages", "message-id", "topic")
	f.Add(`odd"schema`, "table.name", "", "")

	f.Fuzz(func(t *testing.T, schema, table, id, topic string) {
		writer, err := NewWriter(WriterConfig{Schema: schema, Table: table})
		if err != nil {
			t.Fatalf("create writer: %v", err)
		}
		query, arguments := writer.insertQuery([]outbox.Envelope{{
			ID: id, Topic: topic, Payload: []byte{}, PayloadVersion: 1,
			AvailableAt: time.Unix(1, 0), CreatedAt: time.Unix(1, 0),
		}})
		if query == "" || len(arguments) != 10 {
			t.Fatalf("query/arguments = %q/%d", query, len(arguments))
		}
	})
}

func BenchmarkWriterInsertQuery100(b *testing.B) {
	b.ReportAllocs()
	writer, err := NewWriter(WriterConfig{})
	if err != nil {
		b.Fatalf("create writer: %v", err)
	}
	envelopes := make([]outbox.Envelope, 100)
	for index := range envelopes {
		envelopes[index] = outbox.Envelope{
			ID: "message", Topic: "topic", Payload: []byte("payload"), PayloadVersion: 1,
			Metadata:    map[string]string{"trace": "value"},
			AvailableAt: time.Unix(1, 0), CreatedAt: time.Unix(1, 0),
		}
	}

	for b.Loop() {
		_, _ = writer.insertQuery(envelopes)
	}
}
