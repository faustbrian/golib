package gotelemetry

import (
	"errors"
	"net/http"

	telemetry "github.com/faustbrian/golib/pkg/telemetry"
	telemetryhttp "github.com/faustbrian/golib/pkg/telemetry/instrumentation/gohttpclient"
)

// ErrInvalidHTTPClient means telemetry could not be composed without
// discarding the caller's explicit transport policy.
var ErrInvalidHTTPClient = errors.New("gotelemetry: runtime, client, and explicit transport are required")

// InstrumentHTTPClient clones client and wraps its existing transport with
// telemetry tracing, metrics, and propagation. Requiring an explicit base
// transport prevents accidentally replacing a secure SSRF-aware transport
// with http.DefaultTransport.
func InstrumentHTTPClient(runtime *telemetry.Runtime, client *http.Client, operation string) (*http.Client, error) {
	if runtime == nil || client == nil || client.Transport == nil {
		return nil, ErrInvalidHTTPClient
	}
	transport, err := telemetryhttp.NewTransport(client.Transport, telemetryhttp.Config{
		Operation:      operation,
		TracerProvider: runtime.TracerProvider(),
		MeterProvider:  runtime.MeterProvider(),
		Propagator:     runtime.Propagator(),
	})
	if err != nil {
		return nil, err
	}
	clone := *client
	clone.Transport = transport

	return &clone, nil
}
