package requestidbridge_test

import (
	"context"
	"errors"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/correlation/http/requestidbridge"
)

func nilContext() context.Context { return nil }

func TestAdoptRequiresExplicitTrustAndRejectsOverwrite(t *testing.T) {
	lookup := func(context.Context) (string, bool) { return "middleware-request", true }
	values := correlation.Values{CorrelationID: correlation.MustCorrelationID("flow", correlation.Policy{})}

	if _, _, err := requestidbridge.Adopt(context.Background(), values, lookup, requestidbridge.Options{}); !errors.Is(err, requestidbridge.ErrUntrusted) {
		t.Fatalf("untrusted Adopt() error = %v", err)
	}
	ctx, adopted, err := requestidbridge.Adopt(context.Background(), values, lookup, requestidbridge.Options{Trusted: true})
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := correlation.FromContext(ctx)
	if !ok || stored != adopted || adopted.RequestID.String() != "middleware-request" {
		t.Fatalf("adopted = %#v, stored = %#v, %v", adopted, stored, ok)
	}

	values.RequestID = correlation.MustRequestID("existing", correlation.Policy{})
	if _, _, err := requestidbridge.Adopt(context.Background(), values, lookup, requestidbridge.Options{Trusted: true}); !errors.Is(err, requestidbridge.ErrOverwrite) {
		t.Fatalf("overwrite Adopt() error = %v", err)
	}
}

func TestAdoptRejectsMissingMalformedAndNilLookups(t *testing.T) {
	if _, _, err := requestidbridge.Adopt(nilContext(), correlation.Values{}, func(context.Context) (string, bool) { return "id", true }, requestidbridge.Options{Trusted: true}); !errors.Is(err, requestidbridge.ErrInvalidLookup) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, _, err := requestidbridge.Adopt(context.Background(), correlation.Values{}, nil, requestidbridge.Options{Trusted: true}); !errors.Is(err, requestidbridge.ErrInvalidLookup) {
		t.Fatalf("nil lookup error = %v", err)
	}
	missing := func(context.Context) (string, bool) { return "", false }
	if _, _, err := requestidbridge.Adopt(context.Background(), correlation.Values{}, missing, requestidbridge.Options{Trusted: true}); !errors.Is(err, requestidbridge.ErrMissing) {
		t.Fatalf("missing lookup error = %v", err)
	}
	malformed := func(context.Context) (string, bool) { return "bad value", true }
	if _, _, err := requestidbridge.Adopt(context.Background(), correlation.Values{}, malformed, requestidbridge.Options{Trusted: true}); !errors.Is(err, requestidbridge.ErrInvalidLookup) {
		t.Fatalf("malformed lookup error = %v", err)
	}
}
