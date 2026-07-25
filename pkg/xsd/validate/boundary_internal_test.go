package validate

import (
	"context"
	"errors"
	"testing"
)

func TestNewRejectsMissingSetAndNegativeLimits(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, Options{}); err == nil {
		t.Fatal("New(nil) succeeded")
	}
	set := attributeValidationSet(t)
	for _, limits := range []Limits{
		{MaxBytes: -1},
		{MaxDepth: -1},
		{MaxTextBytes: -1},
		{MaxDiagnostics: -1},
		{MaxNodes: -1},
		{MaxAttributes: -1},
		{MaxXPathSteps: -1},
		{MaxIdentityValues: -1},
	} {
		if _, err := New(set, Options{Limits: limits}); err == nil {
			t.Fatalf("New(%#v) succeeded", limits)
		}
	}
}

func TestParseInstanceEnforcesEveryResourceBoundary(t *testing.T) {
	t.Parallel()

	set := attributeValidationSet(t)
	for _, test := range []struct {
		name   string
		source string
		limits Limits
		limit  bool
	}{
		{name: "bytes", source: `<root/>`, limits: Limits{MaxBytes: 1}, limit: true},
		{name: "depth", source: `<root><child/></root>`, limits: Limits{MaxBytes: 100, MaxDepth: 1, MaxNodes: 10, MaxAttributes: 10, MaxTextBytes: 10}, limit: true},
		{name: "nodes", source: `<root><child/></root>`, limits: Limits{MaxBytes: 100, MaxDepth: 10, MaxNodes: 1, MaxAttributes: 10, MaxTextBytes: 10}, limit: true},
		{name: "attributes", source: `<root value="1"/>`, limits: Limits{MaxBytes: 100, MaxDepth: 10, MaxNodes: 10, MaxAttributes: 0, MaxTextBytes: 10}, limit: true},
		{name: "text", source: `<root>text</root>`, limits: Limits{MaxBytes: 100, MaxDepth: 10, MaxNodes: 10, MaxAttributes: 10, MaxTextBytes: 1}, limit: true},
		{name: "duplicate expanded attribute", source: `<root xmlns:a="urn:x" xmlns:b="urn:x" a:value="1" b:value="2"/>`, limits: generousParseLimits()},
		{name: "multiple roots", source: `<first/><second/>`, limits: generousParseLimits()},
		{name: "empty", limits: generousParseLimits()},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			validator := &Validator{set: set, limits: test.limits}
			_, err := validator.parseInstance(context.Background(), []byte(test.source))
			if err == nil {
				t.Fatal("parseInstance() succeeded")
			}
			if test.limit && !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("parseInstance() error = %v, want ErrLimitExceeded", err)
			}
		})
	}
}

func generousParseLimits() Limits {
	return Limits{
		MaxBytes:      1000,
		MaxDepth:      10,
		MaxTextBytes:  100,
		MaxNodes:      10,
		MaxAttributes: 10,
	}
}
