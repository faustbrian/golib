// Package generate produces pinned international metadata tables.
package generate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	cldrValidityURL = "https://raw.githubusercontent.com/unicode-org/cldr/" +
		"release-48-2/common/validity/region.xml"
	cldrValiditySHA256 = "e751e0eedd46b52c38f3cdb72b0fab61ac8b48e052e8b28ba74b6ac26c4c8cb1"
	cldrMappingsURL    = "https://raw.githubusercontent.com/unicode-org/cldr/" +
		"release-48-2/common/supplemental/supplementalData.xml"
	cldrMappingsSHA256 = "cd2af39aef82fdbfba4d591c87548203350538ad2318486d104b3b38b8d62f1a"
	sixCurrentURL      = "https://www.six-group.com/dam/download/financial-information/" +
		"data-center/iso-currrency/lists/list-one.xml"
	sixCurrentSHA256 = "838dfb991648cf36df939edd5fe3811737962b75a32252847d239cedd1e291c9"
	sixHistoricURL   = "https://www.six-group.com/dam/download/financial-information/" +
		"data-center/iso-currrency/lists/list-three.xml"
	sixHistoricSHA256          = "98fde2423cdb916dd59dcf5fe96222edad8fa198d865c1c83dbc464b9cc52387"
	cldrSubdivisionValidityURL = "https://raw.githubusercontent.com/unicode-org/cldr/" +
		"release-48-2/common/validity/subdivision.xml"
	cldrSubdivisionValiditySHA256 = "93b12c9d55938266c96d44a7ccbb66800afeef4f9dd48b0dc16edfab89833d95"
	cldrSubdivisionNamesURL       = "https://raw.githubusercontent.com/unicode-org/cldr/" +
		"release-48-2/common/subdivisions/en.xml"
	cldrSubdivisionNamesSHA256 = "997a14da1144bb66f36a829db1783afe41f7529e33070afbe964bdd8e387b1d2"
	maxSourceBytes             = 8 << 20
)

type validityDocument struct {
	IDs []validityEntry `xml:"idValidity>id"`
}

type validityEntry struct {
	Type   string `xml:"type,attr"`
	Status string `xml:"idStatus,attr"`
	Codes  string `xml:",chardata"`
}

type mappingDocument struct {
	Mappings []territoryMapping `xml:"codeMappings>territoryCodes"`
}

type territoryMapping struct {
	Alpha2  string `xml:"type,attr"`
	Alpha3  string `xml:"alpha3,attr"`
	Numeric string `xml:"numeric,attr"`
}

type countryRecord struct {
	alpha2  string
	alpha3  string
	numeric int
	status  string
}

type currencyDocument struct {
	Published string             `xml:"Pblshd,attr"`
	Current   []currencyXMLEntry `xml:"CcyTbl>CcyNtry"`
	Historic  []currencyXMLEntry `xml:"HstrcCcyTbl>HstrcCcyNtry"`
}

type currencyXMLEntry struct {
	Name       string `xml:"CcyNm"`
	Code       string `xml:"Ccy"`
	Numeric    string `xml:"CcyNbr"`
	MinorUnits string `xml:"CcyMnrUnts"`
	Withdrawal string `xml:"WthdrwlDt"`
}

type currencyRecord struct {
	code          string
	numeric       string
	minorUnits    int
	hasMinorUnits bool
	name          string
	status        string
	history       string
}

type subdivisionNamesDocument struct {
	Names []subdivisionName `xml:"localeDisplayNames>subdivisions>subdivision"`
}

type subdivisionName struct {
	ID    string `xml:"type,attr"`
	Alt   string `xml:"alt,attr"`
	Value string `xml:",chardata"`
}

// Run executes deterministic acquisition and generation for command arguments.
func Run(arguments []string) error {
	return run(arguments, fetchRemote, os.WriteFile)
}

func fetchRemote(source, checksum string) ([]byte, error) {
	return download(&http.Client{Timeout: 30 * time.Second}, source, checksum)
}

type fetchFunc func(string, string) ([]byte, error)
type writeFileFunc func(string, []byte, os.FileMode) error

