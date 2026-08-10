package httpsignature

import (
	"bytes"
	"errors"
	"testing"
)

func TestParseSignatureInputsPreservesLabelsComponentsAndParameters(t *testing.T) {
	t.Parallel()

	inputs, err := ParseSignatureInputs([]string{
		`siga=("@method" "content-digest";sf);created=1618884473;keyid="test-key-rsa-pss";alg="rsa-pss-sha512"`,
		`sigb=("@status" "content-digest";req);tag="response"`,
	})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}

	entries := inputs.Entries()
	if len(entries) != 2 || entries[0].Label != "siga" || entries[1].Label != "sigb" {
		t.Fatalf("Entries() = %#v, want wire label order", entries)
	}
	if got := entries[0].Components; len(got) != 2 || got[0].Name != "@method" || got[1].Name != "content-digest" {
		t.Fatalf("Components = %#v", got)
	}
	if value, ok := entries[0].Parameter("created"); !ok || value != int64(1618884473) {
		t.Fatalf("created = %#v, %t", value, ok)
	}
	if value, ok := entries[0].Components[1].Parameter("sf"); !ok || value != true {
		t.Fatalf("sf = %#v, %t", value, ok)
	}

	const want = `siga=("@method" "content-digest";sf);created=1618884473;keyid="test-key-rsa-pss";alg="rsa-pss-sha512", sigb=("@status" "content-digest";req);tag="response"`
	if got := inputs.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestParseSignatureInputsRejectsAmbiguityAndWrongStructuredTypes(t *testing.T) {
	t.Parallel()

	for _, values := range [][]string{
		{`sig=("@method"), sig=("@path")`},
		{`sig=:YWJj:`},
		{`sig=("@method" "@method")`},
		{`sig=("x-example";foo=1;bar=2 "x-example";bar=2;foo=1)`},
		{`sig=(@method)`},
		{`sig=("Content-Type")`},
		{`sig=("@method");created="yesterday"`},
		{`sig=("@method`},
	} {
		values := values

		t.Run(values[0], func(t *testing.T) {
			t.Parallel()

			_, err := ParseSignatureInputs(values)
			if !errors.Is(err, ErrInvalidSignatureInput) {
				t.Fatalf("ParseSignatureInputs() error = %v, want ErrInvalidSignatureInput", err)
			}
		})
	}
}

func TestParseSignatureInputsAllowsEmptyCoverageAndRFC8941TokenExtensions(t *testing.T) {
	t.Parallel()

	inputs, err := ParseSignatureInputs([]string{`sig=();nonce="one";extension=example:token`})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	entries := inputs.Entries()
	if len(entries) != 1 || len(entries[0].Components) != 0 {
		t.Fatalf("Entries() = %#v, want one empty covered set", entries)
	}
}

func TestParseSignatureInputsAllowsLowercaseHTTPFieldNameCharacters(t *testing.T) {
	t.Parallel()

	inputs, err := ParseSignatureInputs([]string{`sig=("x_test~field")`})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	if got := inputs.Entries()[0].Components[0].Name; got != "x_test~field" {
		t.Fatalf("component name = %q", got)
	}
}

func TestParseSignaturesPreservesOrderAndCopiesBytes(t *testing.T) {
	t.Parallel()

	signatures, err := ParseSignatures([]string{`sigb=:ZGVm:, siga=:YWJj:`})
	if err != nil {
		t.Fatalf("ParseSignatures() error = %v", err)
	}

	entries := signatures.Entries()
	if len(entries) != 2 || entries[0].Label != "sigb" || entries[1].Label != "siga" {
		t.Fatalf("Entries() = %#v", entries)
	}
	entries[0].Value[0] = 'x'
	if bytes.Equal(entries[0].Value, signatures.Entries()[0].Value) {
		t.Fatal("Entries() aliases signature bytes")
	}
	if got := signatures.String(); got != `sigb=:ZGVm:, siga=:YWJj:` {
		t.Fatalf("String() = %q", got)
	}
}

func TestParseSignaturesPreservesRFC8941ItemParameters(t *testing.T) {
	t.Parallel()

	signatures, err := ParseSignatures([]string{`sig=:YWJj:;extension=1;flag`})
	if err != nil {
		t.Fatalf("ParseSignatures() error = %v", err)
	}
	entries := signatures.Entries()
	if len(entries) != 1 || len(entries[0].Parameters) != 2 || entries[0].Parameters[0].Name != "extension" ||
		entries[0].Parameters[0].Value != int64(1) || entries[0].Parameters[1].Name != "flag" || entries[0].Parameters[1].Value != true {
		t.Fatalf("Entries() = %#v", entries)
	}
	if got := signatures.String(); got != `sig=:YWJj:;extension=1;flag` {
		t.Fatalf("String() = %q", got)
	}
}

func TestParseSignaturesRejectsDuplicateLabelsAndNonByteValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		`sig=:YWJj:, sig=:ZGVm:`,
		`sig="not bytes"`,
		`sig=(:YWJj:)`,
		`Sig=:YWJj:`,
	} {
		value := value

		t.Run(value, func(t *testing.T) {
			t.Parallel()

			_, err := ParseSignatures([]string{value})
			if !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("ParseSignatures() error = %v, want ErrInvalidSignature", err)
			}
		})
	}
}
