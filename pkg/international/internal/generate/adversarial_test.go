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
	defer server.Close()
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
