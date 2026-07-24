package server

import (
	"errors"
	"fmt"
	"net/url"
)

var (
	// ErrInvalidReference reports an invalid expanded Server Object URL or
	// relative API URL reference.
	ErrInvalidReference = errors.New("invalid OpenAPI server reference")
	// ErrInvalidReferenceOptions reports negative reference-resolution limits.
	ErrInvalidReferenceOptions = errors.New("invalid server reference options")
	// ErrReferenceLimit reports input or output beyond caller policy.
	ErrReferenceLimit = errors.New("OpenAPI server reference limit exceeded")
)

// ReferenceOptions bounds RFC 3986 resolution against an expanded Server
// Object URL.
type ReferenceOptions struct {
	MaxInputBytes  int
	MaxOutputBytes int
}

// DefaultReferenceOptions returns conservative bounds for server-relative URL
// resolution.
func DefaultReferenceOptions() ReferenceOptions {
	return ReferenceOptions{MaxInputBytes: 1 << 20, MaxOutputBytes: 1 << 20}
}

// ResolveReference resolves an API URL reference against an expanded Server
// Object URL using RFC 3986 semantics. The server URL may itself be relative.
// It must not contain a query or fragment, as required by OpenAPI 3.1 and 3.2.
func ResolveReference(
	serverURL string,
	rawReference string,
	options ReferenceOptions,
) (string, error) {
	if options.MaxInputBytes < 0 || options.MaxOutputBytes < 0 {
		return "", ErrInvalidReferenceOptions
	}
	defaults := DefaultReferenceOptions()
	if options.MaxInputBytes == 0 {
		options.MaxInputBytes = defaults.MaxInputBytes
	}
	if options.MaxOutputBytes == 0 {
		options.MaxOutputBytes = defaults.MaxOutputBytes
	}
	if len(serverURL) > options.MaxInputBytes ||
		len(rawReference) > options.MaxInputBytes {
		return "", ErrReferenceLimit
	}
	if serverURL == "" {
		return "", fmt.Errorf("%w: empty server URL", ErrInvalidReference)
	}
	base, err := url.Parse(serverURL)
	if err != nil || base.Opaque != "" || base.RawQuery != "" ||
		base.Fragment != "" {
		return "", fmt.Errorf("%w: malformed server URL", ErrInvalidReference)
	}
	referenceURL, err := url.Parse(rawReference)
	if err != nil {
		return "", fmt.Errorf("%w: malformed API URL", ErrInvalidReference)
	}
	resolved := base.ResolveReference(referenceURL).String()
	if len(resolved) > options.MaxOutputBytes {
		return "", ErrReferenceLimit
	}
	return resolved, nil
}
