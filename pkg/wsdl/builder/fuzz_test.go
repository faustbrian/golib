package builder_test

import (
	"bytes"
	"context"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	"github.com/faustbrian/golib/pkg/wsdl/builder"
)

func FuzzBuilderRoundTrip(f *testing.F) {
	f.Add(byte(11), "Service", "urn:fuzz", "Operation", false)
	f.Add(byte(20), "API", "urn:fuzz", "Operation", true)
	f.Fuzz(func(t *testing.T, version byte, name, namespace, operation string, duplicate bool) {
		var (
			document *wsdl.Document
			err      error
		)
		if version%2 == 0 {
			value := builder.New20(namespace)
			component := wsdl.Interface20{
				Name: name,
				Operations: []wsdl.InterfaceOperation20{{
					Name:    operation,
					Pattern: "http://www.w3.org/ns/wsdl/in-only",
				}},
			}
			if err = value.AddInterface(component); err != nil {
				return
			}
			if duplicate {
				if err = value.AddInterface(component); err == nil {
					t.Fatal("AddInterface(duplicate) error = nil")
				}
				return
			}
			document, err = value.Build(wsdl.ValidationOptions{})
		} else {
			value := builder.New11(name, namespace)
			component := wsdl.PortType11{
				Name:       name,
				Operations: []wsdl.Operation11{{Name: operation}},
			}
			if err = value.AddPortType(component); err != nil {
				return
			}
			if duplicate {
				if err = value.AddPortType(component); err == nil {
					t.Fatal("AddPortType(duplicate) error = nil")
				}
				return
			}
			document, err = value.Build(wsdl.ValidationOptions{})
		}
		if err != nil {
			return
		}

		first, err := wsdl.Marshal(document, wsdl.MarshalOptions{MaxBytes: 1 << 20})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		reparsed, err := wsdl.Parse(context.Background(), first, wsdl.ParseOptions{MaxDocumentBytes: 1 << 20})
		if err != nil {
			t.Fatalf("Parse(Marshal()) error = %v", err)
		}
		second, err := wsdl.Marshal(reparsed, wsdl.MarshalOptions{MaxBytes: 1 << 20})
		if err != nil {
			t.Fatalf("Marshal(reparsed) error = %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("builder round trip is not deterministic:\nfirst:  %s\nsecond: %s", first, second)
		}
	})
}
