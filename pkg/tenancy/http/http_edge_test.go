package tenancyhttp_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
	tenanthttp "github.com/faustbrian/golib/pkg/tenancy/http"
)

func TestMiddlewareUsesOwnedErrorHandlerAndRejectsInvalidHandler(t *testing.T) {
	t.Parallel()

	var captured error
	adapter, _ := tenanthttp.New(tenanthttp.Options{
		Trust: func(*http.Request) bool { return true },
		HandleError: func(writer http.ResponseWriter, _ *http.Request, err error) {
			captured = err
			writer.WriteHeader(http.StatusUnprocessableEntity)
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	adapter.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler called for missing metadata")
	})).ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !errors.Is(captured, tenancy.ErrTenantMetadataMissing) {
		t.Fatalf("custom rejection = %d, %v", response.Code, captured)
	}

	invalidResponse := httptest.NewRecorder()
	adapter.Wrap(nil).ServeHTTP(invalidResponse, request)
	if invalidResponse.Code != http.StatusInternalServerError {
		t.Fatalf("nil handler status = %d", invalidResponse.Code)
	}
}

func TestHTTPHeaderCarrierBoundsHostileDuplicates(t *testing.T) {
	t.Parallel()

	adapter, _ := tenanthttp.New(tenanthttp.Options{Trust: func(*http.Request) bool { return true }})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Unrelated", "value")
	request.Header[tenanthttp.DefaultHeader] = []string{
		"tenant-a", "tenant-a", "tenant-a", "tenant-a", "tenant-a",
		"tenant-a", "tenant-a", "tenant-a", "tenant-a", "tenant-a",
	}
	if _, err := adapter.Extract(request); !errors.Is(err, tenancy.ErrTenantMetadataOversized) {
		t.Fatalf("Extract(many) error = %v", err)
	}

	var nilAdapter *tenanthttp.Adapter
	if err := nilAdapter.Inject(request, tenancy.Scope{}); !errors.Is(err, tenanthttp.ErrInvalidRequest) {
		t.Fatalf("nil Inject() error = %v", err)
	}
}
