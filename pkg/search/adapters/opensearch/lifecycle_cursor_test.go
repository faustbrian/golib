package opensearch_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestReindexCursorIsEncryptedBoundAndExpiring(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	codec, err := adapter.NewReindexCursorCodec(
		[]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 4096, time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	client := newLifecycleCursorClient(t, codec, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			if request.Method != http.MethodPost || request.URL.Path != "/_reindex" {
				t.Fatalf("start request = %s %s", request.Method, request.URL.String())
			}
			return cursorResponse(http.StatusOK, `{"task":"node:123"}`), nil
		}
		if request.Method != http.MethodGet || request.URL.Path != "/_tasks/node:123" {
			t.Fatalf("poll request = %s %s", request.Method, request.URL.String())
		}
		return cursorResponse(http.StatusOK, `{"completed":false}`), nil
	}))

	cursor, done, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", "")
	if err != nil || done || cursor == "" || cursor == "node:123" {
		t.Fatalf("start Reindex() = %q/%t/%v", cursor, done, err)
	}
	payloadText := strings.SplitN(cursor, ".", 2)[0]
	payload, decodeErr := base64.RawURLEncoding.DecodeString(payloadText)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	for _, secret := range []string{"tenant-a", "events-v1", "events-v2", "node:123"} {
		if bytes.Contains(payload, []byte(secret)) {
			t.Fatalf("encrypted cursor discloses %q", secret)
		}
	}
	now = now.Add(45 * time.Second)
	continued, done, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", cursor)
	if err != nil || done || continued == cursor || continued == "" {
		t.Fatalf("poll Reindex() = %q/%t/%v", continued, done, err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}

	for _, test := range []struct {
		name, tenant, token string
		want                error
	}{
		{name: "tenant binding", tenant: "tenant-b", token: cursor, want: adapter.ErrReindexCursorBinding},
		{name: "tamper", tenant: "tenant-a", token: cursor[:len(cursor)-1] + replacement(cursor[len(cursor)-1]), want: adapter.ErrInvalidReindexCursor},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, cursorErr := client.Reindex(t.Context(), test.tenant, "events-v1", "events-v2", test.token); !errors.Is(cursorErr, test.want) {
				t.Fatalf("Reindex() error = %v, want %v", cursorErr, test.want)
			}
		})
	}
	now = now.Add(30 * time.Second)
	if _, _, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", cursor); !errors.Is(err, adapter.ErrReindexCursorExpired) {
		t.Fatalf("expired Reindex() error = %v", err)
	}
	if renewed, complete, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", continued); err != nil || complete || renewed == continued {
		t.Fatalf("renewed Reindex() = %q/%t/%v", renewed, complete, err)
	}
	if requests != 3 {
		t.Fatalf("invalid/renewed cursors reached transport: %d requests", requests)
	}
}

func TestReindexRejectsStructurallyInvalidIncompleteTaskResponses(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`{}`, `{"completed":false,"error":{"type":"failed"}}`} {
		body := body
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			requests := 0
			client := newLifecycleCursorClient(t, mustReindexCursorCodec(t), roundTripFunc(func(*http.Request) (*http.Response, error) {
				requests++
				if requests == 1 {
					return cursorResponse(http.StatusOK, `{"task":"node:123"}`), nil
				}
				return cursorResponse(http.StatusOK, body), nil
			}))
			cursor, _, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", "")
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", cursor)
			var failure *adapter.Failure
			if !errors.As(err, &failure) || failure.Category != adapter.FailureMalformed || !failure.OutcomeKnown {
				t.Fatalf("Reindex() error = %#v / %v", failure, err)
			}
		})
	}
}

