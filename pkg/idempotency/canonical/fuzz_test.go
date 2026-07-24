package canonical_test

import (
	"bytes"
	"testing"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/canonical"
)

func FuzzJSONIsIdempotent(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`null`),
		[]byte(`{"b":1.0,"a":[true,false]}`),
		[]byte(`{"value":-0}`),
		[]byte(`{"value":1,"value":2}`),
		[]byte(`{"a":1,"\u0061":2}`),
		[]byte(`{"music":"\ud834\udd1e"}`),
		[]byte(`{"nfc":"é","nfd":"é"}`),
		[]byte(`{"number":333333333.33333329}`),
		[]byte(`{"number":1e-27}`),
		[]byte{0xef, 0xbb, 0xbf, '{', '}'},
		{'"', 0xff, '"'},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		limits := canonical.Limits{
			MaxInputBytes:  4096,
			MaxOutputBytes: 4096,
			MaxDepth:       64,
		}
		canonicalValue, err := canonical.JSON(input, limits)
		if err != nil {
			return
		}
		second, err := canonical.JSON(canonicalValue, limits)
		if err != nil {
			t.Fatalf("canonical JSON was rejected: %v", err)
		}
		if !bytes.Equal(canonicalValue, second) {
			t.Fatalf("canonicalization was not idempotent: %q != %q", canonicalValue, second)
		}
	})
}

func FuzzBytesFingerprintPreservesEncoding(f *testing.F) {
	for _, seed := range [][]byte{
		{},
		{0x1f, 0x8b, 0x08, 0x00},
		{0xff, 0xfe, '{', 0x00, '}', 0x00},
		[]byte("eyJhbW91bnQiOjQyfQ=="),
		{'{', 0x00, '}'},
		bytes.Repeat([]byte{'x'}, 4097),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 4097 {
			return
		}
		fingerprint, err := canonical.BytesFingerprint("wire-v1", input, 4096)
		if len(input) > 4096 {
			assertReason(t, err, idempotency.ReasonLimitExceeded)
			return
		}
		if err != nil {
			t.Fatalf("BytesFingerprint() error = %v", err)
		}
		want, err := idempotency.NewFingerprint("wire-v1", input)
		if err != nil {
			t.Fatalf("NewFingerprint() error = %v", err)
		}
		if !fingerprint.Equal(want) {
			t.Fatal("BytesFingerprint() transformed encoded input")
		}
	})
}
