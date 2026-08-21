package tenancyhttp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
	tenanthttp "github.com/faustbrian/golib/pkg/tenancy/http"
)

func TestMiddlewareAcceptsOnlyExplicitlyTrustedTenantHeader(t *testing.T) {
	t.Parallel()

	adapter, err := tenanthttp.New(tenanthttp.Options{
		Trust: func(request *http.Request) bool {
			return request.RemoteAddr == "trusted-proxy"
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	handler := adapter.Wrap(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := tenancy.AssertTenant(request.Context(), tenancy.MustTenantID("tenant-a")); err != nil {
			t.Fatalf("tenant context error = %v", err)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))

	trusted := httptest.NewRequest(http.MethodGet, "/", nil)
	trusted.RemoteAddr = "trusted-proxy"
	trusted.Header.Set(tenanthttp.DefaultHeader, "tenant-a")
	trustedResponse := httptest.NewRecorder()
	handler.ServeHTTP(trustedResponse, trusted)
	if trustedResponse.Code != http.StatusNoContent {
		t.Fatalf("trusted status = %d", trustedResponse.Code)
	}

	spoofed := httptest.NewRequest(http.MethodGet, "/", nil)
	spoofed.RemoteAddr = "direct-client"
	spoofed.Header.Set(tenanthttp.DefaultHeader, "tenant-a")
	spoofedResponse := httptest.NewRecorder()
	handler.ServeHTTP(spoofedResponse, spoofed)
	if spoofedResponse.Code != http.StatusBadRequest {
		t.Fatalf("spoofed status = %d", spoofedResponse.Code)
	}
}

func TestHTTPExtractionRejectsDuplicateCaseVariants(t *testing.T) {
	t.Parallel()

	adapter, _ := tenanthttp.New(tenanthttp.Options{Trust: func(*http.Request) bool { return true }})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header[tenanthttp.DefaultHeader] = []string{"tenant-a"}
	request.Header["x-tenant-id"] = []string{"tenant-b"}
	if _, err := adapter.Extract(request); !errors.Is(err, tenancy.ErrTenantMetadataConflicting) {
		t.Fatalf("Extract() error = %v", err)
	}
}

func TestHTTPExtractionIgnoresUnrelatedHeaders(t *testing.T) {
	t.Parallel()

	adapter, _ := tenanthttp.New(tenanthttp.Options{Trust: func(*http.Request) bool { return true }})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Unrelated-Tenant-Hint", "tenant-b")
	request.Header.Set(tenanthttp.DefaultHeader, "tenant-a")

	scope, err := adapter.Extract(request)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if got := scope.TenantID(); !got.Equal(tenancy.MustTenantID("tenant-a")) {
		t.Fatalf("tenant ID = %q", got)
	}
}

func TestHTTPInjectionRequiresTenantScopeAndRefusesOverwrite(t *testing.T) {
	t.Parallel()

	adapter, _ := tenanthttp.New(tenanthttp.Options{Trust: func(*http.Request) bool { return true }})
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	request := httptest.NewRequest(http.MethodGet, "https://service.example/", nil)
	request.Header = nil
	if err := adapter.Inject(request, scope); err != nil {
		t.Fatalf("Inject() error = %v", err)
	}
	if got := request.Header.Get(tenanthttp.DefaultHeader); got != "tenant-a" {
		t.Fatalf("header = %q", got)
	}
	if err := adapter.Inject(request, scope); !errors.Is(err, tenancy.ErrTenantMetadataOverwrite) {
		t.Fatalf("Inject(overwrite) error = %v", err)
	}

	reason, _ := tenancy.NewAdministrativeReason("operator", "maintenance", "")
	system, _ := tenancy.NewSystemScope(tenancy.NewSystemCapability(reason), tenancy.Metadata{})
	if err := adapter.Inject(httptest.NewRequest(http.MethodGet, "/", nil), system); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("Inject(system) error = %v", err)
	}
}

func TestHTTPAdapterValidatesConfigurationAndInputs(t *testing.T) {
	t.Parallel()

	if _, err := tenanthttp.New(tenanthttp.Options{}); !errors.Is(err, tenanthttp.ErrInvalidOptions) {
		t.Fatalf("New(no trust) error = %v", err)
	}
	if _, err := tenanthttp.New(tenanthttp.Options{Header: "bad header", Trust: func(*http.Request) bool { return true }}); !errors.Is(err, tenanthttp.ErrInvalidOptions) {
		t.Fatalf("New(bad header) error = %v", err)
	}
	adapter, _ := tenanthttp.New(tenanthttp.Options{Trust: func(*http.Request) bool { return true }})
	if _, err := adapter.Extract(nil); !errors.Is(err, tenanthttp.ErrInvalidRequest) {
		t.Fatalf("Extract(nil) error = %v", err)
	}
	if err := adapter.Inject(nil, tenancy.Scope{}); !errors.Is(err, tenanthttp.ErrInvalidRequest) {
		t.Fatalf("Inject(nil) error = %v", err)
	}
	var nilAdapter *tenanthttp.Adapter
	if _, err := nilAdapter.Extract(httptest.NewRequest(http.MethodGet, "/", nil)); !errors.Is(err, tenanthttp.ErrInvalidRequest) {
		t.Fatalf("nil Extract() error = %v", err)
	}

	type key struct{}
	request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(
		context.WithValue(context.Background(), key{}, "retained"),
	)
	request.Header.Set(tenanthttp.DefaultHeader, "tenant-a")
	ctx, err := adapter.Accept(request)
	if err != nil || ctx.Value(key{}) != "retained" {
		t.Fatalf("Accept() = %v, value %v", err, ctx.Value(key{}))
	}
}
