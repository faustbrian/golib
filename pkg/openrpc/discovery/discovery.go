// Package discovery provides transport-neutral OpenRPC service discovery.
// It owns neither JSON-RPC request execution nor any network transport.
package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

const (
	// MethodName is the service discovery method mandated by OpenRPC.
	MethodName = "rpc.discover"
)

var (
	// ErrInvalidOptions reports a missing provider or nil context.
	ErrInvalidOptions = errors.New("discovery: invalid options")
	// ErrProvider reports a provider failure without exposing its details.
	ErrProvider = errors.New("discovery: provider failed")
	// ErrFilter reports a visibility filter failure without exposing details.
	ErrFilter = errors.New("discovery: visibility filter failed")
	// ErrInvalidDocument reports a document that cannot be validated or
	// canonically serialized.
	ErrInvalidDocument = errors.New("discovery: invalid OpenRPC document")
	// ErrDiscoveryLimit reports canonical output beyond the configured bound.
	ErrDiscoveryLimit = errors.New("discovery: output limit exceeded")
)

// Options bounds discovery validation and canonical output.
type Options struct {
	MaxOutputBytes int
	Validation     validate.Options
}

// DefaultOptions returns finite discovery output and diagnostic bounds.
func DefaultOptions() Options {
	return Options{
		MaxOutputBytes: 64 << 20,
		Validation:     validate.DefaultOptions(),
	}
}

// Provider supplies a static or dynamically generated OpenRPC document.
type Provider interface {
	Document(context.Context) (openrpc.Document, error)
}

// ProviderFunc adapts a function to Provider.
type ProviderFunc func(context.Context) (openrpc.Document, error)

// Document implements Provider.
func (function ProviderFunc) Document(ctx context.Context) (openrpc.Document, error) {
	return function(ctx)
}

type staticProvider struct {
	document openrpc.Document
}

// Static returns a provider for one immutable document.
func Static(document openrpc.Document) Provider {
	return staticProvider{document: document}
}

func (provider staticProvider) Document(context.Context) (openrpc.Document, error) {
	return provider.document, nil
}

// Filter applies caller-owned authorization or deployment visibility policy.
// Returning an empty method list is valid.
type Filter interface {
	Filter(context.Context, openrpc.Document) (openrpc.Document, error)
}

// FilterFunc adapts a function to Filter.
type FilterFunc func(context.Context, openrpc.Document) (openrpc.Document, error)

// Filter implements Filter.
func (function FilterFunc) Filter(ctx context.Context, document openrpc.Document) (openrpc.Document, error) {
	return function(ctx, document)
}

// Service coordinates provider, visibility, validation, and canonical output.
// It retains no cache and starts no goroutines.
type Service struct {
	provider Provider
	filter   Filter
	options  Options
}

// NewService constructs a discovery service.
func NewService(provider Provider, filter Filter) (*Service, error) {
	return NewServiceWithOptions(provider, filter, DefaultOptions())
}

// NewServiceWithOptions constructs a discovery service with explicit bounds.
func NewServiceWithOptions(provider Provider, filter Filter, options Options) (*Service, error) {
	if provider == nil || options.MaxOutputBytes <= 0 ||
		options.Validation.MaxDiagnostics <= 0 ||
		(options.Validation.Mode != validate.Collect && options.Validation.Mode != validate.FailFast) {
		return nil, ErrInvalidOptions
	}
	return &Service{provider: provider, filter: filter, options: options}, nil
}

// Snapshot is immutable canonical discovery output with a content revision.
type Snapshot struct {
	document openrpc.Document
	bytes    []byte
	revision string
}

// Document returns the immutable discovered document.
func (snapshot Snapshot) Document() openrpc.Document { return snapshot.document }

// Bytes returns an owned canonical JSON snapshot.
func (snapshot Snapshot) Bytes() []byte {
	return append([]byte(nil), snapshot.bytes...)
}

// Revision returns the lowercase SHA-256 content fingerprint.
func (snapshot Snapshot) Revision() string { return snapshot.revision }

// ETag returns the strong HTTP entity tag corresponding to Revision.
func (snapshot Snapshot) ETag() string { return `"` + snapshot.revision + `"` }

// Discover produces one validated, context-filtered canonical snapshot.
func (service *Service) Discover(ctx context.Context) (Snapshot, error) {
	if service == nil || service.provider == nil || ctx == nil {
		return Snapshot{}, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	document, err := service.provider.Document(ctx)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return Snapshot{}, contextErr
		}
		return Snapshot{}, ErrProvider
	}
	if service.filter != nil {
		document, err = service.filter.Filter(ctx, document)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return Snapshot{}, contextErr
			}
			return Snapshot{}, ErrFilter
		}
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	report := validate.Document(ctx, document, service.options.Validation)
	if !report.Valid() {
		return Snapshot{}, ErrInvalidDocument
	}
	encoded, err := openrpc.MarshalCanonical(document)
	if err != nil {
		return Snapshot{}, ErrInvalidDocument
	}
	if len(encoded) > service.options.MaxOutputBytes {
		return Snapshot{}, ErrDiscoveryLimit
	}
	fingerprint := sha256.Sum256(encoded)
	return Snapshot{
		document: document,
		bytes:    append([]byte(nil), encoded...),
		revision: hex.EncodeToString(fingerprint[:]),
	}, nil
}
