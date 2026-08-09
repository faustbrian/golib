package golib_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/cloudevents"
	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/tenancy"
)

func FuzzDecodeKafka(f *testing.F) {
	f.Add([]byte("body"), "content-type", []byte(cloudevents.JSONMediaType))
	f.Add([]byte("{}"), "ce_specversion", []byte("1.0"))
	f.Fuzz(func(t *testing.T, value []byte, headerKey string, headerValue []byte) {
		if len(value) > 1<<20 || len(headerKey) > 256 || len(headerValue) > 8192 {
			t.Skip()
		}
		record := kafka.ConsumedRecord{
			Topic: "events", Value: append([]byte(nil), value...),
			Headers: []kafka.Header{{Key: headerKey, Value: append([]byte(nil), headerValue...)}},
		}
		message, _, err := golib.DecodeKafka(record, cloudevents.DefaultLimits())
		if err != nil {
			return
		}
		if err := message.Event.Validate(); err != nil {
			t.Fatalf("successful Kafka decode produced invalid event: %v", err)
		}
		before := message.Event.Data().Bytes()
		if len(record.Value) > 0 {
			record.Value[0] ^= 0xff
		}
		if !bytes.Equal(before, message.Event.Data().Bytes()) {
			t.Fatal("decoded Kafka event aliases the input record")
		}
	})
}

func FuzzQueuePayloadRoundTrip(f *testing.F) {
	f.Add([]byte("body"), "tenant-a")
	f.Add([]byte{}, "")
	f.Add([]byte("body"), "bad?tenant")
	f.Fuzz(func(t *testing.T, payload []byte, tenantValue string) {
		if len(payload) > 1<<20 || len(tenantValue) > tenancy.MaxTenantIDBytes+1 {
			t.Skip()
		}
		message := job.Message{Timeout: time.Second, Body: append([]byte(nil), payload...)}
		if tenantValue != "" {
			message.Metadata = &job.Metadata{TenantID: tenantValue}
		}
		event, state, _, err := golib.QueueToCloudEvent(message, golib.QueueOptions{
			Source: "/queue", StableID: "job-1", Type: "example.job",
		})
		if _, tenantErr := tenancy.ParseTenantID(tenantValue); tenantValue != "" && tenantErr != nil {
			if !errors.Is(err, golib.ErrInvalidAdapterInput) || !errors.Is(err, tenancy.ErrInvalidTenantID) {
				t.Fatalf("malformed tenant error = %v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("queue to CloudEvent: %v", err)
		}
		roundTrip, _, err := golib.CloudEventToQueue(event, state)
		if err != nil {
			t.Fatalf("CloudEvent to queue: %v", err)
		}
		if !bytes.Equal(roundTrip.Body, payload) {
			t.Fatal("queue payload changed during round trip")
		}
	})
}
