package wsdl_test

import (
	"bytes"
	"context"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

var fuzzParseOptions = wsdl.ParseOptions{
	MaxDocumentBytes: 1 << 20,
	MaxDepth:         64,
	MaxElements:      10000,
	MaxAttributes:    50000,
	MaxTextBytes:     1 << 20,
}

func FuzzParseRoundTrip(f *testing.F) {
	f.Add([]byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/" targetNamespace="urn:fuzz"/>`))
	f.Add([]byte(`<description xmlns="http://www.w3.org/ns/wsdl" targetNamespace="urn:fuzz"/>`))
	f.Fuzz(func(t *testing.T, source []byte) {
		document, err := wsdl.Parse(context.Background(), source, fuzzParseOptions)
		if err != nil {
			return
		}
		payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{MaxBytes: 1 << 20})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{
			MaxDocumentBytes: 1 << 20,
		})
		if err != nil {
			t.Fatalf("Parse(Marshal()) error = %v", err)
		}
		if roundTrip.Version() != document.Version() {
			t.Fatalf("round-trip version = %q, want %q", roundTrip.Version(), document.Version())
		}
	})
}

func FuzzModelRoundTrip(f *testing.F) {
	f.Add(byte(11), "Service", "urn:fuzz", "Operation")
	f.Add(byte(20), "API", "urn:fuzz", "Operation")
	f.Fuzz(func(t *testing.T, version byte, name, namespace, operation string) {
		var (
			document *wsdl.Document
			err      error
		)
		if version%2 == 0 {
			document, err = wsdl.NewDocument20(wsdl.Description20{
				TargetNamespace: namespace,
				Interfaces: []wsdl.Interface20{{
					Name: name,
					Operations: []wsdl.InterfaceOperation20{{
						Name:    operation,
						Pattern: "http://www.w3.org/ns/wsdl/in-only",
					}},
				}},
			}, wsdl.ValidationOptions{})
		} else {
			document, err = wsdl.NewDocument11(wsdl.Definitions11{
				Name:            name,
				TargetNamespace: namespace,
				PortTypes: []wsdl.PortType11{{
					Name:       name,
					Operations: []wsdl.Operation11{{Name: operation}},
				}},
			}, wsdl.ValidationOptions{})
		}
		if err != nil {
			return
		}

		first, err := wsdl.Marshal(document, wsdl.MarshalOptions{MaxBytes: 1 << 20})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		reparsed, err := wsdl.Parse(context.Background(), first, fuzzParseOptions)
		if err != nil {
			t.Fatalf("Parse(Marshal()) error = %v", err)
		}
		second, err := wsdl.Marshal(reparsed, wsdl.MarshalOptions{MaxBytes: 1 << 20})
		if err != nil {
			t.Fatalf("Marshal(reparsed) error = %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("model round trip is not deterministic:\nfirst:  %s\nsecond: %s", first, second)
		}
	})
}
