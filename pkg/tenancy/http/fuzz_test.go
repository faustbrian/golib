package tenancyhttp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
	tenancyhttp "github.com/faustbrian/golib/pkg/tenancy/http"
)

func FuzzHTTPHeaderExtraction(f *testing.F) {
	f.Add("tenant-a", "tenant-b", uint8(1), true)
	f.Add("tenant-a", "tenant-a", uint8(2), true)
	f.Add("bad tenant", "tenant-b", uint8(1), true)
	f.Add("tenant-a", "tenant-b", uint8(2), false)
	f.Fuzz(func(t *testing.T, first, second string, count uint8, trusted bool) {
		adapter, err := tenancyhttp.New(tenancyhttp.Options{
			Trust: func(*http.Request) bool { return trusted },
		})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		values := int(count % 10)
		if values > 0 {
			request.Header[tenancyhttp.DefaultHeader] = []string{first}
		}
		if values > 1 {
			request.Header["x-tenant-id"] = append([]string{second}, make([]string, values-2)...)
		}
		scope, extractErr := adapter.Extract(request)
		if extractErr == nil {
			parsed, parseErr := tenancy.ParseTenantID(first)
			if !trusted || values != 1 || parseErr != nil || !scope.TenantID().Equal(parsed) {
				t.Fatalf("accepted hostile headers: values=%d trusted=%t scope=%#v", values, trusted, scope)
			}
		}
	})
}
