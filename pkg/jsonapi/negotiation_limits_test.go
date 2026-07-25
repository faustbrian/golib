package jsonapi

import (
	"errors"
	"testing"
)

func TestNegotiationLimitsBoundHeadersAndCandidates(t *testing.T) {
	t.Parallel()

	negotiator, err := NewNegotiatorWithLimits(nil, nil, NegotiationLimits{
		MaxHeaderBytes: 32,
	})
	if err != nil {
		t.Fatalf("construct limited negotiator: %v", err)
	}
	_, err = negotiator.CheckContentType("application/vnd.api+json;profile=toolong")
	assertNegotiationError(t, err, 415, "limit")

	candidateNegotiator, err := NewNegotiatorWithLimits(nil, nil, NegotiationLimits{
		MaxHeaderBytes:      128,
		MaxAcceptCandidates: 1,
	})
	if err != nil {
		t.Fatalf("construct candidate-limited negotiator: %v", err)
	}
	_, err = candidateNegotiator.NegotiateAccept(
		"application/vnd.api+json,application/vnd.api+json",
	)
	assertNegotiationError(t, err, 406, "limit")

	shortHeaderNegotiator, err := NewNegotiatorWithLimits(nil, nil, NegotiationLimits{
		MaxHeaderBytes: 3,
	})
	if err != nil {
		t.Fatalf("construct short-header negotiator: %v", err)
	}
	_, err = shortHeaderNegotiator.NegotiateAccept("application/vnd.api+json")
	assertNegotiationError(t, err, 406, "limit")
}

func TestNegotiationLimitsBoundURILists(t *testing.T) {
	t.Parallel()

	const first = "https://example.com/a"
	const second = "https://example.com/b"
	negotiator, err := NewNegotiatorWithLimits(
		[]string{first},
		nil,
		NegotiationLimits{MaxParameterURIs: 1, MaxURIBytes: len(first)},
	)
	if err != nil {
		t.Fatalf("construct limited negotiator: %v", err)
	}
	_, err = negotiator.CheckContentType(
		`application/vnd.api+json;ext="` + first + ` ` + second + `"`,
	)
	assertNegotiationError(t, err, 415, "limit")

	_, err = negotiator.CheckContentType(
		`application/vnd.api+json;profile="https://example.com/too-long"`,
	)
	assertNegotiationError(t, err, 415, "limit")
}

func TestNegotiationLimitConfiguration(t *testing.T) {
	t.Parallel()

	defaults := DefaultNegotiationLimits()
	if defaults.MaxHeaderBytes < 1 || defaults.MaxAcceptCandidates < 1 ||
		defaults.MaxParameterURIs < 1 || defaults.MaxURIBytes < 1 ||
		defaults.MaxSupportedURIs < 1 {
		t.Fatalf("unsafe negotiation defaults: %#v", defaults)
	}
	if _, err := NewNegotiatorWithLimits(
		nil,
		nil,
		NegotiationLimits{MaxHeaderBytes: -1},
	); err == nil {
		t.Fatal("expected invalid negotiation limits error")
	}
	_, err := NewNegotiatorWithLimits(
		[]string{"https://example.com/a", "https://example.com/b"},
		nil,
		NegotiationLimits{MaxSupportedURIs: 1},
	)
	var limitError *NegotiationError
	if !errors.As(err, &limitError) || limitError.Code != "limit" {
		t.Fatalf("expected supported URI limit error, got %T: %#v", err, limitError)
	}
	_, err = NewNegotiatorWithLimits(
		[]string{"https://example.com/too-long"},
		nil,
		NegotiationLimits{MaxURIBytes: 5},
	)
	if !errors.As(err, &limitError) || limitError.Code != "limit" {
		t.Fatalf("expected supported URI byte limit error, got %T: %#v", err, limitError)
	}
}