func run(arguments []string, fetch fetchFunc, writeFile writeFileFunc) error {
	flags := flag.NewFlagSet("international-generate", flag.ContinueOnError)
	countryOutput := flags.String("country-output", "", "path for generated country data")
	currencyOutput := flags.String("currency-output", "", "path for generated currency data")
	subdivisionOutput := flags.String("subdivision-output", "", "path for generated subdivision data")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("positional arguments are not supported")
	}
	if *countryOutput == "" && *currencyOutput == "" && *subdivisionOutput == "" {
		return errors.New("at least one output is required")
	}
	if fetch == nil || writeFile == nil {
		return errors.New("generator dependencies are required")
	}

	if *countryOutput != "" {
		validity, err := fetch(cldrValidityURL, cldrValiditySHA256)
		if err != nil {
			return err
		}
		mappings, err := fetch(cldrMappingsURL, cldrMappingsSHA256)
		if err != nil {
			return err
		}
		generated, err := generateCountryData(bytes.NewReader(validity), bytes.NewReader(mappings))
		if err != nil {
			return err
		}
		if err := writeFile(*countryOutput, generated, 0o644); err != nil {
			return fmt.Errorf("write country data: %w", err)
		}
	}
	if *currencyOutput != "" {
		current, err := fetch(sixCurrentURL, sixCurrentSHA256)
		if err != nil {
			return err
		}
		historic, err := fetch(sixHistoricURL, sixHistoricSHA256)
		if err != nil {
			return err
		}
		generated, _, err := generateCurrencyData(bytes.NewReader(current), bytes.NewReader(historic))
		if err != nil {
			return err
		}
		if err := writeFile(*currencyOutput, generated, 0o644); err != nil {
			return fmt.Errorf("write currency data: %w", err)
		}
	}
	if *subdivisionOutput != "" {
		validity, err := fetch(cldrSubdivisionValidityURL, cldrSubdivisionValiditySHA256)
		if err != nil {
			return err
		}
		names, err := fetch(cldrSubdivisionNamesURL, cldrSubdivisionNamesSHA256)
		if err != nil {
			return err
		}
		generated, err := generateSubdivisionData(bytes.NewReader(validity), bytes.NewReader(names))
		if err != nil {
			return err
		}
		if err := writeFile(*subdivisionOutput, generated, 0o644); err != nil {
			return fmt.Errorf("write subdivision data: %w", err)
		}
	}
	return nil
}

func parseRegionValidity(reader io.Reader) (map[string]string, error) {
	var document validityDocument
	if err := xml.NewDecoder(io.LimitReader(reader, maxSourceBytes+1)).Decode(&document); err != nil {
		return nil, fmt.Errorf("decode CLDR region validity: %w", err)
	}

	statuses := make(map[string]string)
	for _, entry := range document.IDs {
		if entry.Type != "region" {
			continue
		}
		for _, token := range strings.Fields(entry.Codes) {
			if token[0] >= '0' && token[0] <= '9' {
				continue
			}
			codes, err := expandCodeRange(token)
			if err != nil {
				return nil, err
			}
			for _, code := range codes {
				statuses[code] = entry.Status
			}
		}
	}
	return statuses, nil
}

func expandCodeRange(token string) ([]string, error) {
	parts := strings.Split(token, "~")
	if len(parts) == 1 && validAlpha2(parts[0]) {
		return parts, nil
	}
	if len(parts) != 2 || !validAlpha2(parts[0]) || len(parts[1]) != 1 {
		return nil, fmt.Errorf("invalid CLDR region range %q", token)
	}
	start := parts[0]
	end := parts[1][0]
	if end < start[1] || end < 'A' || end > 'Z' {
		return nil, fmt.Errorf("invalid CLDR region range %q", token)
	}
	codes := make([]string, 0, int(end-start[1])+1)
	for current := start[1]; current <= end; current++ {
		codes = append(codes, string([]byte{start[0], current}))
	}
	return codes, nil
}

func validAlpha2(value string) bool {
	return len(value) == 2 && value[0] >= 'A' && value[0] <= 'Z' &&
		value[1] >= 'A' && value[1] <= 'Z'
}

