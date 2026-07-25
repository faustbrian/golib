// Package jsonrpc adapts transport-neutral OpenRPC discovery to the handler
// signature used by github.com/faustbrian/golib/pkg/jsonrpc without coupling the core
// model, parser, or validator to that server implementation.
package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/faustbrian/golib/pkg/openrpc/discovery"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

var (
	// ErrInvalidAdapter reports a nil discoverer or context.
	ErrInvalidAdapter = errors.New("openrpc jsonrpc adapter: invalid options")
	// ErrInvalidParams reports non-empty or malformed rpc.discover params.
	ErrInvalidParams = errors.New("openrpc jsonrpc adapter: invalid params")
	// ErrDiscovery reports a sanitized discovery provider failure.
	ErrDiscovery = errors.New("openrpc jsonrpc adapter: discovery failed")
	// ErrRegistration reports a sanitized system-method registration failure.
	ErrRegistration = errors.New("openrpc jsonrpc adapter: registration failed")
)

// Handler matches the jsonrpc handler call shape. It can be explicitly
// converted to that package's named Handler type by an integration package.
type Handler func(context.Context, json.RawMessage) (any, error)

// SystemRegistrar is the narrow server contract needed to reserve and
// register rpc.discover. H permits adapters to retain their named handler
// type without introducing a package dependency here.
type SystemRegistrar[H ~func(context.Context, json.RawMessage) (any, error)] interface {
	RegisterSystem(string, H) error
}

// RegisterDiscovery installs rpc.discover as a reserved system method.
// Registration failures are deliberately sanitized at this package boundary.
func RegisterDiscovery[H ~func(context.Context, json.RawMessage) (any, error)](
	registrar SystemRegistrar[H],
	discoverer discovery.Discoverer,
) error {
	if registrar == nil {
		return ErrInvalidAdapter
	}
	handler, err := DiscoveryHandler(discoverer)
	if err != nil {
		return err
	}
	if err := registrar.RegisterSystem(discovery.MethodName, H(handler)); err != nil {
		return ErrRegistration
	}
	return nil
}

// DiscoveryHandler returns an rpc.discover handler. JSON-RPC request IDs,
// notifications, batches, error envelopes, and transports remain owned by the
// JSON-RPC server. The result is json.RawMessage so its document object is
// embedded rather than encoded as an opaque Go struct.
func DiscoveryHandler(discoverer discovery.Discoverer) (Handler, error) {
	if discoverer == nil {
		return nil, ErrInvalidAdapter
	}
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		if ctx == nil {
			return nil, ErrInvalidAdapter
		}
		if err := validEmptyParams(params); err != nil {
			return nil, err
		}
		snapshot, err := discoverer.Discover(ctx)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return nil, contextErr
			}
			return nil, ErrDiscovery
		}
		return json.RawMessage(snapshot.Bytes()), nil
	}, nil
}

func validEmptyParams(params json.RawMessage) error {
	if len(bytes.TrimSpace(params)) == 0 {
		return nil
	}
	policy := jsonvalue.Policy{MaxBytes: 1 << 10, MaxDepth: 4, MaxTokens: 32}
	value, err := jsonvalue.Parse(params, policy)
	if err != nil {
		return ErrInvalidParams
	}
	trimmed := bytes.TrimSpace(value.Bytes())
	switch trimmed[0] {
	case '[':
		var positional []json.RawMessage
		if json.Unmarshal(trimmed, &positional) != nil || len(positional) != 0 {
			return ErrInvalidParams
		}
	case '{':
		var named map[string]json.RawMessage
		if json.Unmarshal(trimmed, &named) != nil || len(named) != 0 {
			return ErrInvalidParams
		}
	default:
		return ErrInvalidParams
	}
	return nil
}
