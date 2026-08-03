package generate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegionParserRejectsMalformedSourcesAndRanges(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"<", `<supplementalData><idValidity><id type="region">AA~</id></idValidity></supplementalData>`} {
		if _, err := parseRegionValidity(strings.NewReader(input)); err == nil {
			t.Fatalf("parseRegionValidity(%q) succeeded", input)
		}
	}
	input := `<supplementalData><idValidity><id type="language">aa</id><id type="region">001 AA</id></idValidity></supplementalData>`
	statuses, err := parseRegionValidity(strings.NewReader(input))
	_, present := statuses["AA"]
	if err != nil || len(statuses) != 1 || !present {
		t.Fatalf("statuses = %#v, %v", statuses, err)
	}
	for _, token := range []string{"A", "AA~Z~Q", "Aa~Z", "AZ~A", "AZ~["} {
		if _, err := expandCodeRange(token); err == nil {
			t.Fatalf("expandCodeRange(%q) succeeded", token)
		}
	}
}

func TestCountryGeneratorRejectsMalformedMappings(t *testing.T) {
	t.Parallel()
	validity := `<supplementalData><idValidity><id type="region" idStatus="regular">AA</id></idValidity></supplementalData>`
	for _, mappings := range []string{"<", `<supplementalData><codeMappings><territoryCodes type="AA" numeric="bad" alpha3="AAA"/></codeMappings></supplementalData>`} {
		if _, err := generateCountryData(strings.NewReader(validity), strings.NewReader(mappings)); err == nil {
			t.Fatalf("generateCountryData(%q) succeeded", mappings)
		}
	}
	ignored, err := generateCountryData(strings.NewReader(validity), strings.NewReader(`<supplementalData><codeMappings><territoryCodes type="BB" numeric="001" alpha3="BBB"/></codeMappings></supplementalData>`))
	if err != nil || strings.Contains(string(ignored), `"BB"`) {
		t.Fatalf("unclassified mapping = %s, %v", ignored, err)
	}
	if _, err := generateCountryData(strings.NewReader("<"), strings.NewReader("<")); err == nil {
		t.Fatal("invalid validity succeeded")
	}
	for _, test := range []struct {
		status, want string
		include      bool
	}{
		{"special", "StatusUserAssigned", true}, {"", "", false},
	} {
		got, include := generatedStatus("AA", test.status)
		if got != test.want || include != test.include {
			t.Fatalf("generatedStatus(%q) = %q, %v", test.status, got, include)
		}
	}
}

func TestCurrencyHelpersRejectConflictsAndMalformedRows(t *testing.T) {
	t.Parallel()
	if _, err := decodeCurrencyDocument(strings.NewReader("<")); err == nil {
		t.Fatal("invalid currency XML succeeded")
	}
	for _, entry := range []currencyXMLEntry{{}, {Code: "EUR", Numeric: "978", Name: "Euro", MinorUnits: "x"}, {Code: "EUR", Numeric: "978", Name: "Euro", MinorUnits: "10"}} {
		if _, err := activeCurrencyRecord(entry); err == nil {
			t.Fatalf("activeCurrencyRecord(%#v) succeeded", entry)
		}
	}
	for _, entry := range []currencyXMLEntry{{}, {Code: "EUR", Numeric: "xx", Name: "Euro", Withdrawal: "2000"}} {
		if _, err := historicCurrencyRecord(entry); err == nil {
			t.Fatalf("historicCurrencyRecord(%#v) succeeded", entry)
		}
	}
	if got := mergeMetadata("", "b"); got != "b" {
		t.Fatalf("merge empty = %q", got)
	}
	if got := mergeMetadata("a|b", "b"); got != "a|b" {
		t.Fatalf("merge duplicate = %q", got)
	}
	if got := mergeMetadata("b", "a"); got != "a|b" {
		t.Fatalf("merge sort = %q", got)
	}

	current := func(rows string) string { return `<ISO_4217 Pblshd="v"><CcyTbl>` + rows + `</CcyTbl></ISO_4217>` }
	historic := func(version, rows string) string {
		return `<ISO_4217 Pblshd="` + version + `"><HstrcCcyTbl>` + rows + `</HstrcCcyTbl></ISO_4217>`
	}
	active := `<CcyNtry><CcyNm>Euro</CcyNm><Ccy>EUR</Ccy><CcyNbr>978</CcyNbr><CcyMnrUnts>2</CcyMnrUnts></CcyNtry>`
	ignoredHistoric := historic("v", `<HstrcCcyNtry><CcyNm>Ignored</CcyNm></HstrcCcyNtry>`)
	if _, _, err := generateCurrencyData(strings.NewReader(current(active)), strings.NewReader(ignoredHistoric)); err != nil {
		t.Fatalf("empty historic code was not ignored: %v", err)
	}
	for _, pair := range [][2]string{
		{"<", historic("v", "")}, {current(active), "<"}, {current(active), historic("x", "")},
		{current(`<CcyNtry><CcyNm>Bad</CcyNm><Ccy>BAD</Ccy><CcyNbr>999</CcyNbr><CcyMnrUnts>x</CcyMnrUnts></CcyNtry>`), historic("v", "")},
		{current(active), historic("v", `<HstrcCcyNtry><CcyNm>Bad</CcyNm><Ccy>BAD</Ccy></HstrcCcyNtry>`)},
		{current(active + strings.Replace(active, "978", "977", 1)), historic("v", "")},
		{current(active), historic("v", `<HstrcCcyNtry><CcyNm>Old</CcyNm><Ccy>EUR</Ccy><CcyNbr>977</CcyNbr><WthdrwlDt>2000</WthdrwlDt></HstrcCcyNtry>`)},
	} {
		if _, _, err := generateCurrencyData(strings.NewReader(pair[0]), strings.NewReader(pair[1])); err == nil {
			t.Fatal("conflicting currency data succeeded")
		}
	}
}