func generateCountryData(validityReader, mappingsReader io.Reader) ([]byte, error) {
	statuses, err := parseRegionValidity(validityReader)
	if err != nil {
		return nil, err
	}

	var mappings mappingDocument
	if err := xml.NewDecoder(io.LimitReader(mappingsReader, maxSourceBytes+1)).Decode(&mappings); err != nil {
		return nil, fmt.Errorf("decode CLDR territory mappings: %w", err)
	}

	records := make([]countryRecord, 0, len(mappings.Mappings))
	for _, mapping := range mappings.Mappings {
		if mapping.Alpha3 == "" || mapping.Numeric == "" {
			continue
		}
		numeric, err := strconv.Atoi(mapping.Numeric)
		if err != nil {
			return nil, fmt.Errorf("parse numeric country code: %w", err)
		}
		status, include := generatedStatus(mapping.Alpha2, statuses[mapping.Alpha2])
		if !include {
			continue
		}
		records = append(records, countryRecord{
			alpha2: mapping.Alpha2, alpha3: mapping.Alpha3, numeric: numeric, status: status,
		})
	}
	sort.Slice(records, func(left, right int) bool { return records[left].alpha2 < records[right].alpha2 })

	var output bytes.Buffer
	output.WriteString("// Code generated by go generate; DO NOT EDIT.\n\n")
	output.WriteString("package country\n\n")
	output.WriteString("import international \"github.com/faustbrian/golib/pkg/international\"\n\n")
	output.WriteString("var countryRecords = map[string]record{\n")
	for _, record := range records {
		fmt.Fprintf(&output, "\t%q: {alpha3: %q, numeric: %d, status: international.%s},\n",
			record.alpha2, record.alpha3, record.numeric, record.status)
	}
	output.WriteString("}\n\n")
	output.WriteString("var officialCodes = [...]string{")
	separator := ""
	for _, record := range records {
		if record.status != "StatusOfficial" {
			continue
		}
		fmt.Fprintf(&output, "%s%q", separator, record.alpha2)
		separator = ", "
	}
	output.WriteString("}\n")

	return formatGenerated("country", output.Bytes())
}

func generatedStatus(code, validityStatus string) (string, bool) {
	switch validityStatus {
	case "regular":
		if code == "XK" {
			return "StatusUserAssigned", true
		}
		return "StatusOfficial", true
	case "deprecated":
		return "StatusDeleted", true
	case "reserved":
		return "StatusReserved", true
	case "private_use", "special":
		return "StatusUserAssigned", true
	case "unknown":
		return "StatusUnknown", true
	default:
		return "", false
	}
}

