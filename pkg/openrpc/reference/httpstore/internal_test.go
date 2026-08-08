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
	if err := store.authorize(nil); !errors.Is(err, ErrURIDenied) {
		t.Fatalf("nil target error = %v", err)
	}
	if err := store.authorize(&url.URL{Scheme: "https", Host: "example.com", Fragment: "value"}); !errors.Is(err, ErrURIDenied) {
		t.Fatalf("fragment target error = %v", err)
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
	store.lookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) { return nil, nil }
	if _, err := store.dialContext(&net.Dialer{})(context.Background(), "tcp", "example.com:443"); !errors.Is(err, ErrRequest) {
		t.Fatalf("empty lookup error = %v", err)
	}
}

func TestDeniedAddressRejectsEveryNonPublicAddressClass(t *testing.T) {
	t.Parallel()

	for _, address := range []net.IP{
		net.ParseIP("10.0.0.1"),
		net.ParseIP("127.0.0.1"),
		net.ParseIP("169.254.1.1"),
		net.ParseIP("224.0.0.1"),
		net.ParseIP("239.1.1.1"),
		net.IPv4zero,
	} {
		if !deniedAddress(address) {
			t.Errorf("address %s was allowed", address)
		}
	}
	if deniedAddress(net.ParseIP("8.8.8.8")) {
		t.Fatal("public address was denied")
	}
}
