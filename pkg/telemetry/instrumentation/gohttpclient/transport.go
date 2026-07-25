// Package gohttpclient adapts the privacy-preserving net/http client bridge to
// http-client's standard RoundTripper composition seam.
package gohttpclient

import (
	"net/http"

	"github.com/faustbrian/golib/pkg/telemetry/instrumentation/nethttp"
)

// Config is the fixed, low-cardinality outbound HTTP instrumentation config.
type Config = nethttp.ClientConfig

// Transport is a standard instrumented http.RoundTripper.
type Transport = nethttp.Transport

// NewTransport wraps the RoundTripper used by http-client without importing
// that module or creating a dependency cycle.
func NewTransport(base http.RoundTripper, config Config) (*Transport, error) {
	return nethttp.NewTransport(base, config)
}
