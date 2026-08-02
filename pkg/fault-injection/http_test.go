package faultinject_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"

	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
)

func TestRoundTripperPreservesRequestAndResponseBodyOwnership(t *testing.T) {
	t.Parallel()

	t.Run("pre-transport error closes request body", func(t *testing.T) {
		t.Parallel()
		body := &trackingReadCloser{Reader: bytes.NewBufferString("request")}
		request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.test", body)
		if err != nil {
			t.Fatal(err)
		}
		called := false
		transport, err := faultinject.NewRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		}), scopedInjector(t, faultinject.BoundaryHTTP, faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)), 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		response, err := transport.RoundTrip(request)
		if response != nil || !errors.Is(err, errInjected) || called || !body.closed {
			t.Fatalf("RoundTrip() = %v, %v; called=%t closed=%t", response, err, called, body.closed)
		}
	})

	t.Run("post-transport error closes response body", func(t *testing.T) {
		t.Parallel()
		body := &trackingReadCloser{Reader: bytes.NewBufferString("response")}
		transport, err := faultinject.NewRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
		}), scopedInjector(t, faultinject.BoundaryHTTP, faultinject.ErrorFault(faultinject.PhaseAfter, errInjected)), 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.test", nil)
		response, err := transport.RoundTrip(request)
		if response != nil || !errors.Is(err, errInjected) || !body.closed {
			t.Fatalf("RoundTrip() = %v, %v; closed=%t", response, err, body.closed)
		}
	})

	t.Run("response body remains caller owned and faultable", func(t *testing.T) {
		t.Parallel()
		body := &trackingReadCloser{Reader: bytes.NewBufferString("response")}
		injector := scopedInjector(t, faultinject.BoundaryHTTPBody,
			faultinject.ByteFault(faultinject.KindShortRead, faultinject.PhaseDuring, 3, 0))
		transport, err := faultinject.NewRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
		}), injector, 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.test", nil)
		response, err := transport.RoundTrip(request)
		if err != nil || response == nil || body.closed {
			t.Fatalf("RoundTrip() = %v, %v; closed=%t", response, err, body.closed)
		}
		buffer := make([]byte, 8)
		n, err := response.Body.Read(buffer)
		if err != nil || string(buffer[:n]) != "res" {
			t.Fatalf("body Read() = %q, %v", buffer[:n], err)
		}
		if err := response.Body.Close(); err != nil || !body.closed {
			t.Fatalf("body Close() = %v, closed=%t", err, body.closed)
		}
	})
}

func TestRoundTripperValidationAndDisabledIdentity(t *testing.T) {
	t.Parallel()

	base := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errInjected })
	transport, err := faultinject.NewRoundTripper(base, nil, 1, 2)
	if err != nil || reflect.ValueOf(transport).Pointer() != reflect.ValueOf(base).Pointer() {
		t.Fatalf("disabled transport = %T, %v", transport, err)
	}
	if _, err := faultinject.NewRoundTripper(nil, nil, 1, 2); !errors.Is(err, faultinject.ErrInvalidConfig) {
		t.Fatalf("nil transport error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (body *trackingReadCloser) Close() error {
	body.closed = true
	return nil
}
