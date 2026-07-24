package generate

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseRegionValidityExpandsRangesAndStatuses(t *testing.T) {
	t.Parallel()

	input := []byte(`
<supplementalData><idValidity>
  <id type="region" idStatus="regular">AD AE~G</id>
  <id type="region" idStatus="deprecated">AN</id>
  <id type="region" idStatus="reserved">QM~N</id>
</idValidity></supplementalData>`)

	statuses, err := parseRegionValidity(bytes.NewReader(input))
	if err != nil {
		t.Fatalf("parseRegionValidity() error = %v", err)
	}
	want := map[string]string{
		"AD": "regular", "AE": "regular", "AF": "regular", "AG": "regular",
		"AN": "deprecated", "QM": "reserved", "QN": "reserved",
	}
	if len(statuses) != len(want) {
		t.Fatalf("len(statuses) = %d, want %d: %v", len(statuses), len(want), statuses)
	}
	for code, status := range want {
		if got := statuses[code]; got != status {
			t.Errorf("statuses[%q] = %q, want %q", code, got, status)
		}
	}
}

func TestGenerateCountryDataIsDeterministicAndClassifiesRecords(t *testing.T) {
	t.Parallel()

	validity := []byte(`
<supplementalData><idValidity>
  <id type="region" idStatus="regular">FI US XK</id>
  <id type="region" idStatus="deprecated">AN</id>
  <id type="region" idStatus="reserved">QM</id>
  <id type="region" idStatus="private_use">XC</id>
  <id type="region" idStatus="unknown">ZZ</id>
</idValidity></supplementalData>`)
	mappings := []byte(`
<supplementalData><codeMappings>
  <territoryCodes type="US" numeric="840" alpha3="USA"/>
  <territoryCodes type="FI" numeric="246" alpha3="FIN"/>
  <territoryCodes type="XK" numeric="983" alpha3="XKK"/>
  <territoryCodes type="AN" numeric="530" alpha3="ANT"/>
  <territoryCodes type="QM" numeric="959" alpha3="QMM"/>
  <territoryCodes type="XC" numeric="975" alpha3="XCC"/>
  <territoryCodes type="ZZ" numeric="999" alpha3="ZZZ"/>
  <territoryCodes type="AC" alpha3="ASC"/>
</codeMappings></supplementalData>`)

	first, err := generateCountryData(bytes.NewReader(validity), bytes.NewReader(mappings))
	if err != nil {
		t.Fatalf("generateCountryData() error = %v", err)
	}
	second, err := generateCountryData(bytes.NewReader(validity), bytes.NewReader(mappings))
	if err != nil {
		t.Fatalf("second generateCountryData() error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("generateCountryData() output is not deterministic")
	}

	output := string(first)
	for _, fragment := range []string{
		`"FI": {alpha3: "FIN", numeric: 246, status: international.StatusOfficial}`,
		`"AN": {alpha3: "ANT", numeric: 530, status: international.StatusDeleted}`,
		`"QM": {alpha3: "QMM", numeric: 959, status: international.StatusReserved}`,
		`"XC": {alpha3: "XCC", numeric: 975, status: international.StatusUserAssigned}`,
		`"XK": {alpha3: "XKK", numeric: 983, status: international.StatusUserAssigned}`,
		`"ZZ": {alpha3: "ZZZ", numeric: 999, status: international.StatusUnknown}`,
		`var officialCodes = [...]string{"FI", "US"}`,
	} {
		if !strings.Contains(output, fragment) {
			t.Errorf("output missing %q:\n%s", fragment, output)
		}
	}
	if strings.Contains(output, `"AC"`) {
		t.Fatalf("output includes mapping without a numeric code:\n%s", output)
	}
}
