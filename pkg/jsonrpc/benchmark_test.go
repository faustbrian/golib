package jsonrpc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func benchmarkDispatcher() *Dispatcher {
	registry := NewRegistry()
	_ = registry.Register("sum", func(_ context.Context, params json.RawMessage) (any, error) {
		var values []int
		if err := json.Unmarshal(params, &values); err != nil {
			return nil, InvalidParams()
		}
		return values[0] + values[1], nil
	})
	return NewDispatcher(registry)
}

func BenchmarkDispatchSingle(b *testing.B) {
	dispatcher := benchmarkDispatcher()
	payload := []byte(`{"jsonrpc":"2.0","method":"sum","params":[1,2],"id":1}`)
	b.ReportAllocs()
	for b.Loop() {
		dispatcher.Dispatch(context.Background(), payload)
	}
}

func BenchmarkDispatchBatch(b *testing.B) {
	dispatcher := benchmarkDispatcher()
	payload := []byte(`[
		{"jsonrpc":"2.0","method":"sum","params":[1,2],"id":1},
		{"jsonrpc":"2.0","method":"sum","params":[3,4],"id":2},
		{"jsonrpc":"2.0","method":"sum","params":[5,6]}
	]`)
	b.ReportAllocs()
	for b.Loop() {
		dispatcher.Dispatch(context.Background(), payload)
	}
}

func BenchmarkDispatchMaximumBatch(b *testing.B) {
	dispatcher := NewDispatcher(nil)
	payload := notificationBatch(defaultMaxBatchItems)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		dispatcher.Dispatch(context.Background(), payload)
	}
}

func BenchmarkDispatchRejectedBatch(b *testing.B) {
	dispatcher := NewDispatcher(nil)
	payload := notificationBatch(defaultMaxBatchItems + 1)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		dispatcher.Dispatch(context.Background(), payload)
	}
}

func BenchmarkDispatchMaximumPayload(b *testing.B) {
	dispatcher := NewDispatcher(nil)
	prefix := []byte(`{"jsonrpc":"2.0","method":"missing","id":1}`)
	payload := append(prefix, []byte(strings.Repeat(" ", int(defaultMaxDispatchBytes)-len(prefix)))...)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		dispatcher.Dispatch(context.Background(), payload)
	}
}

func BenchmarkDispatchRejectedPayload(b *testing.B) {
	dispatcher := NewDispatcher(nil)
	payload := make([]byte, defaultMaxDispatchBytes+1)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		dispatcher.Dispatch(context.Background(), payload)
	}
}

func notificationBatch(size int) []byte {
	var batch strings.Builder
	batch.WriteByte('[')
	for index := range size {
		if index > 0 {
			batch.WriteByte(',')
		}
		batch.WriteString(`{"jsonrpc":"2.0","method":"missing"}`)
	}
	batch.WriteByte(']')
	return []byte(batch.String())
}

func BenchmarkIDHostileExponent(b *testing.B) {
	payload := []byte(`1e1000000`)
	b.ReportAllocs()
	for b.Loop() {
		var id ID
		if err := json.Unmarshal(payload, &id); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIDHostileExponentDigits(b *testing.B) {
	payload := []byte(`1e` + strings.Repeat("9", 64<<10))
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		var id ID
		if err := json.Unmarshal(payload, &id); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClientRejectedResponse(b *testing.B) {
	reply := make([]byte, defaultMaxClientResponseBytes+1)
	client := NewClient(TransportFunc(func(context.Context, []byte) ([]byte, error) {
		return reply, nil
	}))
	b.ReportAllocs()
	b.SetBytes(int64(len(reply)))
	for b.Loop() {
		_ = client.Call(context.Background(), "probe", nil, nil)
	}
}
