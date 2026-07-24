package httpstore

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
)

func TestAuthorizeAndDialRejectInvalidInternalInputs(t *testing.T) {
	t.Parallel()

	store, err := New(DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.authorize(&url.URL{Scheme: "https"}); !errors.Is(err, ErrURIDenied) {
		t.Fatalf("empty host error = %v", err)
	}
	dial := store.dialContext(&net.Dialer{})
	if _, err := dial(context.Background(), "tcp", "invalid-address"); !errors.Is(err, ErrAddressDenied) {
		t.Fatalf("invalid address error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dial(ctx, "tcp", "example.invalid:443"); !errors.Is(err, ErrRequest) {
		t.Fatalf("lookup error = %v", err)
	}
}
