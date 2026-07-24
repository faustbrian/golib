package soap_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/soap"
)

func assertAllSOAPOutputCutoffs(t *testing.T, encode func(int64) ([]byte, error)) {
	t.Helper()

	payload, err := encode(0)
	if err != nil {
		t.Fatal(err)
	}
	for limit := int64(1); limit < int64(len(payload)); limit++ {
		if _, err := encode(limit); !errors.Is(err, wire.ErrSizeLimit) || !errors.Is(err, soap.ErrPayloadTooLarge) {
			t.Fatalf("limit %d error = %v", limit, err)
		}
	}
	got, err := encode(int64(len(payload)))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("exact Encode() = %q, %v", got, err)
	}
	if _, err := encode(-1); !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("negative error = %v", err)
	}
}

func TestMarshalEnforcesEveryOutputCutoff(t *testing.T) {
	t.Parallel()

	assertAllSOAPOutputCutoffs(t, func(limit int64) ([]byte, error) {
		return soap.MarshalWithOptions(
			soap.Version12,
			[]byte(`<auth>token</auth>`),
			[]byte(`<request>value</request>`),
			soap.MarshalOptions{MaxBytes: limit},
		)
	})
}

func TestMarshalFault11EnforcesEveryOutputCutoff(t *testing.T) {
	t.Parallel()

	assertAllSOAPOutputCutoffs(t, func(limit int64) ([]byte, error) {
		return soap.MarshalFaultWithOptions(soap.Fault{
			Version: soap.Version11,
			Code:    "soap:Server",
			Reason:  "failed & escaped",
			Actor:   "carrier",
			Detail:  []byte(`<retry>true</retry>`),
		}, soap.MarshalOptions{MaxBytes: limit})
	})
}

func TestMarshalFault12EnforcesEveryOutputCutoff(t *testing.T) {
	t.Parallel()

	assertAllSOAPOutputCutoffs(t, func(limit int64) ([]byte, error) {
		return soap.MarshalFaultWithOptions(soap.Fault{
			Version:  soap.Version12,
			Code:     "soap:Receiver",
			Subcodes: []string{"m:Timeout", "m:Upstream"},
			Reasons:  []soap.FaultReason{{Language: "en&fi", Text: "failed & escaped"}},
			Node:     "node",
			Role:     "role",
			Detail:   []byte(`<retry>true</retry>`),
		}, soap.MarshalOptions{MaxBytes: limit})
	})
}

func TestRawInputsOverOutputLimitAreRejectedBeforeXMLValidation(t *testing.T) {
	t.Parallel()

	invalidLargeFragment := bytes.Repeat([]byte{'<'}, 1024)
	if _, err := soap.MarshalWithOptions(
		soap.Version12,
		invalidLargeFragment,
		nil,
		soap.MarshalOptions{MaxBytes: 1},
	); !errors.Is(err, wire.ErrSizeLimit) {
		t.Fatalf("header error = %v, want wire.ErrSizeLimit", err)
	}
	if _, err := soap.MarshalFaultWithOptions(soap.Fault{
		Version: soap.Version12,
		Code:    "soap:Receiver",
		Reason:  "failed",
		Detail:  invalidLargeFragment,
	}, soap.MarshalOptions{MaxBytes: 1}); !errors.Is(err, wire.ErrSizeLimit) {
		t.Fatalf("detail error = %v, want wire.ErrSizeLimit", err)
	}
}
