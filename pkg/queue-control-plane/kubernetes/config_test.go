package kubernetes

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestLoadTenantResolverBuildsNamespaceScopedAdapters(t *testing.T) {
	t.Parallel()

	document := `{"tenants":[{"id":"tenant-1","namespace":"workers-one"},{"id":"tenant-2","namespace":"workers-two"}]}`
	namespaces := make([]string, 0, 2)
	resolver, err := LoadTenantResolver(strings.NewReader(document), 1024, func(namespace string) DeploymentClient {
		namespaces = append(namespaces, namespace)

		return &deploymentClient{}
	})
	if err != nil || resolver == nil {
		t.Fatalf("LoadTenantResolver() = (%v, %v), want resolver and nil", resolver, err)
	}
	if strings.Join(namespaces, ",") != "workers-one,workers-two" {
		t.Fatalf("factory namespaces = %v, want document order", namespaces)
	}
	for _, tenant := range []string{"tenant-1", "tenant-2"} {
		if _, err := resolver.ResolveTenant(t.Context(), tenant); err != nil {
			t.Fatalf("ResolveTenant(%q) error = %v", tenant, err)
		}
	}
}

func TestLoadTenantResolverRejectsUnsafeDocuments(t *testing.T) {
	t.Parallel()

	validFactory := func(string) DeploymentClient { return &deploymentClient{} }
	var typedNil *strings.Reader
	tests := []struct {
		reader  io.Reader
		limit   int64
		factory DeploymentFactory
	}{
		{},
		{reader: typedNil, limit: 100, factory: validFactory},
		{reader: strings.NewReader(`{}`), limit: 100, factory: validFactory},
		{reader: strings.NewReader(`{"tenants":[]}`), limit: 100, factory: validFactory},
		{reader: strings.NewReader(`{"unknown":[]}`), limit: 100, factory: validFactory},
		{reader: strings.NewReader(`{"tenants":[]}{}`), limit: 100, factory: validFactory},
		{reader: strings.NewReader(`{"tenants":[{"id":"tenant-1","namespace":"workers"}]}`), limit: 8, factory: validFactory},
		{reader: failingTenantReader{}, limit: 100, factory: validFactory},
		{reader: strings.NewReader(`{"tenants":[{"id":"tenant-1","namespace":"workers"},{"id":"tenant-1","namespace":"other"}]}`), limit: 1000, factory: validFactory},
		{reader: strings.NewReader(`{"tenants":[{"id":"tenant-1","namespace":"INVALID"}]}`), limit: 1000, factory: validFactory},
		{reader: strings.NewReader(`{"tenants":[{"id":"tenant-1","namespace":"workers"}]}`), limit: 1000},
		{
			reader: strings.NewReader(`{"tenants":[{"id":"tenant-1","namespace":"workers"}]}`),
			limit:  1000,
			factory: func(string) DeploymentClient {
				var client *deploymentClient

				return client
			},
		},
	}
	for _, test := range tests {
		resolver, err := LoadTenantResolver(test.reader, test.limit, test.factory)
		if resolver != nil || !errors.Is(err, ErrInvalidTenantDocument) {
			t.Fatalf("LoadTenantResolver() = (%v, %v), want nil and stable error", resolver, err)
		}
	}
}

type failingTenantReader struct{}

func (failingTenantReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}
