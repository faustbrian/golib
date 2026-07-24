package log_test

import (
	"errors"
	"log/slog"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	correlationlog "github.com/faustbrian/golib/pkg/correlation/log"
)

func TestAttrsRequireExplicitDisclosure(t *testing.T) {
	values := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("private-flow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("private-request", correlation.Policy{}),
	}
	redacted, err := correlationlog.Attrs(values, correlation.DisclosurePolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if got := attrValue(redacted, "correlation.id"); got != "[redacted]" {
		t.Fatalf("default correlation attr = %q", got)
	}

	hashed, err := correlationlog.Attrs(values, correlation.DisclosurePolicy{
		Mode: correlation.HashDisclosure, Key: []byte("log-key"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := attrValue(hashed, "correlation.id"); got == "private-flow" || got == "[redacted]" || len(got) != 22 {
		t.Fatalf("hashed correlation attr = %q", got)
	}

	exposed, err := correlationlog.Attrs(values, correlation.DisclosurePolicy{Mode: correlation.ExposeDisclosure})
	if err != nil {
		t.Fatal(err)
	}
	if got := attrValue(exposed, "request.id"); got != "private-request" {
		t.Fatalf("exposed request attr = %q", got)
	}
}

func TestAttrsRejectInvalidDisclosure(t *testing.T) {
	_, err := correlationlog.Attrs(correlation.Values{CorrelationID: "flow"}, correlation.DisclosurePolicy{Mode: correlation.HashDisclosure})
	if !errors.Is(err, correlation.ErrInvalidDisclosure) {
		t.Fatalf("Attrs() error = %v", err)
	}
}

func attrValue(attrs []slog.Attr, key string) string {
	for _, attr := range attrs {
		if attr.Key == key {
			return attr.Value.String()
		}
	}
	return ""
}