func TestGeneratedFormatterReportsInvalidGoSafely(t *testing.T) {
	t.Parallel()
	if _, err := formatGenerated("test", []byte("not Go")); err == nil {
		t.Fatal("formatGenerated() succeeded")
	}
}

func TestSubdivisionHelpersRejectMalformedSourcesAndRanges(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"<", `<supplementalData><idValidity><id type="subdivision">aa1~</id></idValidity></supplementalData>`} {
		if _, err := parseSubdivisionValidity(strings.NewReader(input)); err == nil {
			t.Fatalf("parseSubdivisionValidity(%q) succeeded", input)
		}
	}
	input := `<supplementalData><idValidity><id type="region">AA</id><id type="subdivision" idStatus="regular">aa1</id></idValidity></supplementalData>`
	statuses, err := parseSubdivisionValidity(strings.NewReader(input))
	if err != nil || statuses["aa1"] != "regular" {
		t.Fatalf("statuses = %#v, %v", statuses, err)
	}
	for _, token := range []string{"a", "aa1~2~3", "aa-~2", "aa9~a", "aaz~a"} {
		if _, err := expandSubdivisionRange(token); err == nil {
			t.Fatalf("expandSubdivisionRange(%q) succeeded", token)
		}
	}
	for _, value := range []string{"aa", "aa-"} {
		if validLowerAlphanumeric(value) {
			t.Fatalf("validLowerAlphanumeric(%q) true", value)
		}
	}
	validity := `<supplementalData><idValidity><id type="subdivision" idStatus="regular">aa1</id></idValidity></supplementalData>`
	if _, err := generateSubdivisionData(strings.NewReader("<"), strings.NewReader("<")); err == nil {
		t.Fatal("invalid validity succeeded")
	}
	if _, err := generateSubdivisionData(strings.NewReader(validity), strings.NewReader("<")); err == nil {
		t.Fatal("invalid names succeeded")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }
func (errorReader) Close() error             { return nil }

type roundTripper func(*http.Request) (*http.Response, error)

func (transport roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type nilResponseDoer struct{}

func (nilResponseDoer) Do(*http.Request) (*http.Response, error) { return nil, nil }

type doerFunc func(*http.Request) (*http.Response, error)

func (doer doerFunc) Do(request *http.Request) (*http.Response, error) {
	return doer(request)
}

func TestDownloadTransportResultBoundary(t *testing.T) {
	t.Parallel()

	cause := errors.New("offline")
	if _, err := download(doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	}), "http://example.invalid", ""); !errors.Is(err, cause) {
		t.Fatalf("download(transport failure) error = %v", err)
	}

	payload := []byte("data")
	checksum := sha256.Sum256(payload)
	actual, err := download(doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
		}, nil
	}), "http://example.invalid", hex.EncodeToString(checksum[:]))
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatalf("download(success) = %q, %v", actual, err)
	}
}

