package httpsignature

import (
	"bytes"
	"errors"
	"testing"
)

func TestComputeDigestsMatchesRFC9530Example(t *testing.T) {
	t.Parallel()

	field, err := ComputeDigests([]DigestAlgorithm{SHA512, SHA256}, []byte("{\"hello\": \"world\"}\n"))
	if err != nil {
		t.Fatalf("ComputeDigests() error = %v", err)
	}

	const want = "sha-256=:RK/0qy18MlBSVnWgjwz6lZEWjP/lF5HF9bvEF8FabDg=:, sha-512=:YMAam51Jz/jOATT6/zvHrLVgOYTGFy1d6GJiOHTohq4yP+pgk4vf2aCsyRZOtw8MjkM7iw7yZ/WkppmM44T3qg==:"
	if got := field.String(); got != want {
		t.Fatalf("DigestField.String() = %q, want %q", got, want)
	}
}

func TestRFC9530ReportedBrotliErratumKeepsPublishedAndProposedBytesDistinct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		bytes string
		want  string
	}{
		{
			name:  "published RFC 9530 figures 18 and 21",
			bytes: "8b08807b2268656c6c6f223a2022776f726c64227d0a03",
			want:  "sha-256=:MklYnI/SsUF/5X7enJ2TU+DFjodRObdKLFaPPLe/Kcw=:, sha-512=:uItqfeAbi/tOHM+dcFUkMAXmOJ02fQug/kveWtMG7/ZO7pX+4XbgXnxNAoIEhbTdc5g++QOBUAt1+85yolxEIA==:",
		},
		{
			name:  "reported erratum 8890 proposed bytes",
			bytes: "0b09807b2268656c6c6f223a2022776f726c64227d0a03",
			want:  "sha-256=:d435Qo+nKZ+gLcUHn7GQtQ72hiBVAgqoLsZnZPiTGPk=:, sha-512=:db7fdBbgZMgX1Wb2MjA8zZj+rSNgfmDCEEXM8qLWfpfoNY0sCpHAzZbj09X1/7HAb7Od5Qfto4QpuBsFbUO3dQ==:",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			field, err := ComputeDigests([]DigestAlgorithm{SHA256, SHA512}, decodeHex(t, test.bytes))
			if err != nil {
				t.Fatalf("ComputeDigests() error = %v", err)
			}
			if got := field.String(); got != test.want {
				t.Fatalf("DigestField.String() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestComputeDigestsRejectsUnsupportedAndDuplicateAlgorithms(t *testing.T) {
	t.Parallel()

	tests := map[string][]DigestAlgorithm{
		"unsupported": {DigestAlgorithm("md5")},
		"duplicate":   {SHA256, SHA256},
		"empty":       nil,
	}

	for name, algorithms := range tests {
		algorithms := algorithms

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := ComputeDigests(algorithms, []byte("content"))
			if !errors.Is(err, ErrUnsupportedDigestAlgorithm) && !errors.Is(err, ErrInvalidDigestField) {
				t.Fatalf("ComputeDigests() error = %v, want a typed digest error", err)
			}
		})
	}
}

func TestParseDigestFieldPreservesWireOrderAndCopiesValues(t *testing.T) {
	t.Parallel()

	field, err := ParseDigestField("sha-512=:YWJjZA==:, sha-256=:YWJj:")
	if err != nil {
		t.Fatalf("ParseDigestField() error = %v", err)
	}

	entries := field.Entries()
	if len(entries) != 2 || entries[0].Algorithm != SHA512 || entries[1].Algorithm != SHA256 {
		t.Fatalf("Entries() = %#v, want wire order", entries)
	}

	entries[0].Value[0] = 'x'
	got := field.Entries()
	if bytes.Equal(entries[0].Value, got[0].Value) {
		t.Fatal("Entries() aliases internal digest bytes")
	}
}

func TestParseDigestFieldPreservesRFC8941ItemParameters(t *testing.T) {
	t.Parallel()

	field, err := ParseDigestField(`sha-256=:YWJj:;extension="value";flag`)
	if err != nil {
		t.Fatalf("ParseDigestField() error = %v", err)
	}
	entries := field.Entries()
	if len(entries) != 1 || len(entries[0].Parameters) != 2 || entries[0].Parameters[0].Name != "extension" ||
		entries[0].Parameters[0].Value != "value" || entries[0].Parameters[1].Name != "flag" || entries[0].Parameters[1].Value != true {
		t.Fatalf("Entries() = %#v", entries)
	}
	if got := field.String(); got != `sha-256=:YWJj:;extension="value";flag` {
		t.Fatalf("String() = %q", got)
	}
}

func TestParseDigestFieldAllowsRFC8941OptionalWhitespace(t *testing.T) {
	t.Parallel()

	field, err := ParseDigestField("\tsha-256=:YWJj:\t,\tsha-512=:YWJjZA==:\t")
	if err != nil {
		t.Fatalf("ParseDigestField() error = %v", err)
	}
	if got := field.String(); got != "sha-256=:YWJj:, sha-512=:YWJjZA==:" {
		t.Fatalf("String() = %q", got)
	}
}

func TestParseDigestFieldRejectsMalformedOrAmbiguousValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"sha-256=\"not bytes\"",
		"SHA-256=:YWJj:",
		"sha-256=:YWJj:, sha-256=:ZGVm:",
		"sha-256=:YWJj",
		"sha-256=:YW Jj:",
		"sha-256=:YWJj: trailing",
	} {
		value := value

		t.Run(value, func(t *testing.T) {
			t.Parallel()

			_, err := ParseDigestField(value)
			if !errors.Is(err, ErrInvalidDigestField) {
				t.Fatalf("ParseDigestField(%q) error = %v, want ErrInvalidDigestField", value, err)
			}
		})
	}
}

