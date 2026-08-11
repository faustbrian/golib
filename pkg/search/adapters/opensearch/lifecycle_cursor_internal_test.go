package opensearch

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func TestReindexCursorCodecValidatesEachBindingBoundary(t *testing.T) {
	t.Parallel()

	codec := newInternalReindexCursorCodec(t)
	maximumTenant := strings.Repeat("t", search.DefaultLimits().MaxTenantBytes)
	for _, test := range []struct {
		name, tenant, source, target, task string
		wantValid                          bool
	}{
		{name: "valid", tenant: "tenant", source: "events-v1", target: "events-v2", task: "node:123", wantValid: true},
		{name: "maximum tenant", tenant: maximumTenant, source: "events-v1", target: "events-v2", task: "node:123", wantValid: true},
		{name: "empty tenant", tenant: "", source: "events-v1", target: "events-v2", task: "node:123"},
		{name: "oversized tenant", tenant: maximumTenant + "t", source: "events-v1", target: "events-v2", task: "node:123"},
		{name: "invalid source", tenant: "tenant", source: "events/v1", target: "events-v2", task: "node:123"},
		{name: "invalid target", tenant: "tenant", source: "events-v1", target: "events/v2", task: "node:123"},
		{name: "identical generations", tenant: "tenant", source: "events-v1", target: "events-v1", task: "node:123"},
		{name: "empty task", tenant: "tenant", source: "events-v1", target: "events-v2", task: ""},
		{name: "oversized task", tenant: "tenant", source: "events-v1", target: "events-v2", task: strings.Repeat("t", 513)},
		{name: "unsafe task", tenant: "tenant", source: "events-v1", target: "events-v2", task: "node/task"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := codec.encode(test.tenant, test.source, test.target, test.task)
			if (err == nil) != test.wantValid {
				t.Fatalf("encode() error = %v, want valid %t", err, test.wantValid)
			}
		})
	}
}

func TestReindexCursorCodecRejectsEachEncryptedTokenDefect(t *testing.T) {
	t.Parallel()

	codec := newInternalReindexCursorCodec(t)
	valid, err := codec.encode("tenant", "events-v1", "events-v2", "node:123")
	if err != nil {
		t.Fatal(err)
	}
	malformedJSON, err := codec.seal([]byte("{"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, token, tenant string
	}{
		{name: "oversized token", token: strings.Repeat("x", codec.maxBytes+1), tenant: "tenant"},
		{name: "invalid requested binding", token: valid, tenant: ""},
		{name: "invalid base64", token: "%", tenant: "tenant"},
		{name: "noncanonical base64", token: "_x", tenant: "tenant"},
		{name: "malformed decrypted json", token: malformedJSON, tenant: "tenant"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := codec.decode(test.token, test.tenant, "events-v1", "events-v2"); !errors.Is(err, ErrInvalidReindexCursor) {
				t.Fatalf("decode() error = %v", err)
			}
		})
	}
}

func TestReindexCursorCodecRejectsEachDecryptedEnvelopeDefect(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	codec := newInternalReindexCursorCodecAt(t, now)
	valid := reindexCursorEnvelope{
		Version: 1, Tenant: "tenant", Source: "events-v1", Target: "events-v2",
		Task: "node:123", ExpiresUnix: now.Add(time.Minute).UnixNano(),
	}
	for _, test := range []struct {
		name   string
		mutate func(*reindexCursorEnvelope)
		want   error
	}{
		{name: "version", mutate: func(value *reindexCursorEnvelope) { value.Version = 0 }, want: ErrInvalidReindexCursor},
		{name: "empty task", mutate: func(value *reindexCursorEnvelope) { value.Task = "" }, want: ErrInvalidReindexCursor},
		{name: "oversized task", mutate: func(value *reindexCursorEnvelope) { value.Task = strings.Repeat("t", 513) }, want: ErrInvalidReindexCursor},
		{name: "unsafe task", mutate: func(value *reindexCursorEnvelope) { value.Task = "node/task" }, want: ErrInvalidReindexCursor},
		{name: "tenant binding", mutate: func(value *reindexCursorEnvelope) { value.Tenant = "other" }, want: ErrReindexCursorBinding},
		{name: "source binding", mutate: func(value *reindexCursorEnvelope) { value.Source = "other-v1" }, want: ErrReindexCursorBinding},
		{name: "target binding", mutate: func(value *reindexCursorEnvelope) { value.Target = "other-v2" }, want: ErrReindexCursorBinding},
		{name: "exact expiry", mutate: func(value *reindexCursorEnvelope) { value.ExpiresUnix = now.UnixNano() }, want: ErrReindexCursorExpired},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			envelope := valid
			test.mutate(&envelope)
			payload, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			token, err := codec.seal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := codec.decode(token, "tenant", "events-v1", "events-v2"); !errors.Is(err, test.want) {
				t.Fatalf("decode() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReindexCursorCodecAcceptsExactEncodedSizeLimit(t *testing.T) {
	t.Parallel()

	codec := newInternalReindexCursorCodec(t)
	token, err := codec.encode("tenant", "events-v1", "events-v2", "node:123")
	if err != nil {
		t.Fatal(err)
	}
	codec.maxBytes = len(token)
	codec.random = zeroReader{}
	exact, err := codec.encode("tenant", "events-v1", "events-v2", "node:123")
	if err != nil || len(exact) != codec.maxBytes {
		t.Fatalf("encode() token/error = %d/%v, want %d", len(exact), err, codec.maxBytes)
	}
	if task, err := codec.decode(exact, "tenant", "events-v1", "events-v2"); err != nil || task != "node:123" {
		t.Fatalf("decode() task/error = %q/%v", task, err)
	}
	codec.maxBytes--
	if _, err := codec.encode("tenant", "events-v1", "events-v2", "node:123"); !errors.Is(err, ErrInvalidReindexCursor) {
		t.Fatalf("oversized encode() error = %v", err)
	}
}

func TestReindexCursorCodecRejectsExpiryOutsideUnixNanoRange(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		now  time.Time
	}{
		{name: "below minimum", now: time.Unix(0, -1<<63).Add(-2 * time.Hour)},
		{name: "above maximum", now: time.Unix(0, 1<<63-1).Add(-30 * time.Minute)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			codec := newInternalReindexCursorCodecAt(t, test.now)
			if _, err := codec.encode("tenant", "events-v1", "events-v2", "node:123"); !errors.Is(err, ErrInvalidReindexCursor) {
				t.Fatalf("encode() error = %v, want ErrInvalidReindexCursor", err)
			}
		})
	}
}

func newInternalReindexCursorCodec(t *testing.T) *ReindexCursorCodec {
	t.Helper()
	return newInternalReindexCursorCodecAt(t, time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC))
}

func newInternalReindexCursorCodecAt(t *testing.T, now time.Time) *ReindexCursorCodec {
	t.Helper()
	codec, err := NewReindexCursorCodec(make([]byte, 32), func() time.Time { return now }, MaximumReindexCursorBytes, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	codec.random = zeroReader{}
	return codec
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}
