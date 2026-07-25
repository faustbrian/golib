// Package httpcorrelation explicitly propagates correlation metadata over
// HTTP. Middleware installation and proxy trust remain application owned.
package httpcorrelation

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

const (
	// CorrelationHeader carries the logical workflow identifier.
	CorrelationHeader = "X-Correlation-ID"
	// RequestHeader carries the current HTTP hop identifier.
	RequestHeader = "X-Request-ID"
	// CausationHeader carries the immediate parent identifier.
	CausationHeader = "X-Causation-ID"
	maxHeaderValues = 8
)

// InvalidPolicy controls malformed or ambiguous inbound metadata.
type InvalidPolicy uint8

const (
	// ReplaceInvalid discards every inbound value and creates a fresh root.
	ReplaceInvalid InvalidPolicy = iota
	// RejectInvalid returns HTTP 400 before application code runs.
	RejectInvalid
)

// ErrInvalidOptions reports invalid HTTP adapter configuration.
var ErrInvalidOptions = errors.New("http correlation: invalid options")

// Options configure immutable HTTP propagation policy.
type Options struct {
	Policy  correlation.Policy
	Invalid InvalidPolicy
	Trust   func(*http.Request) bool
}

// Middleware owns explicit HTTP extraction, trust, context, and injection.
type Middleware struct {
	factory    *correlation.Factory
	codec      *correlation.Codec
	propagator *correlation.Propagator
	options    Options
}

// New constructs an HTTP adapter without installing global middleware.
func New(factory *correlation.Factory, options Options) (*Middleware, error) {
	if factory == nil || options.Invalid > RejectInvalid {
		return nil, ErrInvalidOptions
	}
	codec, err := correlation.NewCodec(correlation.CodecOptions{
		Policy: options.Policy, CorrelationField: CorrelationHeader,
		RequestField: RequestHeader, CausationField: CausationHeader,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidOptions, err)
	}
	propagator, _ := correlation.NewPropagator(factory, codec)
	return &Middleware{factory: factory, codec: codec, propagator: propagator, options: options}, nil
}

// Wrap creates one fresh request ID per HTTP request. Valid inbound metadata
// is preserved only when Trust explicitly accepts the immediate peer.
func (middleware *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if middleware == nil || next == nil || request == nil {
			http.Error(writer, "internal server error", http.StatusInternalServerError)
			return
		}
		inbound, err := middleware.codec.Extract(headerCarrier{request.Header})
		if err != nil && middleware.options.Invalid == RejectInvalid {
			http.Error(writer, "invalid correlation metadata", http.StatusBadRequest)
			return
		}
		if err != nil {
			inbound = correlation.Values{}
		}
		trusted := middleware.options.Trust != nil && middleware.options.Trust(request)
		values, err := middleware.factory.Accept(inbound, correlation.InboundPolicy{
			TrustCorrelation: trusted, TrustRequestAsCausation: trusted,
		})
		if err != nil {
			http.Error(writer, "internal server error", http.StatusInternalServerError)
			return
		}

		setHeaders(request.Header, values)
		setHeaders(writer.Header(), values)
		ctx := correlation.WithValues(request.Context(), values)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

// Inject creates and injects the next outbound HTTP hop.
func (middleware *Middleware) Inject(request *http.Request, parent correlation.Values) (correlation.Values, error) {
	if middleware == nil || request == nil {
		return correlation.Values{}, ErrInvalidOptions
	}
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	return middleware.propagator.Send(headerCarrier{request.Header}, parent)
}

type headerCarrier struct{ header http.Header }

func (carrier headerCarrier) Values(key string) []string {
	values := make([]string, 0, maxHeaderValues+1)
	for name, entries := range carrier.header {
		if strings.EqualFold(name, key) {
			for _, entry := range entries {
				values = append(values, entry)
				if len(values) > maxHeaderValues {
					return values
				}
			}
		}
	}
	return values
}

func (carrier headerCarrier) Set(key, value string) { carrier.header.Set(key, value) }

func setHeaders(header http.Header, values correlation.Values) {
	header.Set(CorrelationHeader, values.CorrelationID.String())
	header.Set(RequestHeader, values.RequestID.String())
	if values.CausationID != "" {
		header.Set(CausationHeader, values.CausationID.String())
	} else {
		header.Del(CausationHeader)
	}
}
