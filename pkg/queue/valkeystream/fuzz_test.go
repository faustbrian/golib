package valkeystream

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func FuzzDeliveryEnvelope(f *testing.F) {
	valid := job.NewMessage(rawMessage("payload"))
	f.Add(string(valid.Bytes()), int64(1), false)
	f.Add("not-json", int64(2), true)

	f.Fuzz(func(t *testing.T, body string, attempts int64, reclaimed bool) {
		opts := fuzzWorkerOptions(t)
		transport := newFakeTransport(streamqueue.Delivery{})
		worker := &Worker{opts: opts, transport: transport}
		message, _ := worker.decode(streamqueue.Delivery{
			ID: "1-0", Body: []byte(body), Attempts: attempts, Reclaimed: reclaimed,
		})
		if message != nil {
			_ = message.(*job.Message).Ack()
		}
	})
}

func FuzzOptions(f *testing.F) {
	f.Add("127.0.0.1:6379", "user", "secret", int64(time.Second), int64(16), int64(8))
	f.Add("", "", "", int64(0), int64(0), int64(0))

	f.Fuzz(func(
		t *testing.T,
		address, username, password string,
		timeout, readBatch, poolSize int64,
	) {
		configuredAddress := address
		if address != "" {
			configuredAddress = "fuzz-endpoint-" + hex.EncodeToString([]byte(address))
		}
		configuredPassword := password
		if password != "" {
			configuredPassword = "fuzz-secret-" + hex.EncodeToString([]byte(password))
		}
		_, err := newOptions(
			WithAddress(configuredAddress), WithAuthentication(username, configuredPassword),
			WithCommandTimeout(time.Duration(timeout)),
			WithReadBatchSize(int(readBatch)),
			WithBlockingPool(0, int(poolSize), time.Second),
		)
		if err == nil {
			return
		}
		text := err.Error()
		if configuredPassword != "" && strings.Contains(text, configuredPassword) {
			t.Fatal("configuration error exposed password")
		}
		if configuredAddress != "" && strings.Contains(text, configuredAddress) {
			t.Fatal("configuration error exposed endpoint")
		}
	})
}

func FuzzNativeResponses(f *testing.F) {
	f.Add(`[["1-0",["body","payload"]]]`)
	f.Add(`[{"name":"workers","pending":1,"lag":0}]`)
	f.Add(`invalid`)

	f.Fuzz(func(t *testing.T, input string) {
		decoder := json.NewDecoder(bytes.NewBufferString(input))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			value = input
		} else {
			value = normalizeJSONNumbers(value)
		}
		_, _ = parseClaimResponse(value)
		_, _ = parsePendingAttempts(value)
		_, _ = parseOldestPendingID(value)
		_, _ = parseGroupState(value, "workers")
		_, _ = alternatingFields(value)
	})
}

func FuzzSettlementStateTransitions(f *testing.F) {
	f.Add(int64(1), byte(0), false, false)
	f.Add(int64(2), byte(1), true, true)

	f.Fuzz(func(t *testing.T, attempts int64, action byte, reclaimed, fail bool) {
		opts := fuzzWorkerOptions(t)
		opts.maxDeliveryAttempts = 2
		encoded := job.NewMessage(rawMessage("payload"))
		transport := newFakeTransport(streamqueue.Delivery{})
		if fail {
			transport.ackErr = errors.New("ack failed")
			transport.deadLetterErr = errors.New("dead letter failed")
		}
		worker := &Worker{opts: opts, transport: transport}
		message, err := worker.decode(streamqueue.Delivery{
			ID: "1-0", Body: encoded.Bytes(), Attempts: attempts, Reclaimed: reclaimed,
		})
		if err != nil {
			t.Fatalf("decode valid delivery: %v", err)
		}
		settlement := message.(*job.Message)
		var first, second error
		if action%2 == 0 {
			first, second = settlement.Ack(), settlement.Nack()
		} else {
			first, second = settlement.Nack(), settlement.Ack()
		}
		if !errors.Is(second, first) || !errors.Is(first, second) {
			t.Fatalf("settlement was not stable: first=%v second=%v", first, second)
		}
	})
}

func fuzzWorkerOptions(t *testing.T) options {
	t.Helper()
	opts, err := newOptions(WithAddress("unused:6379"))
	if err != nil {
		t.Fatalf("construct fuzz options: %v", err)
	}
	return opts
}

func normalizeJSONNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		integer, err := typed.Int64()
		if err != nil {
			return typed.String()
		}
		return integer
	case []any:
		for index := range typed {
			typed[index] = normalizeJSONNumbers(typed[index])
		}
	case map[string]any:
		for key := range typed {
			typed[key] = normalizeJSONNumbers(typed[key])
		}
	}
	return value
}
