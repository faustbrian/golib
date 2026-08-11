package httpsignature

import (
	"errors"
	"testing"
)

func TestExplicitSyntaxLimitsRejectBeforeStructuredParsing(t *testing.T) {
	t.Parallel()

	limits := SyntaxLimits{
		MaxFieldBytes:             32,
		MaxFieldLines:             1,
		MaxDictionaryMembers:      1,
		MaxComponentsPerSignature: 1,
		MaxParametersPerItem:      1,
		MaxBinaryBytes:            3,
	}
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "field bytes", run: func() error {
			_, err := ParseSignatureInputsWithLimits([]string{`sig=("@method");nonce="value-too-long"`}, limits)
			return err
		}},
		{name: "field lines", run: func() error {
			_, err := ParseSignatureInputsWithLimits([]string{`sig=("@method")`, `two=("@method")`}, limits)
			return err
		}},
		{name: "members", run: func() error {
			_, err := ParseSignaturesWithLimits([]string{`one=:YWJj:, two=:YWJj:`}, limits)
			return err
		}},
		{name: "components", run: func() error {
			_, err := ParseSignatureInputsWithLimits([]string{`sig=("@method" "@path")`}, limits)
			return err
		}},
		{name: "parameters", run: func() error {
			_, err := ParseSignatureInputsWithLimits([]string{`sig=("@method";a=1;b=2)`}, limits)
			return err
		}},
		{name: "binary", run: func() error { _, err := ParseSignaturesWithLimits([]string{`sig=:YWJjZA==:`}, limits); return err }},
		{name: "digest", run: func() error {
			_, err := ParseDigestFieldsWithLimits([]string{`sha-256=:YWJjZA==:`}, limits)
			return err
		}},
		{name: "preference", run: func() error {
			_, err := ParseDigestPreferencesWithLimits([]string{`sha-256=1, sha-512=2`}, limits)
			return err
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.run(); !errors.Is(err, ErrSyntaxLimit) {
				t.Fatalf("error = %v, want ErrSyntaxLimit", err)
			}
		})
	}
}

func TestDefaultSyntaxLimitsAreValidAndBounded(t *testing.T) {
	t.Parallel()

	limits := DefaultSyntaxLimits()
	if err := limits.Validate(); err != nil {
		t.Fatalf("DefaultSyntaxLimits.Validate() error = %v", err)
	}
	if limits.MaxDictionaryMembers < 1024 || limits.MaxComponentsPerSignature < 256 || limits.MaxParametersPerItem < 256 {
		t.Fatalf("default Structured Fields cardinalities are below RFC 8941 minima: %#v", limits)
	}
	if _, err := ParseSignatureInputsWithLimits([]string{`sig=("@method")`}, SyntaxLimits{}); !errors.Is(err, ErrInvalidSyntaxLimits) {
		t.Fatalf("zero limits error = %v", err)
	}
}

func TestSyntaxLimitsValidateEveryIndependentBoundary(t *testing.T) {
	t.Parallel()

	valid := SyntaxLimits{
		MaxFieldBytes:             2,
		MaxFieldLines:             1,
		MaxDictionaryMembers:      1,
		MaxComponentsPerSignature: 1,
		MaxParametersPerItem:      1,
		MaxBinaryBytes:            2,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid limits error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*SyntaxLimits)
	}{
		{"field bytes zero", func(limits *SyntaxLimits) { limits.MaxFieldBytes = 0 }},
		{"field bytes negative", func(limits *SyntaxLimits) { limits.MaxFieldBytes = -1 }},
		{"field lines zero", func(limits *SyntaxLimits) { limits.MaxFieldLines = 0 }},
		{"field lines negative", func(limits *SyntaxLimits) { limits.MaxFieldLines = -1 }},
		{"members zero", func(limits *SyntaxLimits) { limits.MaxDictionaryMembers = 0 }},
		{"members negative", func(limits *SyntaxLimits) { limits.MaxDictionaryMembers = -1 }},
		{"components zero", func(limits *SyntaxLimits) { limits.MaxComponentsPerSignature = 0 }},
		{"components negative", func(limits *SyntaxLimits) { limits.MaxComponentsPerSignature = -1 }},
		{"parameters zero", func(limits *SyntaxLimits) { limits.MaxParametersPerItem = 0 }},
		{"parameters negative", func(limits *SyntaxLimits) { limits.MaxParametersPerItem = -1 }},
		{"binary zero", func(limits *SyntaxLimits) { limits.MaxBinaryBytes = 0 }},
		{"binary negative", func(limits *SyntaxLimits) { limits.MaxBinaryBytes = -1 }},
		{"binary exceeds field", func(limits *SyntaxLimits) { limits.MaxBinaryBytes = 3 }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := valid
			test.mutate(&limits)
			if err := limits.Validate(); !errors.Is(err, ErrInvalidSyntaxLimits) {
				t.Fatalf("Validate() error = %v, want ErrInvalidSyntaxLimits", err)
			}
		})
	}
}