func TestReindexRejectsInvalidStartedTaskIdentifiersAsMalformedUnknownOutcomes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		task string
	}{
		{name: "empty", task: ""},
		{name: "oversized", task: strings.Repeat("t", 513)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := newLifecycleCursorClient(t, mustReindexCursorCodec(t), roundTripFunc(func(*http.Request) (*http.Response, error) {
				return cursorResponse(http.StatusOK, `{"task":"`+test.task+`"}`), nil
			}))

			_, _, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", "")
			var failure *adapter.Failure
			if !errors.Is(err, adapter.ErrLifecycleRejected) || errors.Is(err, adapter.ErrInvalidReindexCursor) ||
				!errors.As(err, &failure) || failure.Category != adapter.FailureMalformed || failure.OutcomeKnown {
				t.Fatalf("Reindex() error = %#v / %v", failure, err)
			}
		})
	}
}

func TestReindexFailsBeforeDispatchWithoutCursorCodec(t *testing.T) {
	t.Parallel()

	requests := 0
	client := newLifecycleCursorClient(t, nil, roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return cursorResponse(http.StatusOK, `{"task":"node:123"}`), nil
	}))
	if _, _, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", ""); !errors.Is(err, adapter.ErrLifecycleCursorCodecRequired) {
		t.Fatalf("Reindex() error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("missing codec reached transport: %d requests", requests)
	}
}

func TestReindexRejectsIdenticalGenerationsBeforeDispatch(t *testing.T) {
	t.Parallel()

	requests := 0
	client := newLifecycleCursorClient(t, mustReindexCursorCodec(t), roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return cursorResponse(http.StatusOK, `{"task":"node:123"}`), nil
	}))
	if _, _, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v1", ""); !errors.Is(err, adapter.ErrUnsafeIndexTarget) {
		t.Fatalf("Reindex() error = %v, want ErrUnsafeIndexTarget", err)
	}
	if requests != 0 {
		t.Fatalf("identical generations reached transport: %d requests", requests)
	}
}

func TestReindexRejectsOversizedTenantBeforeAuthorizationOrDispatch(t *testing.T) {
	t.Parallel()

	authorizations, requests := 0, 0
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"},
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return cursorResponse(http.StatusOK, `{"task":"node:123"}`), nil
		}),
		TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout:     time.Second, MaximumResponseBytes: 4096,
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error {
				authorizations++
				return nil
			}),
			ReindexCursorCodec: mustReindexCursorCodec(t),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	tenant := strings.Repeat("t", search.DefaultLimits().MaxTenantBytes+1)
	if _, _, err := client.Reindex(t.Context(), tenant, "events-v1", "events-v2", ""); !errors.Is(err, adapter.ErrUnsafeIndexTarget) {
		t.Fatalf("Reindex() error = %v, want ErrUnsafeIndexTarget", err)
	}
	if _, err := client.CutoverAlias(t.Context(), tenant, "events-read", "events-v1", "events-v2", "definition-v2"); !errors.Is(err, adapter.ErrUnsafeIndexTarget) {
		t.Fatalf("CutoverAlias() error = %v, want ErrUnsafeIndexTarget", err)
	}
	if authorizations != 0 || requests != 0 {
		t.Fatalf("oversized tenant reached authorization/transport: %d/%d", authorizations, requests)
	}
}

