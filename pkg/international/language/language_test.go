package language_test

import (
	"errors"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	intlLanguage "github.com/faustbrian/golib/pkg/international/language"
	"golang.org/x/text/language"
)

func TestParseCanonicalISO639LanguageAndConvertISO3(t *testing.T) {
	t.Parallel()

	code, err := intlLanguage.Parse("fi")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if code.String() != "fi" || code.ISO3() != "fin" || code.IsZero() {
		t.Fatalf("code = %q, ISO3 %q, IsZero %v", code, code.ISO3(), code.IsZero())
	}
	if got := intlLanguage.Name(code, language.Finnish); got != "suomi" {
		t.Fatalf("Name() = %q, want suomi", got)
	}
}

func TestParseISO3UsesAuthoritativeMapping(t *testing.T) {
	t.Parallel()

	code, err := intlLanguage.ParseISO3("eng")
	if err != nil {
		t.Fatalf("ParseISO3() error = %v", err)
	}
	if code.String() != "en" || code.ISO3() != "eng" {
		t.Fatalf("code = %q (%q), want en (eng)", code, code.ISO3())
	}
}

func TestCanonicalThreeLetterOnlyLanguageRoundTrips(t *testing.T) {
	t.Parallel()
	code, err := intlLanguage.Parse("ace")
	if err != nil {
		t.Fatalf("Parse(ace) error = %v", err)
	}
	if code.String() != "ace" || code.ISO3() != "ace" {
		t.Fatalf("code = %q (%q), want ace", code, code.ISO3())
	}
	text, err := code.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var decoded intlLanguage.Code
	if err := decoded.UnmarshalText(text); err != nil || decoded != code {
		t.Fatalf("round trip = %q, %v", decoded, err)
	}
}

func TestLanguageParsingIsStrictAndBounded(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "EN", "eng", "e", "zz", "iw", "e1", "\xffn"} {
		if _, err := intlLanguage.Parse(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("Parse(%q) error = %v, want ErrInvalid", input, err)
		}
	}
	for _, input := range []string{"", "ENG", "en", "zzz", "e1g", "\xffng"} {
		if _, err := intlLanguage.ParseISO3(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("ParseISO3(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}

func TestZeroLanguageIsExplicitlyAbsent(t *testing.T) {
	t.Parallel()

	var code intlLanguage.Code
	if !code.IsZero() || code.String() != "" || code.ISO3() != "" {
		t.Fatalf("zero code = %q (%q), IsZero %v", code, code.ISO3(), code.IsZero())
	}
	if got := intlLanguage.Name(code, language.English); got != "" {
		t.Fatalf("Name(zero) = %q, want empty", got)
	}
	finnish, err := intlLanguage.Parse("fi")
	if err != nil {
		t.Fatal(err)
	}
	if got := intlLanguage.Name(finnish, language.Tag{}); got != "" {
		t.Fatalf("Name() with unsupported display locale = %q, want empty", got)
	}
}

func TestLanguageProvenanceIsPinnedToIANARegistry(t *testing.T) {
	t.Parallel()

	provenance := intlLanguage.DatasetProvenance()
	if err := provenance.Validate(); err != nil {
		t.Fatalf("DatasetProvenance().Validate() error = %v", err)
	}
	if provenance.UpstreamVersion != "IANA registry 2026-06-14; x/text v0.40.0" {
		t.Fatalf("version = %q", provenance.UpstreamVersion)
	}
}
