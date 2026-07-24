package apihttp

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func BenchmarkFailurePageTwoHundredRecords(b *testing.B) {
	observedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	items := make([]queue.JobRecord, queue.MaxPageSize)
	for index := range items {
		items[index] = queue.JobRecord{
			Kind: queue.RecordFailure, ID: fmt.Sprintf("failure-%03d", index),
			Backend: "valkey-streams", Queue: "critical", OccurredAt: observedAt,
			Attempts: 3, FailureCode: "handler_failed",
			Payload: queue.Payload{
				Visibility: queue.PayloadHidden, ContentType: "application/json", Size: 128,
			},
		}
	}
	page := queue.RecordPage{Items: items, NextCursor: "next"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if converted := recordPage(page); len(converted.Records) != int(queue.MaxPageSize) {
			b.Fatalf("recordPage() records = %d", len(converted.Records))
		}
	}
}

func BenchmarkPrivilegedPayloadOneMebibyte(b *testing.B) {
	payload := make([]byte, queue.MaxAdministrativePayloadBytes)
	for index := range payload {
		payload[index] = 'x'
	}
	record := queue.JobRecord{
		Kind: queue.RecordDeadLetter, ID: "dead-letter-1", Backend: "valkey-streams",
		Queue: "critical", OccurredAt: time.Unix(1, 0), Attempts: 3,
		FailureCode: "handler_failed", EnvelopeVersion: queue.CurrentEnvelopeVersion,
		Payload: queue.Payload{
			Visibility: queue.PayloadRevealed, ContentType: "application/octet-stream",
			Size: int64(len(payload)), Data: payload,
		},
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for b.Loop() {
		encoded, err := json.Marshal(recordModel(record, queue.PayloadRevealed, false))
		if err != nil || len(encoded) > 2*queue.MaxAdministrativePayloadBytes {
			b.Fatalf("marshal payload = %d bytes, error %v", len(encoded), err)
		}
	}
}
