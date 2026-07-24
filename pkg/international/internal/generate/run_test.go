package generate

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRunGeneratesEveryRequestedDatasetOffline(t *testing.T) {
	t.Parallel()
	sources := map[string]string{
		cldrValidityURL:            `<supplementalData><idValidity><id type="region" idStatus="regular">FI</id></idValidity></supplementalData>`,
		cldrMappingsURL:            `<supplementalData><codeMappings><territoryCodes type="FI" numeric="246" alpha3="FIN"/></codeMappings></supplementalData>`,
		sixCurrentURL:              `<ISO_4217 Pblshd="v"><CcyTbl><CcyNtry><CcyNm>Euro</CcyNm><Ccy>EUR</Ccy><CcyNbr>978</CcyNbr><CcyMnrUnts>2</CcyMnrUnts></CcyNtry></CcyTbl></ISO_4217>`,
		sixHistoricURL:             `<ISO_4217 Pblshd="v"><HstrcCcyTbl></HstrcCcyTbl></ISO_4217>`,
		cldrSubdivisionValidityURL: `<supplementalData><idValidity><id type="subdivision" idStatus="regular">fi18</id></idValidity></supplementalData>`,
		cldrSubdivisionNamesURL:    `<ldml><localeDisplayNames><subdivisions><subdivision type="fi18">Uusimaa</subdivision></subdivisions></localeDisplayNames></ldml>`,
	}
	writes := map[string][]byte{}
	err := run([]string{"-country-output", "country.go", "-currency-output", "currency.go", "-subdivision-output", "subdivision.go"},
		func(source, _ string) ([]byte, error) { return []byte(sources[source]), nil },
		func(path string, data []byte, mode os.FileMode) error {
			if mode != 0o644 {
				t.Fatalf("mode = %v", mode)
			}
			writes[path] = append([]byte(nil), data...)
			return nil
		})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	for _, path := range []string{"country.go", "currency.go", "subdivision.go"} {
		if len(writes[path]) == 0 {
			t.Fatalf("missing write %s", path)
		}
	}
}

func TestRunRejectsInvalidInvocationAndDependencyFailures(t *testing.T) {
	t.Parallel()
	if err := Run(nil); err == nil {
		t.Fatal("Run(nil) succeeded")
	}
	noopFetch := func(string, string) ([]byte, error) { return nil, errors.New("fetch") }
	noopWrite := func(string, []byte, os.FileMode) error { return nil }
	for _, arguments := range [][]string{{"-unknown"}, {"extra"}, {}} {
		if err := run(arguments, noopFetch, noopWrite); err == nil {
			t.Fatalf("run(%v) succeeded", arguments)
		}
	}
	if err := run([]string{"-country-output", "x"}, nil, noopWrite); err == nil {
		t.Fatal("nil fetch succeeded")
	}
	for _, flagName := range []string{"-country-output", "-currency-output", "-subdivision-output"} {
		if err := run([]string{flagName, "x"}, noopFetch, noopWrite); err == nil {
			t.Fatalf("fetch failure for %s succeeded", flagName)
		}
		calls := 0
		secondFailure := func(source, _ string) ([]byte, error) {
			calls++
			if calls == 2 {
				return nil, errors.New("second fetch")
			}
			return successfulSource(source), nil
		}
		if err := run([]string{flagName, "x"}, secondFailure, noopWrite); err == nil {
			t.Fatalf("second fetch failure for %s succeeded", flagName)
		}
	}
	badFetch := func(string, string) ([]byte, error) { return []byte("<"), nil }
	for _, flagName := range []string{"-country-output", "-currency-output", "-subdivision-output"} {
		if err := run([]string{flagName, "x"}, badFetch, noopWrite); err == nil {
			t.Fatalf("generation failure for %s succeeded", flagName)
		}
	}
	writeFailure := func(string, []byte, os.FileMode) error { return errors.New("write") }
	validCountryFetch := func(source, _ string) ([]byte, error) {
		if strings.Contains(source, "region.xml") {
			return []byte(`<supplementalData><idValidity><id type="region" idStatus="regular">FI</id></idValidity></supplementalData>`), nil
		}
		return []byte(`<supplementalData><codeMappings><territoryCodes type="FI" numeric="246" alpha3="FIN"/></codeMappings></supplementalData>`), nil
	}
	if err := run([]string{"-country-output", "x"}, validCountryFetch, writeFailure); err == nil {
		t.Fatal("write failure succeeded")
	}
	for _, flagName := range []string{"-currency-output", "-subdivision-output"} {
		if err := run([]string{flagName, "x"}, func(source, _ string) ([]byte, error) { return successfulSource(source), nil }, writeFailure); err == nil {
			t.Fatalf("write failure for %s succeeded", flagName)
		}
	}
}

func successfulSource(source string) []byte {
	sources := map[string]string{
		cldrValidityURL:            `<supplementalData><idValidity><id type="region" idStatus="regular">FI</id></idValidity></supplementalData>`,
		cldrMappingsURL:            `<supplementalData><codeMappings><territoryCodes type="FI" numeric="246" alpha3="FIN"/></codeMappings></supplementalData>`,
		sixCurrentURL:              `<ISO_4217 Pblshd="v"><CcyTbl><CcyNtry><CcyNm>Euro</CcyNm><Ccy>EUR</Ccy><CcyNbr>978</CcyNbr><CcyMnrUnts>2</CcyMnrUnts></CcyNtry></CcyTbl></ISO_4217>`,
		sixHistoricURL:             `<ISO_4217 Pblshd="v"><HstrcCcyTbl></HstrcCcyTbl></ISO_4217>`,
		cldrSubdivisionValidityURL: `<supplementalData><idValidity><id type="subdivision" idStatus="regular">fi18</id></idValidity></supplementalData>`,
		cldrSubdivisionNamesURL:    `<ldml><localeDisplayNames><subdivisions><subdivision type="fi18">Uusimaa</subdivision></subdivisions></localeDisplayNames></ldml>`,
	}
	return []byte(sources[source])
}
