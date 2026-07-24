package server_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/server"
)

func TestResolveReferenceUsesServerURLAsRFC3986Base(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		serverURL string
		reference string
		want      string
	}{
		{name: "absolute server", serverURL: "https://api.example.test/v1/",
			reference: "../docs/pets", want: "https://api.example.test/docs/pets"},
		{name: "relative server", serverURL: "/v1/",
			reference: "docs", want: "/v1/docs"},
		{name: "absolute reference", serverURL: "https://api.example.test/v1/",
			reference: "https://docs.example.test/pets#read",
			want:      "https://docs.example.test/pets#read"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := server.ResolveReference(
				test.serverURL, test.reference, server.ReferenceOptions{},
			)
			if err != nil || got != test.want {
				t.Fatalf("ResolveReference() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestResolveReferenceRejectsInvalidInputsAndBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		serverURL string
		reference string
		options   server.ReferenceOptions
		want      error
	}{
		{name: "empty server", reference: "docs", want: server.ErrInvalidReference},
		{name: "server query", serverURL: "https://api.example.test/?x=1",
			reference: "docs", want: server.ErrInvalidReference},
		{name: "server fragment", serverURL: "https://api.example.test/#part",
			reference: "docs", want: server.ErrInvalidReference},
		{name: "invalid server", serverURL: "%", reference: "docs",
			want: server.ErrInvalidReference},
		{name: "opaque server", serverURL: "https:opaque", reference: "docs",
			want: server.ErrInvalidReference},
		{name: "invalid reference", serverURL: "https://api.example.test/",
			reference: "%", want: server.ErrInvalidReference},
		{name: "negative limit", serverURL: "https://api.example.test/",
			reference: "docs", options: server.ReferenceOptions{MaxOutputBytes: -1},
			want: server.ErrInvalidReferenceOptions},
		{name: "output limit", serverURL: "https://api.example.test/",
			reference: "docs", options: server.ReferenceOptions{MaxOutputBytes: 3},
			want: server.ErrReferenceLimit},
		{name: "server input limit", serverURL: "relative", reference: "docs",
			options: server.ReferenceOptions{MaxInputBytes: 3},
			want:    server.ErrReferenceLimit},
		{name: "reference input limit", serverURL: "/", reference: "docs",
			options: server.ReferenceOptions{MaxInputBytes: 3},
			want:    server.ErrReferenceLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.ResolveReference(
				test.serverURL, test.reference, test.options,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("ResolveReference() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestResolveReferenceAcceptsExactBounds(t *testing.T) {
	t.Parallel()

	got, err := server.ResolveReference("/", "x", server.ReferenceOptions{
		MaxInputBytes: 1, MaxOutputBytes: 2,
	})
	if err != nil || got != "/x" {
		t.Fatalf("exact bounds = %q, %v", got, err)
	}
}
