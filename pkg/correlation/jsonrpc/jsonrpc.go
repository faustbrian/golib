// Package jsonrpc propagates correlation through an explicit JSON-RPC
// metadata object. It never edits JSON-RPC protocol envelopes implicitly.
package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

const (
	// CorrelationField carries the logical workflow identifier.
	CorrelationField = correlation.DefaultCorrelationField
	// RequestField carries the current JSON-RPC hop identifier.
	RequestField = correlation.DefaultRequestField
	// CausationField carries the immediate parent identifier.
	CausationField    = correlation.DefaultCausationField
	maxMetadataValues = 8
	maxEncodedIDBytes = 1026
)

var (
	// ErrInvalidOptions reports incomplete adapter configuration.
	ErrInvalidOptions = errors.New("jsonrpc correlation: invalid options")
	// ErrMalformedMetadata reports non-string or oversized JSON metadata.
	ErrMalformedMetadata = errors.New("jsonrpc correlation: malformed metadata")
)

// Metadata preserves repeated JSON members supplied by a strict envelope
// decoder so conflicting duplicates cannot be silently collapsed.
type Metadata map[string][]json.RawMessage

// Options configure JSON-RPC metadata validation and names.
type Options struct {
	Codec correlation.CodecOptions
}

// Adapter explicitly sends and receives JSON-RPC metadata.
type Adapter struct {
	propagator *correlation.Propagator
	fields     [3]string
}

// New constructs a JSON-RPC metadata adapter.
func New(factory *correlation.Factory, options Options) (*Adapter, error) {
	if factory == nil {
		return nil, ErrInvalidOptions
	}
	codec, err := correlation.NewCodec(options.Codec)
	if err != nil {
		return nil, err
	}
	if options.Codec.CorrelationField == "" {
		options.Codec.CorrelationField = CorrelationField
	}
	if options.Codec.RequestField == "" {
		options.Codec.RequestField = RequestField
	}
	if options.Codec.CausationField == "" {
		options.Codec.CausationField = CausationField
	}
	propagator, _ := correlation.NewPropagator(factory, codec)
	return &Adapter{
		propagator: propagator,
		fields: [3]string{
			options.Codec.CorrelationField,
			options.Codec.RequestField,
			options.Codec.CausationField,
		},
	}, nil
}

// Send creates and injects a child JSON-RPC metadata hop.
func (adapter *Adapter) Send(metadata Metadata, parent correlation.Values) (correlation.Values, error) {
	if adapter == nil || metadata == nil {
		return correlation.Values{}, ErrInvalidOptions
	}
	return adapter.propagator.Send(rawCarrier(metadata), parent)
}

// Receive validates metadata and creates a fresh receiving request ID.
func (adapter *Adapter) Receive(metadata Metadata, trusted bool) (correlation.Values, error) {
	if adapter == nil {
		return correlation.Values{}, ErrInvalidOptions
	}
	if metadata == nil {
		metadata = Metadata{}
	}
	for _, field := range adapter.fields {
		if len(metadata[field]) > maxMetadataValues {
			return correlation.Values{}, fmt.Errorf("%w: too many %s values", correlation.ErrInvalidCarrier, field)
		}
		for _, raw := range metadata[field] {
			var value string
			if len(raw) > maxEncodedIDBytes || json.Unmarshal(raw, &value) != nil {
				return correlation.Values{}, fmt.Errorf("%w: %s", ErrMalformedMetadata, field)
			}
		}
	}
	return adapter.propagator.Receive(rawCarrier(metadata), correlation.InboundPolicy{
		TrustCorrelation: trusted, TrustRequestAsCausation: trusted,
	})
}

type rawCarrier Metadata

func (carrier rawCarrier) Values(key string) []string {
	rawValues := carrier[key]
	if len(rawValues) > maxMetadataValues {
		rawValues = rawValues[:maxMetadataValues+1]
	}
	values := make([]string, 0, len(rawValues))
	for _, raw := range rawValues {
		var value string
		_ = json.Unmarshal(raw, &value)
		values = append(values, value)
	}
	return values
}

func (carrier rawCarrier) Set(key, value string) {
	raw, _ := json.Marshal(value)
	carrier[key] = []json.RawMessage{raw}
}
