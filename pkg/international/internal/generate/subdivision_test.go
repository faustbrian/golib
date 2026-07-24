package generate

import (
	"bytes"
	"strings"
	"testing"
)

func TestGenerateSubdivisionDataExpandsAndClassifiesCLDRValues(t *testing.T) {
	t.Parallel()

	validity := []byte(`
<supplementalData><idValidity>
  <id type="subdivision" idStatus="regular">ad02~3 usca gbeng toolong</id>
  <id type="subdivision" idStatus="deprecated">fi01</id>
  <id type="subdivision" idStatus="unknown">uszzzz</id>
</idValidity></supplementalData>`)
	names := []byte(`
<ldml><localeDisplayNames><subdivisions>
  <subdivision type="ad02">Canillo</subdivision>
  <subdivision type="ad03">Encamp</subdivision>
  <subdivision type="usca">California</subdivision>
  <subdivision type="gbeng">England</subdivision>
  <subdivision type="usca" alt="short">CA</subdivision>
</subdivisions></localeDisplayNames></ldml>`)

	first, err := generateSubdivisionData(bytes.NewReader(validity), bytes.NewReader(names))
	if err != nil {
		t.Fatalf("generateSubdivisionData() error = %v", err)
	}
	second, err := generateSubdivisionData(bytes.NewReader(validity), bytes.NewReader(names))
	if err != nil {
		t.Fatalf("second generateSubdivisionData() error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("subdivision output is not deterministic")
	}
	output := string(first)
	for _, fragment := range []string{
		`"AD-02":`,
		`name: "Canillo", status: international.StatusOfficial`,
		`"AD-03":`,
		`name: "Encamp", status: international.StatusOfficial`,
		`"GB-ENG": {name: "England", status: international.StatusOfficial}`,
		`"FI-01":`,
		`status: international.StatusDeleted`,
		`var currentSubdivisionCodes = [...]string{"AD-02", "AD-03", "GB-ENG", "US-CA"}`,
	} {
		if !strings.Contains(output, fragment) {
			t.Errorf("output missing %q:\n%s", fragment, output)
		}
	}
	for _, excluded := range []string{"TO-OLONG", "US-ZZZZ", `name: "CA"`} {
		if strings.Contains(output, excluded) {
			t.Errorf("output unexpectedly contains %q", excluded)
		}
	}
}
