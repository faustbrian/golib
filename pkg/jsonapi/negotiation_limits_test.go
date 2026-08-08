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

func TestNegotiationLimitsAcceptExactBoundaries(t *testing.T) {
	t.Parallel()

	const (
		first  = "https://example.com/a"
		second = "https://example.com/b"
	)
	negotiator, err := NewNegotiatorWithLimits(
		[]string{first},
		[]string{second},
		NegotiationLimits{
			MaxSupportedURIs: 2,
			MaxURIBytes:      len(first),
			MaxHeaderBytes:   len(MediaTypeJSONAPI),
		},
	)
	if err != nil {
		t.Fatalf("construct exact-limit negotiator: %v", err)
	}
	if _, err := negotiator.CheckContentType(MediaTypeJSONAPI); err != nil {
		t.Fatalf("exact Content-Type byte limit rejected: %v", err)
	}
	if _, err := negotiator.NegotiateAccept(MediaTypeJSONAPI); err != nil {
		t.Fatalf("exact Accept byte limit rejected: %v", err)
	}

	listHeader := `application/vnd.api+json;ext="` + first + ` ` + second + `"`
	listNegotiator, err := NewNegotiatorWithLimits(
		[]string{first, second},
		nil,
		NegotiationLimits{
			MaxSupportedURIs: 2,
			MaxParameterURIs: 2,
			MaxURIBytes:      len(first),
			MaxHeaderBytes:   len(listHeader),
		},
	)
	if err != nil {
		t.Fatalf("construct exact URI-list negotiator: %v", err)
	}
	if _, err := listNegotiator.CheckContentType(listHeader); err != nil {
		t.Fatalf("exact URI-list limits rejected: %v", err)
	}

	candidates := MediaTypeJSONAPI + "," + MediaTypeJSONAPI
	candidateNegotiator, err := NewNegotiatorWithLimits(
		nil,
		nil,
		NegotiationLimits{
			MaxHeaderBytes:      len(candidates),
			MaxAcceptCandidates: 2,
		},
	)
	if err != nil {
		t.Fatalf("construct exact candidate negotiator: %v", err)
	}
	if _, err := candidateNegotiator.NegotiateAccept(candidates); err != nil {
		t.Fatalf("exact candidate limits rejected: %v", err)
	}
}

func TestNegotiationSupportedURILimitAccumulatesKinds(t *testing.T) {
	t.Parallel()

	_, err := NewNegotiatorWithLimits(
		[]string{"https://example.com/extension"},
		[]string{"https://example.com/profile"},
		NegotiationLimits{MaxSupportedURIs: 1},
	)
	var limitError *NegotiationError
	if !errors.As(err, &limitError) || limitError.Code != "limit" {
		t.Fatalf("mixed supported URI limit escaped: %T %#v", err, limitError)
	}
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