func TestDownloadEnforcesTransportStatusSizeAndChecksum(t *testing.T) {
	t.Parallel()
	if _, err := download(http.DefaultClient, ":", ""); err == nil {
		t.Fatal("invalid request URL succeeded")
	}
	if _, err := download(nil, "http://invalid", ""); err == nil {
		t.Fatal("nil HTTP client succeeded")
	}
	if _, err := download(nilResponseDoer{}, "http://invalid", ""); err == nil {
		t.Fatal("nil HTTP response succeeded")
	}
	failed := &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	if _, err := download(failed, "http://invalid", ""); err == nil {
		t.Fatal("transport error succeeded")
	}
	readFailed := &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: errorReader{}}, nil
	})}
	if _, err := download(readFailed, "http://invalid", ""); err == nil {
		t.Fatal("read error succeeded")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/status":
			writer.WriteHeader(http.StatusTeapot)
		case "/large":
			_, _ = io.CopyN(writer, bytes.NewReader(make([]byte, maxSourceBytes+1)), maxSourceBytes+1)
		default:
			_, _ = writer.Write([]byte("data"))
		}
	}))
	defer func() {
		server.CloseClientConnections()
		server.Close()
	}()
	for _, path := range []string{"/status", "/large", "/data"} {
		if _, err := download(server.Client(), server.URL+path, "bad"); err == nil {
			t.Fatalf("download(%s) succeeded", path)
		}
	}
	checksum := sha256.Sum256([]byte("data"))
	payload, err := download(server.Client(), server.URL+"/data", hex.EncodeToString(checksum[:]))
	if err != nil || string(payload) != "data" {
		t.Fatalf("download success = %q, %v", payload, err)
	}
	payload, err = fetchRemote(server.URL+"/data", hex.EncodeToString(checksum[:]))
	if err != nil || string(payload) != "data" {
		t.Fatalf("fetchRemote success = %q, %v", payload, err)
	}
}

func TestGeneratorScalarAndRangeBoundaries(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"AA", "AZ", "ZA", "ZZ"} {
		if !validAlpha2(value) {
			t.Errorf("validAlpha2(%q) = false", value)
		}
	}
	for _, value := range []string{"@A", "[A", "A@", "A[", "aa", "A"} {
		if validAlpha2(value) {
			t.Errorf("validAlpha2(%q) = true", value)
		}
	}

	for _, token := range []string{"AA~A", "AZ~Z"} {
		codes, err := expandCodeRange(token)
		if err != nil || len(codes) != 1 {
			t.Errorf("expandCodeRange(%q) = %v, %v", token, codes, err)
		}
	}
	codes, err := expandCodeRange("AA~Z")
	if err != nil || len(codes) != 26 || codes[0] != "AA" || codes[25] != "AZ" {
		t.Fatalf("expandCodeRange(AA~Z) = %v, %v", codes, err)
	}

	for _, value := range []string{"AAA", "ZAA", "AZA", "AAZ", "ZZZ"} {
		if !validCurrencyCode(value) {
			t.Errorf("validCurrencyCode(%q) = false", value)
		}
	}
	for _, value := range []string{"@AA", "[AA", "A@A", "A[A", "AA@", "AA[", "aaa", "AA"} {
		if validCurrencyCode(value) {
			t.Errorf("validCurrencyCode(%q) = true", value)
		}
	}
	for _, value := range []string{"000", "900", "090", "009", "999"} {
		if !validNumericCode(value) {
			t.Errorf("validNumericCode(%q) = false", value)
		}
	}
	for _, value := range []string{"/00", ":00", "0/0", "0:0", "00/", "00:", "0000"} {
		if validNumericCode(value) {
			t.Errorf("validNumericCode(%q) = true", value)
		}
	}

	for _, entry := range []currencyXMLEntry{
		{Code: "AAA", Numeric: "000", Name: "lower", MinorUnits: "0"},
		{Code: "ZZZ", Numeric: "999", Name: "upper", MinorUnits: "9"},
	} {
		if _, err := activeCurrencyRecord(entry); err != nil {
			t.Errorf("activeCurrencyRecord(%#v) error = %v", entry, err)
		}
	}
	if _, err := activeCurrencyRecord(currencyXMLEntry{
		Code: "AAA", Numeric: "000", Name: "negative", MinorUnits: "-1",
	}); err == nil {
		t.Fatal("negative minor units were accepted")
	}
	for _, entry := range []currencyXMLEntry{
		{Code: "AAA", Numeric: "00A", Name: "invalid numeric", MinorUnits: "0"},
		{Code: "AAA", Numeric: "000", MinorUnits: "0"},
	} {
		if _, err := activeCurrencyRecord(entry); err == nil {
			t.Errorf("activeCurrencyRecord(%#v) succeeded", entry)
		}
	}
}

