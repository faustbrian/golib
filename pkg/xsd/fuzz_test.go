package xsd_test

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func FuzzParseSchema(f *testing.F) {
	f.Add(benchmarkSchema)
	f.Add([]byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`))
	f.Add([]byte(`<!DOCTYPE schema><schema/>`))
	f.Fuzz(func(t *testing.T, source []byte) {
		document, err := xsd.Parse(context.Background(), source, xsd.ParseOptions{
			MaxDocumentBytes: 1 << 20,
		})
		if err != nil {
			return
		}
		if _, err := xsd.Marshal(document); err != nil {
			t.Fatalf("Marshal(parsed document) error = %v", err)
		}
	})
}

func FuzzValidateInstance(f *testing.F) {
	validator := benchmarkValidator(f)
	f.Add(benchmarkInstance)
	f.Add([]byte(`<root xmlns="urn:benchmark" status="ok"><code>A</code></root>`))
	f.Add([]byte(`<!DOCTYPE root><root/>`))
	f.Fuzz(func(t *testing.T, source []byte) {
		_, _ = validator.Validate(context.Background(), source)
	})
}
