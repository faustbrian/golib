package cloudevents

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func FuzzDecodeJSON(f *testing.F) {
	f.Add([]byte(`{"specversion":"1.0","id":"1","source":"/source","type":"example","data":null}`))
	f.Add([]byte(`{"specversion":"1.0","id":"1","source":"/source","type":"example","data_base64":"AAE="}`))
	f.Add([]byte(`{"specversion":"1.0","id":"1","id":"2","source":"/source","type":"example"}`))
	limits := DefaultLimits()
	limits.MaxEventBytes = 64 << 10
	limits.MaxDataBytes = 64 << 10
	f.Fuzz(func(t *testing.T, value []byte) {
		if int64(len(value)) > limits.MaxEventBytes {
			t.Skip()
		}
		event, err := DecodeJSON(value, limits)
		if err != nil {
			return
		}
		encoded, err := EncodeJSON(event)
		if err != nil {
			t.Fatalf("EncodeJSON(decoded event) error = %v", err)
		}
		if _, err := DecodeJSON(encoded, limits); err != nil {
			t.Fatalf("DecodeJSON(re-encoded event) error = %v", err)
		}
	})
}

func FuzzDecodeJSONBatch(f *testing.F) {
	f.Add([]byte(`[]`))
	f.Add([]byte(`[
		{"specversion":"1.0","id":"1","source":"/source","type":"example","data":null},
		{"specversion":"1.0","id":"2","source":"/source","type":"example","data_base64":"AAE="}
	]`))
	f.Add([]byte(`[{"specversion":"1.0","id":"1","source":"/source","type":"example"},null]`))
	limits := DefaultLimits()
	limits.MaxEventBytes = 64 << 10
	limits.MaxDataBytes = 64 << 10
	f.Fuzz(func(t *testing.T, value []byte) {
		if int64(len(value)) > limits.MaxEventBytes {
			t.Skip()
		}
		events, err := DecodeJSONBatch(value, limits)
		if err != nil {
			return
		}
		var rawEvents []json.RawMessage
		if err := json.Unmarshal(value, &rawEvents); err != nil {
			t.Fatalf("successful batch decode rejected by encoding/json: %v", err)
		}
		if len(events) != len(rawEvents) {
			t.Fatalf("decoded batch event count = %d, want %d", len(events), len(rawEvents))
		}
		encoded, err := EncodeJSONBatch(events)
		if err != nil {
			t.Fatalf("EncodeJSONBatch(decoded batch) error = %v", err)
		}
		roundTrip, err := DecodeJSONBatch(encoded, limits)
		if err != nil {
			t.Fatalf("DecodeJSONBatch(re-encoded batch) error = %v", err)
		}
		if len(roundTrip) != len(events) {
			t.Fatalf("re-encoded batch event count = %d, want %d", len(roundTrip), len(events))
		}
	})
}

func FuzzDecodeHTTP(f *testing.F) {
	f.Add("application/json", "1.0", "1", "/source", "example", []byte(`{"value":true}`))
	f.Add(JSONMediaType, "", "", "", "", []byte(`{"specversion":"1.0","id":"1","source":"/source","type":"example"}`))
	limits := DefaultLimits()
	limits.MaxEventBytes = 64 << 10
	limits.MaxDataBytes = 64 << 10
	f.Fuzz(func(t *testing.T, contentType, spec, id, source, eventType string, body []byte) {
		if len(contentType)+len(spec)+len(id)+len(source)+len(eventType)+len(body) > 64<<10 {
			t.Skip()
		}
		header := make(http.Header)
		if contentType != "" {
			header.Set("Content-Type", contentType)
		}
		if spec != "" {
			header.Set("Ce-Specversion", spec)
		}
		if id != "" {
			header.Set("Ce-Id", id)
		}
		if source != "" {
			header.Set("Ce-Source", source)
		}
		if eventType != "" {
			header.Set("Ce-Type", eventType)
		}
		_, _ = DecodeHTTP(context.Background(), header, bytes.NewReader(body), limits)
	})
}

func FuzzDecodeKafka(f *testing.F) {
	f.Add("application/json", "1.0", "1", "/source", "example", []byte(`{"value":true}`), true)
	limits := DefaultLimits()
	limits.MaxEventBytes = 64 << 10
	limits.MaxDataBytes = 64 << 10
	f.Fuzz(func(t *testing.T, contentType, spec, id, source, eventType string, value []byte, present bool) {
		if len(contentType)+len(spec)+len(id)+len(source)+len(eventType)+len(value) > 64<<10 {
			t.Skip()
		}
		headers := []KafkaHeader{
			{Key: "ce_specversion", Value: []byte(spec)},
			{Key: "ce_id", Value: []byte(id)},
			{Key: "ce_source", Value: []byte(source)},
			{Key: "ce_type", Value: []byte(eventType)},
		}
		if contentType != "" {
			headers = append(headers, KafkaHeader{Key: "content-type", Value: []byte(contentType)})
		}
		if !present {
			value = nil
		}
		_, _ = DecodeKafka(KafkaRecord{Value: value, Headers: headers}, limits)
	})
}