func TestReindexCursorCodecRejectsUnboundedConfigurationAndClock(t *testing.T) {
	t.Parallel()

	key := []byte("0123456789abcdef0123456789abcdef")
	for _, test := range []struct {
		name     string
		key      []byte
		maxBytes int
		ttl      time.Duration
	}{
		{name: "undersized key", key: key[:len(key)-1], maxBytes: 4096, ttl: time.Hour},
		{name: "oversized key", key: make([]byte, adapter.MaximumReindexCursorKeyBytes+1), maxBytes: 4096, ttl: time.Hour},
		{name: "nil clock", key: key, maxBytes: 4096, ttl: time.Hour},
		{name: "zero token budget", key: key, maxBytes: 0, ttl: time.Hour},
		{name: "oversized token budget", key: key, maxBytes: adapter.MaximumReindexCursorBytes + 1, ttl: time.Hour},
		{name: "zero ttl", key: key, maxBytes: 4096, ttl: 0},
		{name: "oversized ttl", key: key, maxBytes: 4096, ttl: adapter.MaximumReindexCursorTTL + time.Nanosecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now
			if test.name == "nil clock" {
				now = nil
			}
			if _, err := adapter.NewReindexCursorCodec(test.key, now, test.maxBytes, test.ttl); !errors.Is(err, adapter.ErrInvalidReindexCursorCodec) {
				t.Fatalf("NewReindexCursorCodec() error = %v", err)
			}
		})
	}

	for _, test := range []struct {
		name     string
		keyBytes int
		maxBytes int
		ttl      time.Duration
	}{
		{name: "minimum key", keyBytes: 32, maxBytes: 4096, ttl: time.Hour},
		{name: "maximum key", keyBytes: adapter.MaximumReindexCursorKeyBytes, maxBytes: 4096, ttl: time.Hour},
		{name: "maximum token budget", keyBytes: 32, maxBytes: adapter.MaximumReindexCursorBytes, ttl: time.Hour},
		{name: "maximum ttl", keyBytes: 32, maxBytes: 4096, ttl: adapter.MaximumReindexCursorTTL},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := adapter.NewReindexCursorCodec(make([]byte, test.keyBytes), time.Now, test.maxBytes, test.ttl); err != nil {
				t.Fatalf("NewReindexCursorCodec() error = %v", err)
			}
		})
	}

	codec, err := adapter.NewReindexCursorCodec(key, func() time.Time {
		return time.Unix(0, int64(^uint64(0)>>1)).Add(-time.Minute + time.Nanosecond)
	}, 4096, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client := newLifecycleCursorClient(t, codec, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return cursorResponse(http.StatusOK, `{"task":"node:123"}`), nil
	}))
	if _, _, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", ""); !errors.Is(err, adapter.ErrInvalidReindexCursor) {
		t.Fatalf("Reindex() error = %v, want ErrInvalidReindexCursor", err)
	}

	for _, test := range []struct {
		name, task string
		maxBytes   int
	}{
		{name: "unsafe backend task", task: "node/task", maxBytes: 4096},
		{name: "encoded token exceeds configured bound", task: "node:123", maxBytes: 64},
	} {
		t.Run(test.name, func(t *testing.T) {
			bounded, codecErr := adapter.NewReindexCursorCodec(key, time.Now, test.maxBytes, time.Minute)
			if codecErr != nil {
				t.Fatal(codecErr)
			}
			boundedClient := newLifecycleCursorClient(t, bounded, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return cursorResponse(http.StatusOK, `{"task":"`+test.task+`"}`), nil
			}))
			if _, _, reindexErr := boundedClient.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", ""); !errors.Is(reindexErr, adapter.ErrInvalidReindexCursor) {
				t.Fatalf("Reindex() error = %v, want ErrInvalidReindexCursor", reindexErr)
			} else if test.maxBytes == 64 {
				var failure *adapter.Failure
				if !errors.As(reindexErr, &failure) || failure.OutcomeKnown {
					t.Fatalf("started task without a returnable cursor = %#v", failure)
				}
			}
		})
	}
}

func newLifecycleCursorClient(t *testing.T, codec *adapter.ReindexCursorCodec, transport http.RoundTripper) *adapter.Client {
	t.Helper()
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, Transport: transport,
		TransportOwnership: adapter.TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }),
			Verifier: adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
				return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint}, nil
			}),
			ReindexCursorCodec: codec,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func mustReindexCursorCodec(t *testing.T) *adapter.ReindexCursorCodec {
	t.Helper()
	codec, err := adapter.NewReindexCursorCodec(
		[]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096, time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func replacement(value byte) string {
	if value == 'A' {
		return "B"
	}
	return strings.Repeat("A", 1)
}

func cursorResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
