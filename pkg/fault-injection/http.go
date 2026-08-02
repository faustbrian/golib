package faultinject

import (
	"io"
	"net/http"
)

// NewRoundTripper returns a concurrent-safe in-process HTTP adapter. The base
// transport retains responsibility for HTTP semantics. Injected failures close
// request or response bodies at the same ownership boundary as transport
// failures; successful response bodies remain caller-owned.
func NewRoundTripper(
	base http.RoundTripper,
	injector *Injector,
	operation uint32,
	bodyOperation uint32,
) (http.RoundTripper, error) {
	if base == nil {
		return nil, invalid("RoundTripper", "must be non-nil")
	}
	if injector == nil || !injector.enabled {
		return base, nil
	}
	return &roundTripper{
		base: base, injector: injector, operation: operation,
		bodyOperation: bodyOperation,
	}, nil
}

type roundTripper struct {
	base          http.RoundTripper
	injector      *Injector
	operation     uint32
	bodyOperation uint32
}

func (transport *roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	decision := transport.injector.Decide(Metadata{Boundary: BoundaryHTTP, Operation: transport.operation})
	if !decision.Injected() {
		response, err := transport.base.RoundTrip(request)
		return transport.wrapBody(response), err
	}
	if err := faultPhaseError(request.Context(), decision.faults, PhaseBefore, transport.injector.sleeper); err != nil {
		closeRequestBody(request)
		return nil, err
	}
	operationContext, cleanup, duringError := prepareDuring(request.Context(), transport.injector.sleeper, decision.faults)
	defer cleanup()
	operationRequest := request
	if operationContext != request.Context() {
		operationRequest = request.Clone(operationContext)
	}
	response, organicError := transport.base.RoundTrip(operationRequest)
	if duringError != nil {
		closeResponseBody(response)
		return nil, duringError
	}
	if err := faultPhaseError(request.Context(), decision.faults, PhaseAfter, transport.injector.sleeper); err != nil {
		closeResponseBody(response)
		return nil, err
	}
	return transport.wrapBody(response), organicError
}

func (transport *roundTripper) wrapBody(response *http.Response) *http.Response {
	if response == nil || response.Body == nil {
		return response
	}
	response.Body = &injectedReadCloser{
		Reader: &injectedReader{
			reader: response.Body, injector: transport.injector,
			operation: transport.bodyOperation, boundary: BoundaryHTTPBody,
		},
		Closer: response.Body,
	}
	return response
}

func closeRequestBody(request *http.Request) {
	if request != nil && request.Body != nil {
		_ = request.Body.Close()
	}
}

func closeResponseBody(response *http.Response) {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
}

type injectedReadCloser struct {
	io.Reader
	io.Closer
}
