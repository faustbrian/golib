package kubernetes

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// ErrInvalidTenantDocument is a stable Kubernetes mapping error.
var ErrInvalidTenantDocument = errors.New("kubernetes: invalid tenant document")

// DeploymentFactory returns the restricted Deployment client for one
// namespace.
type DeploymentFactory func(namespace string) DeploymentClient

type tenantDocument struct {
	Tenants []tenantBinding `json:"tenants"`
}

type tenantBinding struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
}

// LoadTenantResolver builds immutable namespace-scoped adapters from one
// strict, bounded tenant document.
func LoadTenantResolver(
	reader io.Reader,
	maxBytes int64,
	factory DeploymentFactory,
) (*StaticTenantResolver, error) {
	if nilInterface(reader) || maxBytes < 1 || factory == nil {
		return nil, ErrInvalidTenantDocument
	}
	encoded, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil || int64(len(encoded)) > maxBytes {
		return nil, ErrInvalidTenantDocument
	}

	var document tenantDocument
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, ErrInvalidTenantDocument
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidTenantDocument
	}

	adapters := make(map[string]TenantAdapter, len(document.Tenants))
	for _, binding := range document.Tenants {
		if _, duplicate := adapters[binding.ID]; duplicate {
			return nil, ErrInvalidTenantDocument
		}
		adapter, err := New(binding.Namespace, factory(binding.Namespace))
		if err != nil {
			return nil, ErrInvalidTenantDocument
		}
		adapters[binding.ID] = adapter
	}
	resolver, err := NewStaticTenantResolver(adapters)
	if err != nil {
		return nil, ErrInvalidTenantDocument
	}

	return resolver, nil
}