func TestParseDigestFieldsCombinesLinesAndRejectsCrossLineDuplicates(t *testing.T) {
	t.Parallel()

	field, err := ParseDigestFields([]string{
		"sha-256=:YWJj:",
		"sha-512=:ZGVm:",
	})
	if err != nil {
		t.Fatalf("ParseDigestFields() error = %v", err)
	}
	entries := field.Entries()
	if len(entries) != 2 || entries[0].Algorithm != SHA256 || string(entries[0].Value) != "abc" ||
		entries[1].Algorithm != SHA512 || string(entries[1].Value) != "def" {
		t.Fatalf("Entries() = %#v", entries)
	}

	if _, err := ParseDigestFields([]string{"sha-256=:YWJj:", "sha-256=:ZGVm:"}); !errors.Is(err, ErrInvalidDigestField) {
		t.Fatalf("ParseDigestFields(duplicate) error = %v", err)
	}
}

func TestDigestFieldVerifyRequiresEverySelectedAlgorithm(t *testing.T) {
	t.Parallel()

	content := []byte("{\"hello\": \"world\"}\n")
	field, err := ParseDigestField("sha-256=:RK/0qy18MlBSVnWgjwz6lZEWjP/lF5HF9bvEF8FabDg=:, unknown=:aWdub3JlZA==:")
	if err != nil {
		t.Fatalf("ParseDigestField() error = %v", err)
	}

	if err := field.Verify(content, []DigestAlgorithm{SHA256}); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if err := field.Verify(content, []DigestAlgorithm{SHA512}); !errors.Is(err, ErrMissingDigest) {
		t.Fatalf("Verify() missing error = %v, want ErrMissingDigest", err)
	}

	tampered := append([]byte(nil), content...)
	tampered[0] = '['
	if err := field.Verify(tampered, []DigestAlgorithm{SHA256}); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("Verify() mismatch error = %v, want ErrDigestMismatch", err)
	}
}
