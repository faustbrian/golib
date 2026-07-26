package webhook

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewDelivererDefaultsClockAndSleepAndRejectsUnsafeConfig(t *testing.T) {
	t.Parallel()

	if _, err := NewDeliverer(DeliveryConfig{}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewDeliverer() error = %v, want ErrInvalidConfiguration", err)
	}
	signer, _ := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: []byte("key")}}})
	deliverer, err := NewDeliverer(DeliveryConfig{
		Client: &responseDoer{response: response(http.StatusNoContent, http.NoBody)}, Signer: signer,
		EndpointPolicy: EndpointPolicyFunc(func(context.Context, *url.URL) error { return nil }),
		Retry:          RetryPolicy{MaxAttempts: 1, BaseDelay: 0, MaxDelay: 0},
		IDGenerator:    func() (string, error) { return "id", nil }, MaxRequestBytes: 64,
		MaxResponseBytes: 64, MaxFanOut: 1, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 256},
	})
	if err != nil {
		t.Fatalf("NewDeliverer() defaults error = %v", err)
	}
	if deliverer.clock == nil || deliverer.sleep == nil {
		t.Fatal("NewDeliverer() did not install defaults")
	}
}

func TestDeliverRejectsInvalidRequestAndIdentifierFailures(t *testing.T) {
	t.Parallel()

	deliverer := deliveryFixture(t, &errorDoer{err: errors.New("unused")}, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	for _, delivery := range []DeliveryRequest{
		{},
		{Endpoint: mustURL(t, "https://example.com"), EventID: ""},
		{Endpoint: mustURL(t, "https://example.com"), EventID: "event", Body: make([]byte, 1025)},
	} {
		if _, err := deliverer.Deliver(context.Background(), delivery); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("Deliver(%#v) error = %v", delivery, err)
		}
	}
	deliverer.id = func() (string, error) { return "", errors.New("entropy unavailable") }
	if _, err := deliverer.Deliver(context.Background(), validDelivery(t)); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("Deliver() delivery ID error = %v", err)
	}
	var calls atomic.Int32
	deliverer.id = func() (string, error) {
		if calls.Add(1) == 1 {
			return "delivery", nil
		}
		return "", errors.New("entropy unavailable")
	}
	if _, err := deliverer.Deliver(context.Background(), validDelivery(t)); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("Deliver() attempt ID error = %v", err)
	}
}

func TestDeliverHandlesCancellationRequestConstructionAndSigningFailures(t *testing.T) {
	t.Parallel()

	deliverer := deliveryFixture(t, &errorDoer{err: errors.New("unused")}, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := deliverer.Deliver(ctx, validDelivery(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Deliver() canceled error = %v", err)
	}
	invalid := validDelivery(t)
	invalid.Endpoint = &url.URL{Scheme: "https", Host: "[invalid"}
	if _, err := deliverer.Deliver(context.Background(), invalid); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("Deliver() request construction error = %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	inactive, _ := NewSigner(SignerConfig{
		Algorithm: SHA256,
		Keys:      []SigningKey{{ID: "future", Secret: []byte("key"), NotBefore: now.Add(time.Hour)}},
		Clock:     func() time.Time { return now },
	})
	deliverer.signer = inactive
	if _, err := deliverer.Deliver(context.Background(), validDelivery(t)); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("Deliver() signing error = %v", err)
	}
}

func TestDeliverClassifiesCanceledTransportAndExhaustedStatus(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	doer := HTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return nil, errors.New("transport stopped")
	})
	deliverer := deliveryFixture(t, doer, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	result, err := deliverer.Deliver(ctx, validDelivery(t))
	if !errors.Is(err, context.Canceled) || len(result.Attempts) != 1 || result.Attempts[0].Classification != FailureTerminal {
		t.Fatalf("Deliver() canceled transport = %#v, %v", result, err)
	}

	doer = HTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusServiceUnavailable, http.NoBody), nil
	})
	deliverer = deliveryFixture(t, doer, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	result, err = deliverer.DeliverOnce(context.Background(), validDelivery(t))
	if !errors.Is(err, ErrDeliveryFailed) || result.Attempts[0].Classification != FailureExhausted {
		t.Fatalf("DeliverOnce() exhausted status = %#v, %v", result, err)
	}
}

