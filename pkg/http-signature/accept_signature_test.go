package httpsignature

import (
	"errors"
	"testing"
)

func TestParseAcceptSignaturesPreservesRequestedMetadata(t *testing.T) {
	t.Parallel()

	requests, err := ParseAcceptSignatures([]string{
		`sig1=("@method" "@target-uri" "content-digest");keyid="test-key-rsa-pss";created;expires;alg="rsa-pss-sha512";tag="app-123"`,
	})
	if err != nil {
		t.Fatalf("ParseAcceptSignatures() error = %v", err)
	}

	entries := requests.Entries()
	if len(entries) != 1 || entries[0].Label != "sig1" || len(entries[0].Components) != 3 {
		t.Fatalf("Entries() = %#v", entries)
	}
	if value, ok := entries[0].Parameter("created"); !ok || value != true {
		t.Fatalf("created = %#v, %t", value, ok)
	}
	if value, ok := entries[0].Parameter("expires"); !ok || value != true {
		t.Fatalf("expires = %#v, %t", value, ok)
	}

	const want = `sig1=("@method" "@target-uri" "content-digest");keyid="test-key-rsa-pss";created;expires;alg="rsa-pss-sha512";tag="app-123"`
	if got := requests.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestParseAcceptSignaturesRejectsActualTimestampValuesAndDuplicateLabels(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		`sig=("@method");created=1618884473`,
		`sig=("@method");expires="later"`,
		`sig=("@method"), sig=("@path")`,
		`sig=:YWJj:`,
	} {
		value := value

		t.Run(value, func(t *testing.T) {
			t.Parallel()

			_, err := ParseAcceptSignatures([]string{value})
			if !errors.Is(err, ErrInvalidAcceptSignature) {
				t.Fatalf("ParseAcceptSignatures() error = %v, want ErrInvalidAcceptSignature", err)
			}
		})
	}
}
