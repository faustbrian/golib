package bodylimit

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigurationErrorAndExactLimit(t *testing.T) {
	t.Parallel()
	_, err := New(Policy{})
	var config *ConfigError
	if !errors.As(err, &config) || !errors.Is(err, ErrInvalidPolicy) || config.Error() == "" {
		t.Fatalf("error = %v", err)
	}
	middleware, _ := New(Policy{MaxBytes: 4})
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("1234"))
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		payload, readErr := io.ReadAll(r.Body)
		if readErr != nil || string(payload) != "1234" {
			t.Fatalf("read = %q, %v", payload, readErr)
		}
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
}