func TestDeliverClosesResponseReturnedWithTransportError(t *testing.T) {
	t.Parallel()

	body := &observedBody{reader: bytes.NewReader([]byte("ignored"))}
	doer := &responseDoer{response: response(http.StatusBadGateway, body), err: errors.New("redirect or transport failure")}
	deliverer := deliveryFixture(t, doer, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	if _, err := deliverer.DeliverOnce(context.Background(), validDelivery(t)); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("DeliverOnce() error = %v", err)
	}
	if !body.closed || body.reads != 0 {
		t.Fatalf("response body closed = %v, reads = %d", body.closed, body.reads)
	}
}

func TestDeliverSurfacesCancellationDuringStatusRetryDelay(t *testing.T) {
	t.Parallel()

	doer := HTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusServiceUnavailable, http.NoBody), nil
	})
	deliverer := deliveryFixture(t, doer, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error {
		return context.Canceled
	})
	result, err := deliverer.Deliver(context.Background(), validDelivery(t))
	if !errors.Is(err, context.Canceled) || len(result.Attempts) != 1 || result.Attempts[0].Classification != FailureRetryable {
		t.Fatalf("Deliver() retry cancellation = %#v, %v", result, err)
	}
}

func TestReadResponseRejectsMissingReadAndCloseFailures(t *testing.T) {
	t.Parallel()

	if _, err := readResponse(nil, 1); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("readResponse(nil) error = %v", err)
	}
	if _, err := readResponse(&http.Response{}, 1); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("readResponse(no body) error = %v", err)
	}
	if _, err := readResponse(response(200, &faultBody{readErr: errors.New("read")}), 1); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("readResponse(read) error = %v", err)
	}
	if _, err := readResponse(response(200, &faultBody{reader: bytesReader("x"), closeErr: errors.New("close")}), 1); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("readResponse(close) error = %v", err)
	}
}

func TestDeliveryHelpersCoverFallbackReplayAndSleep(t *testing.T) {
	t.Parallel()

	policy := RetryPolicy{BaseDelay: 4 * time.Second, MaxDelay: 5 * time.Second}
	if got := policy.Delay(3, time.Now(), "invalid"); got != 5*time.Second {
		t.Fatalf("Delay() overflow cap = %v", got)
	}
	deliverer := deliveryFixture(t, &responseDoer{response: response(http.StatusNoContent, http.NoBody)}, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	results, err := deliverer.FanOut(context.Background(), nil, 1)
	if err != nil || len(results) != 0 {
		t.Fatalf("FanOut(empty) = %#v, %v", results, err)
	}
	if _, err := deliverer.Replay(context.Background(), "", validDelivery(t)); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("Replay(empty) error = %v", err)
	}
	result, err := deliverer.Replay(context.Background(), "old", validDelivery(t))
	if err != nil || len(result.Attempts) != 1 {
		t.Fatalf("Replay() = %#v, %v", result, err)
	}
	if err := sleepContext(context.Background(), 0); err != nil {
		t.Fatalf("sleepContext() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("sleepContext(canceled) error = %v", err)
	}
}

func TestDeliveryWireRejectsUnsafeMarshalAndNilMetadata(t *testing.T) {
	t.Parallel()

	unsafe := validDelivery(t)
	unsafe.Endpoint.User = url.User("user")
	if _, err := MarshalDeliveryRequest(unsafe, 1024); !errors.Is(err, ErrDeliveryEncoding) {
		t.Fatalf("MarshalDeliveryRequest() unsafe error = %v", err)
	}
	if cloneStrings(nil) != nil {
		t.Fatal("cloneStrings(nil) was non-nil")
	}
}

func validDelivery(t *testing.T) DeliveryRequest {
	t.Helper()

	return DeliveryRequest{
		Endpoint: mustURL(t, "https://example.com/hook"), Body: []byte("body"),
		EventID: "event", IdempotencyKey: "event",
	}
}

type HTTPDoerFunc func(*http.Request) (*http.Response, error)

func (f HTTPDoerFunc) Do(request *http.Request) (*http.Response, error) { return f(request) }

type responseDoer struct {
	response *http.Response
	err      error
}

func (d *responseDoer) Do(*http.Request) (*http.Response, error) { return d.response, d.err }

func response(status int, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: body}
}

func bytesReader(value string) *bytes.Reader { return bytes.NewReader([]byte(value)) }
