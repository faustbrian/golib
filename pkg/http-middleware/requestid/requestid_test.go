package requestid_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/requestid"
)

func TestUntrustedInboundIdentifierIsReplacedAndPropagated(t *testing.T) {
	t.Parallel()

	middleware, err := requestid.New(requestid.Policy{
		Kind:      requestid.Request,
		Generator: func() (string, error) { return "generated", nil },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identifier, ok := requestid.FromContext(r.Context(), requestid.Request)
		if !ok || identifier != "generated" {
			t.Fatalf("identifier = %q, %v", identifier, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "untrusted")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got := recorder.Header().Get("X-Request-ID"); got != "generated" {
		t.Fatalf("response identifier = %q", got)
	}
}

func TestTrustedInvalidIdentifiersFollowNamedPolicy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		policy requestid.InvalidPolicy
		status int
		wantID string
	}{
		{name: "replace", policy: requestid.ReplaceInvalid, status: http.StatusNoContent, wantID: "replacement"},
		{name: "reject", policy: requestid.RejectInvalid, status: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			middleware, err := requestid.New(requestid.Policy{
				Kind:         requestid.Request,
				TrustInbound: true,
				Invalid:      tc.policy,
				MaxLength:    16,
				Generator:    func() (string, error) { return "replacement", nil },
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header["X-Request-Id"] = []string{"one", "two"}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.status)
			}
			if got := recorder.Header().Get("X-Request-ID"); got != tc.wantID {
				t.Fatalf("response identifier = %q, want %q", got, tc.wantID)
			}
		})
	}
}

func TestGeneratorFailureProducesSafeResponse(t *testing.T) {
	t.Parallel()

	middleware, err := requestid.New(requestid.Policy{
		Kind: requestid.Correlation,
		Generator: func() (string, error) {
			return "", errors.New("secret generator detail")
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run")
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusInternalServerError || recorder.Body.String() != "internal server error\n" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}
