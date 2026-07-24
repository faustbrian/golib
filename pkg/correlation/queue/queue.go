// Package queue propagates correlation metadata through backend-neutral queue
// metadata maps. Each Receive call creates a distinct delivery-attempt ID.
package queue

import (
	"errors"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

// ErrInvalidOptions reports invalid queue propagation configuration.
var ErrInvalidOptions = errors.New("queue correlation: invalid options")

// Options configure queue metadata validation and field names.
type Options struct {
	Codec correlation.CodecOptions
}

// Adapter explicitly sends and receives queue metadata.
type Adapter struct{ propagator *correlation.Propagator }

// New constructs a queue adapter.
func New(factory *correlation.Factory, options Options) (*Adapter, error) {
	if factory == nil {
		return nil, ErrInvalidOptions
	}
	codec, err := correlation.NewCodec(options.Codec)
	if err != nil {
		return nil, err
	}
	propagator, _ := correlation.NewPropagator(factory, codec)
	return &Adapter{propagator: propagator}, nil
}

// Send creates a message hop and injects it into an application-owned map.
func (adapter *Adapter) Send(metadata map[string]string, parent correlation.Values) (correlation.Values, error) {
	if adapter == nil || metadata == nil {
		return correlation.Values{}, ErrInvalidOptions
	}
	return adapter.propagator.Send(mapCarrier(metadata), parent)
}

// Receive creates a fresh delivery-attempt ID. trusted must be explicitly true
// only after the queue backend's metadata boundary has been authenticated.
func (adapter *Adapter) Receive(metadata map[string]string, trusted bool) (correlation.Values, error) {
	if adapter == nil {
		return correlation.Values{}, ErrInvalidOptions
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	return adapter.propagator.Receive(mapCarrier(metadata), correlation.InboundPolicy{
		TrustCorrelation: trusted, TrustRequestAsCausation: trusted,
	})
}

type mapCarrier map[string]string

func (carrier mapCarrier) Values(key string) []string {
	value, ok := carrier[key]
	if !ok {
		return nil
	}
	return []string{value}
}

func (carrier mapCarrier) Set(key, value string) { carrier[key] = value }