func TestRawSyntaxLimitsAcceptExactLimitsAndRejectTheirNeighbors(t *testing.T) {
	t.Parallel()

	limits := SyntaxLimits{
		MaxFieldBytes:             4,
		MaxFieldLines:             2,
		MaxDictionaryMembers:      1,
		MaxComponentsPerSignature: 1,
		MaxParametersPerItem:      1,
		MaxBinaryBytes:            1,
	}
	if err := enforceRawSyntaxLimits([]string{"a", "b"}, limits); err != nil {
		t.Fatalf("exact limits error = %v", err)
	}
	for _, values := range [][]string{nil, {}, {"a", "b", "c"}, {"abcde"}, {"abcd", "a"}} {
		if err := enforceRawSyntaxLimits(values, limits); !errors.Is(err, ErrSyntaxLimit) {
			t.Fatalf("enforceRawSyntaxLimits(%q) error = %v, want ErrSyntaxLimit", values, err)
		}
	}
}

func TestRawSyntaxLimitsCountCombinedFieldSeparators(t *testing.T) {
	t.Parallel()

	limits := SyntaxLimits{
		MaxFieldBytes:             7,
		MaxFieldLines:             8,
		MaxDictionaryMembers:      2,
		MaxComponentsPerSignature: 1,
		MaxParametersPerItem:      1,
		MaxBinaryBytes:            1,
	}
	if _, err := ParseDigestPreferencesWithLimits([]string{"x=1", "y=1"}, limits); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("combined field error = %v, want ErrSyntaxLimit", err)
	}

	limits.MaxFieldBytes = 4
	if err := enforceRawSyntaxLimits([]string{"a", "b"}, limits); err != nil {
		t.Fatalf("exact combined field error = %v", err)
	}

	emptyLines := make([]string, limits.MaxFieldLines)
	limits.MaxFieldBytes = 2 * (len(emptyLines) - 1)
	if err := enforceRawSyntaxLimits(emptyLines, limits); err != nil {
		t.Fatalf("exact empty-line separators error = %v", err)
	}
	limits.MaxFieldBytes--
	if err := enforceRawSyntaxLimits(emptyLines, limits); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("oversized empty-line separators error = %v, want ErrSyntaxLimit", err)
	}
}

func TestStructuredFieldParsersAcceptEveryExactResourceLimit(t *testing.T) {
	t.Parallel()

	limits := SyntaxLimits{
		MaxFieldBytes:             256,
		MaxFieldLines:             1,
		MaxDictionaryMembers:      1,
		MaxComponentsPerSignature: 1,
		MaxParametersPerItem:      1,
		MaxBinaryBytes:            1,
	}
	tests := []struct {
		name string
		run  func() error
	}{
		{"signature input", func() error {
			_, err := ParseSignatureInputsWithLimits([]string{`sig=("@query-param";name="x");created=1`}, limits)
			return err
		}},
		{"signature", func() error {
			_, err := ParseSignaturesWithLimits([]string{`sig=:eA==:;x=1`}, limits)
			return err
		}},
		{"accept signature", func() error {
			_, err := ParseAcceptSignaturesWithLimits([]string{`sig=("@method");keyid="x"`}, limits)
			return err
		}},
		{"digest", func() error {
			_, err := ParseDigestFieldsWithLimits([]string{`x=:eA==:;x=1`}, limits)
			return err
		}},
		{"digest preference", func() error {
			_, err := ParseDigestPreferencesWithLimits([]string{`x=10`}, limits)
			return err
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.run(); err != nil {
				t.Fatalf("exact parser limits error = %v", err)
			}
		})
	}
}
