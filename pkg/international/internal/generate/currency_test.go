package generate

import (
	"bytes"
	"strings"
	"testing"
)

func TestGenerateCurrencyDataJoinsCurrentAndHistoricLists(t *testing.T) {
	t.Parallel()

	current := []byte(`
<ISO_4217 Pblshd="2026-01-01"><CcyTbl>
  <CcyNtry><CcyNm>Euro</CcyNm><Ccy>EUR</Ccy><CcyNbr>978</CcyNbr><CcyMnrUnts>2</CcyMnrUnts></CcyNtry>
  <CcyNtry><CcyNm>Euro</CcyNm><Ccy>EUR</Ccy><CcyNbr>978</CcyNbr><CcyMnrUnts>2</CcyMnrUnts></CcyNtry>
  <CcyNtry><CcyNm>Gold</CcyNm><Ccy>XAU</Ccy><CcyNbr>959</CcyNbr><CcyMnrUnts>N.A.</CcyMnrUnts></CcyNtry>
  <CcyNtry><CcyNm>No universal currency</CcyNm></CcyNtry>
</CcyTbl></ISO_4217>`)
	historic := []byte(`
<ISO_4217 Pblshd="2026-01-01"><HstrcCcyTbl>
  <HstrcCcyNtry><CcyNm>Markka</CcyNm><Ccy>FIM</Ccy><CcyNbr>246</CcyNbr><WthdrwlDt>2002-03</WthdrwlDt></HstrcCcyNtry>
  <HstrcCcyNtry><CcyNm>Markka</CcyNm><Ccy>FIM</Ccy><CcyNbr>246</CcyNbr><WthdrwlDt>2002-03</WthdrwlDt></HstrcCcyNtry>
  <HstrcCcyNtry><CcyNm>Old Euro</CcyNm><Ccy>EUR</Ccy><CcyNbr>978</CcyNbr><WthdrwlDt>2001-01</WthdrwlDt></HstrcCcyNtry>
</HstrcCcyTbl></ISO_4217>`)

	first, version, err := generateCurrencyData(bytes.NewReader(current), bytes.NewReader(historic))
	if err != nil {
		t.Fatalf("generateCurrencyData() error = %v", err)
	}
	second, secondVersion, err := generateCurrencyData(bytes.NewReader(current), bytes.NewReader(historic))
	if err != nil {
		t.Fatalf("second generateCurrencyData() error = %v", err)
	}
	if version != "2026-01-01" || secondVersion != version || !bytes.Equal(first, second) {
		t.Fatalf("version/output not deterministic: %q, %q", version, secondVersion)
	}

	output := string(first)
	for _, fragment := range []string{
		`"EUR": {numeric: "978", minorUnits: 2, hasMinorUnits: true, name: "Euro", status: international.StatusOfficial, history: "Old Euro\t2001-01"}`,
		`"XAU": {numeric: "959", name: "Gold", status: international.StatusOfficial}`,
		`"FIM": {numeric: "246", name: "", status: international.StatusHistoric, history: "Markka\t2002-03"}`,
		`var activeCodes = [...]string{"EUR", "XAU"}`,
	} {
		if !strings.Contains(output, fragment) {
			t.Errorf("output missing %q:\n%s", fragment, output)
		}
	}
}
