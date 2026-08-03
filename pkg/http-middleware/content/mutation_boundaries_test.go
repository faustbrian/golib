package content

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExactContentPolicyBoundsAreAccepted(t *testing.T) {
	t.Parallel()

	maximumTypes := make([]string, 256)
	for index := range maximumTypes {
		maximumTypes[index] = "text/plain"
	}
	for name, policy := range map[string]Policy{
		"minimum values":       {MaxValues: 1},
		"maximum values":       {MaxValues: 256, RequestTypes: maximumTypes},
		"minimum header bytes": {MaxHeaderBytes: 1},
		"maximum header bytes": {MaxHeaderBytes: 1 << 20},
		"request values":       {MaxValues: 1, RequestTypes: []string{"a/b"}},
		"response values":      {MaxValues: 1, ResponseTypes: []string{"a/b"}},
		"shared byte budget": {
			MaxValues: 1, MaxHeaderBytes: 6,
			RequestTypes: []string{"a/b"}, ResponseTypes: []string{"c/d"},
		},
	} {
		if _, err := New(policy); err != nil {
			t.Fatalf("New(%s exact bound) error = %v", name, err)
		}
	}
}

func TestConfiguredMediaTypesShareTheByteBudget(t *testing.T) {
	t.Parallel()

	_, err := New(Policy{
		MaxValues: 1, MaxHeaderBytes: 5,
		RequestTypes: []string{"a/b"}, ResponseTypes: []string{"c/d"},
	})
	if err == nil {
		t.Fatal("cumulative configured media-type bytes were accepted")
	}
}

func TestUnconfiguredGuardsDoNotInspectRequestHeaders(t *testing.T) {
	t.Parallel()

	middleware, err := New(Policy{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	request.ContentLength = 1
	request.Header.Set("Content-Type", "invalid")
	request.Header.Set("Accept", "invalid;")
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestMediaTypeMatchingOperandsAreIndependent(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		candidate string
		supported []string
		want      bool
	}{
		"supported wildcard": {candidate: "text/plain", supported: []string{"*/*"}, want: true},
		"candidate wildcard": {candidate: "*/json", supported: []string{"application/json"}, want: true},
		"exact":              {candidate: "application/json", supported: []string{"application/json"}, want: true},
		"major mismatch":     {candidate: "text/json", supported: []string{"application/json"}},
		"minor mismatch":     {candidate: "application/xml", supported: []string{"application/json"}},
	} {
		if got := matchesAny(test.candidate, test.supported); got != test.want {
			t.Fatalf("%s match = %v, want %v", name, got, test.want)
		}
	}
}

func TestAcceptBudgetsAccumulateAcrossHeaderLines(t *testing.T) {
	t.Parallel()

	supported := []string{"application/json"}
	exactLines := []string{"text/plain", "application/json"}
	if !acceptable(exactLines, supported, 2, len(exactLines[0])+len(exactLines[1])) {
		t.Fatal("exact cumulative Accept budget was rejected")
	}
	if acceptable(exactLines, supported, 2, len(exactLines[0])+len(exactLines[1])-1) {
		t.Fatal("cumulative Accept byte budget was not enforced")
	}
	if acceptable([]string{"text/plain", "text/html", "application/json"}, supported, 2, 128) {
		t.Fatal("cumulative Accept item budget was not enforced")
	}
}

func TestMediaRangeRequiresSlashAndBothComponents(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"application", "/json", "application/"} {
		if validMediaRange(value, true) {
			t.Fatalf("validMediaRange(%q) = true", value)
		}
	}
	if !validMediaRange("application/json", true) {
		t.Fatal("valid media range was rejected")
	}
}
