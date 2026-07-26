package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/faustbrian/golib/pkg/http-middleware/admission"
	"github.com/faustbrian/golib/pkg/http-middleware/deadline"
)

func TestNoLeaks(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	timeout, _ := deadline.NewTimeout(deadline.TimeoutPolicy{Timeout: time.Millisecond, MaxResponseBytes: 64})
	timeout(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	limit, _ := admission.New(admission.Policy{MaxInFlight: 1})
	limit(http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