func TestSubdivisionScalarAndRangeBoundaries(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"aaa", "zzz", "aa0", "aa9", "a0z"} {
		if !validLowerAlphanumeric(value) {
			t.Errorf("validLowerAlphanumeric(%q) = false", value)
		}
	}
	for _, value := range []string{"aa@", "aa{", "aa/", "aa:", "aa"} {
		if validLowerAlphanumeric(value) {
			t.Errorf("validLowerAlphanumeric(%q) = true", value)
		}
	}
	for _, pair := range [][2]byte{{'a', 'z'}, {'z', 'a'}, {'0', '9'}, {'9', '0'}} {
		if !sameCharacterClass(pair[0], pair[1]) {
			t.Errorf("sameCharacterClass(%q, %q) = false", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]byte{{'a', '0'}, {'0', 'a'}, {'@', 'a'}} {
		if sameCharacterClass(pair[0], pair[1]) {
			t.Errorf("sameCharacterClass(%q, %q) = true", pair[0], pair[1])
		}
	}
	for _, id := range []string{"aa0", "az000", "za000", "zz999"} {
		if !validSubdivisionID(id) {
			t.Errorf("validSubdivisionID(%q) = false", id)
		}
	}
	for _, id := range []string{"aa", "aa0000", "@a0", "[a0", "a@0", "a[0"} {
		if validSubdivisionID(id) {
			t.Errorf("validSubdivisionID(%q) = true", id)
		}
	}

	for _, token := range []string{"aa0~0", "aaa~a", "aaz~z"} {
		ids, err := expandSubdivisionRange(token)
		if err != nil || len(ids) != 1 {
			t.Errorf("expandSubdivisionRange(%q) = %v, %v", token, ids, err)
		}
	}
	ids, err := expandSubdivisionRange("aa0~9")
	if err != nil || len(ids) != 10 || ids[0] != "aa0" || ids[9] != "aa9" {
		t.Fatalf("expandSubdivisionRange(aa0~9) = %v, %v", ids, err)
	}
	ids, err = expandSubdivisionRange("aaa~z")
	if err != nil || len(ids) != 26 || ids[0] != "aaa" || ids[25] != "aaz" {
		t.Fatalf("expandSubdivisionRange(aaa~z) = %v, %v", ids, err)
	}
}

func TestGeneratorsContinueAfterIgnoredRows(t *testing.T) {
	t.Parallel()

	validity := `<supplementalData><idValidity><id type="region" idStatus="regular">AA</id></idValidity></supplementalData>`
	for _, ignored := range []string{
		`<territoryCodes type="AC" numeric="001"/>`,
		`<territoryCodes type="AC" alpha3="ACC"/>`,
		`<territoryCodes type="BB" numeric="002" alpha3="BBB"/>`,
	} {
		mappings := `<supplementalData><codeMappings>` + ignored +
			`<territoryCodes type="AA" numeric="001" alpha3="AAA"/>` +
			`</codeMappings></supplementalData>`
		output, err := generateCountryData(strings.NewReader(validity), strings.NewReader(mappings))
		if err != nil || !strings.Contains(string(output), `"AA"`) {
			t.Fatalf("country generator stopped after ignored row: %v", err)
		}
	}

	currentRows := `<CcyNtry><CcyNm>Ignored</CcyNm></CcyNtry>` +
		`<CcyNtry><CcyNm>Euro</CcyNm><Ccy>EUR</Ccy><CcyNbr>978</CcyNbr><CcyMnrUnts>2</CcyMnrUnts></CcyNtry>`
	current := `<ISO_4217 Pblshd="v"><CcyTbl>` + currentRows + `</CcyTbl></ISO_4217>`
	historic := `<ISO_4217 Pblshd="v"><HstrcCcyTbl></HstrcCcyTbl></ISO_4217>`
	output, _, err := generateCurrencyData(strings.NewReader(current), strings.NewReader(historic))
	if err != nil || !strings.Contains(string(output), `"EUR"`) {
		t.Fatalf("currency generator stopped after ignored current row: %v", err)
	}

	current = `<ISO_4217 Pblshd="v"><CcyTbl></CcyTbl></ISO_4217>`
	historicRows := `<HstrcCcyNtry><CcyNm>Ignored</CcyNm></HstrcCcyNtry>` +
		`<HstrcCcyNtry><CcyNm>Markka</CcyNm><Ccy>FIM</Ccy><CcyNbr>246</CcyNbr><WthdrwlDt>2002-03</WthdrwlDt></HstrcCcyNtry>`
	historic = `<ISO_4217 Pblshd="v"><HstrcCcyTbl>` + historicRows + `</HstrcCcyTbl></ISO_4217>`
	output, _, err = generateCurrencyData(strings.NewReader(current), strings.NewReader(historic))
	if err != nil || !strings.Contains(string(output), `"FIM"`) {
		t.Fatalf("currency generator stopped after ignored historic row: %v", err)
	}
}

func TestRegionParserSkipsDigitBoundaries(t *testing.T) {
	t.Parallel()

	input := `<supplementalData><idValidity><id type="region" idStatus="regular">000 999 AA</id></idValidity></supplementalData>`
	statuses, err := parseRegionValidity(strings.NewReader(input))
	if err != nil || len(statuses) != 1 || statuses["AA"] != "regular" {
		t.Fatalf("parseRegionValidity() = %#v, %v", statuses, err)
	}
}