func generateCurrencyData(currentReader, historicReader io.Reader) ([]byte, string, error) {
	current, err := decodeCurrencyDocument(currentReader)
	if err != nil {
		return nil, "", err
	}
	historic, err := decodeCurrencyDocument(historicReader)
	if err != nil {
		return nil, "", err
	}
	if current.Published == "" || current.Published != historic.Published {
		return nil, "", errors.New("currency lists have missing or mismatched publication dates")
	}

	records := make(map[string]currencyRecord)
	for _, entry := range current.Current {
		if entry.Code == "" {
			continue
		}
		record, err := activeCurrencyRecord(entry)
		if err != nil {
			return nil, "", err
		}
		if existing, exists := records[record.code]; exists && existing != record {
			return nil, "", fmt.Errorf("conflicting current currency %s", record.code)
		}
		records[record.code] = record
	}
	for _, entry := range historic.Historic {
		if entry.Code == "" {
			continue
		}
		record, err := historicCurrencyRecord(entry)
		if err != nil {
			return nil, "", err
		}
		if existing, exists := records[record.code]; exists {
			if existing.numeric != record.numeric {
				return nil, "", fmt.Errorf("conflicting historic currency %s", record.code)
			}
			if existing.status == "StatusOfficial" {
				existing.history = mergeMetadata(existing.history, record.history)
				records[record.code] = existing
				continue
			}
			existing.history = mergeMetadata(existing.history, record.history)
			records[record.code] = existing
			continue
		}
		records[record.code] = record
	}

	codes := make([]string, 0, len(records))
	for code := range records {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	var output bytes.Buffer
	output.WriteString("// Code generated by go generate; DO NOT EDIT.\n\n")
	output.WriteString("package currency\n\n")
	output.WriteString("import international \"github.com/faustbrian/golib/pkg/international\"\n\n")
	output.WriteString("var currencyRecords = map[string]record{\n")
	for _, code := range codes {
		record := records[code]
		fmt.Fprintf(&output, "\t%q: {numeric: %q", code, record.numeric)
		if record.hasMinorUnits {
			fmt.Fprintf(&output, ", minorUnits: %d, hasMinorUnits: true", record.minorUnits)
		}
		fmt.Fprintf(&output, ", name: %q, status: international.%s", record.name, record.status)
		if record.history != "" {
			fmt.Fprintf(&output, ", history: %q", record.history)
		}
		output.WriteString("},\n")
	}
	output.WriteString("}\n\n")
	output.WriteString("var activeCodes = [...]string{")
	separator := ""
	for _, code := range codes {
		if records[code].status != "StatusOfficial" {
			continue
		}
		fmt.Fprintf(&output, "%s%q", separator, code)
		separator = ", "
	}
	output.WriteString("}\n")

	formatted, err := formatGenerated("currency", output.Bytes())
	return formatted, current.Published, err
}

func decodeCurrencyDocument(reader io.Reader) (currencyDocument, error) {
	var document currencyDocument
	if err := xml.NewDecoder(io.LimitReader(reader, maxSourceBytes+1)).Decode(&document); err != nil {
		return currencyDocument{}, fmt.Errorf("decode ISO 4217 list: %w", err)
	}
	return document, nil
}

func activeCurrencyRecord(entry currencyXMLEntry) (currencyRecord, error) {
	if !validCurrencyCode(entry.Code) || !validNumericCode(entry.Numeric) || entry.Name == "" {
		return currencyRecord{}, errors.New("invalid current currency entry")
	}
	record := currencyRecord{
		code: entry.Code, numeric: entry.Numeric, name: entry.Name, status: "StatusOfficial",
	}
	if entry.MinorUnits == "N.A." {
		return record, nil
	}
	minorUnits, err := strconv.Atoi(entry.MinorUnits)
	if err != nil || minorUnits < 0 || minorUnits > 9 {
		return currencyRecord{}, errors.New("invalid currency minor units")
	}
	record.minorUnits = minorUnits
	record.hasMinorUnits = true
	return record, nil
}

func historicCurrencyRecord(entry currencyXMLEntry) (currencyRecord, error) {
	if !validCurrencyCode(entry.Code) || (entry.Numeric != "" && !validNumericCode(entry.Numeric)) ||
		entry.Name == "" || entry.Withdrawal == "" {
		return currencyRecord{}, errors.New("invalid historic currency entry")
	}
	return currencyRecord{
		code: entry.Code, numeric: entry.Numeric, status: "StatusHistoric",
		history: entry.Name + "\t" + entry.Withdrawal,
	}, nil
}

func mergeMetadata(left, right string) string {
	if left == "" {
		return right
	}
	values := strings.Split(left, "|")
	for _, value := range values {
		if value == right {
			return left
		}
	}
	values = append(values, right)
	sort.Strings(values)
	return strings.Join(values, "|")
}

func validCurrencyCode(value string) bool {
	return len(value) == 3 && value[0] >= 'A' && value[0] <= 'Z' &&
		value[1] >= 'A' && value[1] <= 'Z' && value[2] >= 'A' && value[2] <= 'Z'
}

func validNumericCode(value string) bool {
	return len(value) == 3 && value[0] >= '0' && value[0] <= '9' &&
		value[1] >= '0' && value[1] <= '9' && value[2] >= '0' && value[2] <= '9'
}

func generateSubdivisionData(validityReader, namesReader io.Reader) ([]byte, error) {
	statuses, err := parseSubdivisionValidity(validityReader)
	if err != nil {
		return nil, err
	}

	var namesDocument subdivisionNamesDocument
	if err := xml.NewDecoder(io.LimitReader(namesReader, maxSourceBytes+1)).Decode(&namesDocument); err != nil {
		return nil, fmt.Errorf("decode CLDR subdivision names: %w", err)
	}
	names := make(map[string]string)
	for _, entry := range namesDocument.Names {
		if entry.Alt == "" {
			names[entry.ID] = entry.Value
		}
	}

	ids := make([]string, 0, len(statuses))
	for id, status := range statuses {
		if !validSubdivisionID(id) || (status != "regular" && status != "deprecated") {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var output bytes.Buffer
	output.WriteString("// Code generated by go generate; DO NOT EDIT.\n\n")
	output.WriteString("package subdivision\n\n")
	output.WriteString("import international \"github.com/faustbrian/golib/pkg/international\"\n\n")
	output.WriteString("var subdivisionRecords = map[string]record{\n")
	for _, id := range ids {
		code := subdivisionCode(id)
		status := "StatusOfficial"
		if statuses[id] == "deprecated" {
			status = "StatusDeleted"
		}
		fmt.Fprintf(&output, "\t%q: {", code)
		if name := names[id]; name != "" {
			fmt.Fprintf(&output, "name: %q, ", name)
		}
		fmt.Fprintf(&output, "status: international.%s},\n", status)
	}
	output.WriteString("}\n\n")
	output.WriteString("var currentSubdivisionCodes = [...]string{")
	separator := ""
	for _, id := range ids {
		if statuses[id] != "regular" {
			continue
		}
		fmt.Fprintf(&output, "%s%q", separator, subdivisionCode(id))
		separator = ", "
	}
	output.WriteString("}\n")

	return formatGenerated("subdivision", output.Bytes())
}

func formatGenerated(kind string, source []byte) ([]byte, error) {
	formatted, err := format.Source(source)
	if err != nil {
		return nil, fmt.Errorf("format %s data: %w", kind, err)
	}
	return formatted, nil
}

func parseSubdivisionValidity(reader io.Reader) (map[string]string, error) {
	var document validityDocument
	if err := xml.NewDecoder(io.LimitReader(reader, maxSourceBytes+1)).Decode(&document); err != nil {
		return nil, fmt.Errorf("decode CLDR subdivision validity: %w", err)
	}
	statuses := make(map[string]string)
	for _, entry := range document.IDs {
		if entry.Type != "subdivision" {
			continue
		}
		for _, token := range strings.Fields(entry.Codes) {
			ids, err := expandSubdivisionRange(token)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				statuses[id] = entry.Status
			}
		}
	}
	return statuses, nil
}

func expandSubdivisionRange(token string) ([]string, error) {
	parts := strings.Split(token, "~")
	if len(parts) == 1 && validLowerAlphanumeric(parts[0]) {
		return parts, nil
	}
	if len(parts) != 2 || !validLowerAlphanumeric(parts[0]) || len(parts[1]) != 1 {
		return nil, fmt.Errorf("invalid CLDR subdivision range %q", token)
	}
	start := parts[0]
	end := parts[1][0]
	if end < start[len(start)-1] || !sameCharacterClass(start[len(start)-1], end) {
		return nil, fmt.Errorf("invalid CLDR subdivision range %q", token)
	}
	ids := make([]string, 0, int(end-start[len(start)-1])+1)
	for current := start[len(start)-1]; current <= end; current++ {
		ids = append(ids, start[:len(start)-1]+string(current))
	}
	return ids, nil
}

func validLowerAlphanumeric(value string) bool {
	if len(value) < 3 {
		return false
	}
	for index := range value {
		character := value[index]
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func sameCharacterClass(left, right byte) bool {
	return (left >= 'a' && left <= 'z' && right >= 'a' && right <= 'z') ||
		(left >= '0' && left <= '9' && right >= '0' && right <= '9')
}

func validSubdivisionID(id string) bool {
	return len(id) >= 3 && len(id) <= 5 && id[0] >= 'a' && id[0] <= 'z' &&
		id[1] >= 'a' && id[1] <= 'z' && validLowerAlphanumeric(id)
}

func subdivisionCode(id string) string {
	return strings.ToUpper(id[:2] + "-" + id[2:])
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func download(client httpDoer, source, expectedChecksum string) ([]byte, error) {
	if client == nil {
		return nil, errors.New("download dataset: HTTP client is required")
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, source, nil)
	if err != nil {
		return nil, fmt.Errorf("create dataset request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download dataset: %w", err)
	}
	if response == nil {
		return nil, errors.New("download dataset: empty HTTP response")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download dataset: HTTP %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxSourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read dataset: %w", err)
	}
	if len(payload) > maxSourceBytes {
		return nil, errors.New("download dataset: source exceeds byte limit")
	}
	checksum := sha256.Sum256(payload)
	if hex.EncodeToString(checksum[:]) != expectedChecksum {
		return nil, errors.New("download dataset: checksum mismatch")
	}
	return payload, nil
}
