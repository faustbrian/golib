// Package tenancyhttp provides explicit tenant propagation for net/http.
// Tenant headers are accepted only when a configured trust function validates
// the immediate peer; header presence alone is never trusted.
package tenancyhttp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/faustbrian/golib/pkg/tenancy"
)

const (
	// DefaultHeader is the default HTTP tenant metadata field.
	DefaultHeader   = "X-Tenant-ID"
	maxHeaderValues = 8
)

var (
	// ErrInvalidOptions reports missing trust policy or invalid field names.
	ErrInvalidOptions = errors.New("tenancy http: invalid options")
	// ErrInvalidRequest reports a nil adapter or request.
	ErrInvalidRequest = errors.New("tenancy http: invalid request")
)

// ErrorHandler owns the HTTP response for rejected tenant metadata.
type ErrorHandler func(http.ResponseWriter, *http.Request, error)

// Options configure immutable extraction and rejection policy.
type Options struct {
	Header      string
	Trust       func(*http.Request) bool
	HandleError ErrorHandler
}

// Adapter extracts, injects, and installs tenant scope for HTTP requests.
type Adapter struct {
	codec       *tenancy.PropagationCodec
	header      string
	trust       func(*http.Request) bool
	handleError ErrorHandler
}

// New validates options. Trust is required even for callers that initially
// use only injection, preventing a later extraction path from trusting by
// presence.
func New(options Options) (*Adapter, error) {
	if options.Trust == nil {
		return nil, ErrInvalidOptions
	}
	header := options.Header
	if header == "" {
		header = DefaultHeader
	}
	codec, err := tenancy.NewPropagationCodec(tenancy.PropagationOptions{Field: header})
	if err != nil {
		return nil, ErrInvalidOptions
	}
	return &Adapter{
		codec: codec, header: header, trust: options.Trust, handleError: options.HandleError,
	}, nil
}

// Extract validates exactly one tenant header from a trusted immediate peer.
func (adapter *Adapter) Extract(request *http.Request) (tenancy.Scope, error) {
	if adapter == nil || adapter.codec == nil || adapter.trust == nil || request == nil {
		return tenancy.Scope{}, ErrInvalidRequest
	}
	return adapter.codec.Extract(headerCarrier{request.Header, adapter.header}, adapter.trust(request))
}

// Accept extracts tenant scope and derives from the request context.
func (adapter *Adapter) Accept(request *http.Request) (context.Context, error) {
	scope, err := adapter.Extract(request)
	if err != nil {
		return nil, err
	}
	return tenancy.WithScope(request.Context(), scope)
}

// Inject writes tenant scope to an outbound request and refuses overwrite.
func (adapter *Adapter) Inject(request *http.Request, scope tenancy.Scope) error {
	if adapter == nil || adapter.codec == nil || request == nil {
		return ErrInvalidRequest
	}
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	return adapter.codec.Inject(headerCarrier{request.Header, adapter.header}, scope)
}

// Wrap rejects invalid metadata before application code and installs trusted
// tenant scope while preserving the original request context.
func (adapter *Adapter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if adapter == nil || next == nil || request == nil {
			http.Error(writer, "invalid tenant boundary", http.StatusInternalServerError)
			return
		}
		ctx, err := adapter.Accept(request)
		if err != nil {
			if adapter.handleError != nil {
				adapter.handleError(writer, request, err)
				return
			}
			http.Error(writer, "invalid tenant metadata", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

type headerCarrier struct {
	header http.Header
	field  string
}

func (carrier headerCarrier) Values(_ string) []string {
	values := make([]string, 0, 1)
	for name, entries := range carrier.header {
		if !strings.EqualFold(name, carrier.field) {
			continue
		}
		for _, entry := range entries {
			values = append(values, entry)
			if len(values) > maxHeaderValues {
				return values
			}
		}
	}
	return values
}

func (carrier headerCarrier) Set(_ string, value string) {
	carrier.header.Set(carrier.field, value)
}
