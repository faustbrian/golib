package validate

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/specification"
)

func TestRegisteredHTTPStatusCodesMatchPinnedIANARegistry(t *testing.T) {
	t.Parallel()

	data, err := specification.Read("registries/iana/http-status-codes-1.csv")
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	registered := make(map[string]bool)
	for _, record := range records[1:] {
		if len(record) != 3 {
			t.Fatalf("registry record = %#v", record)
		}
		if !strings.Contains(record[0], "-") && record[1] != "Unassigned" {
			registered[record[0]] = true
		}
	}

	for code := 100; code <= 599; code++ {
		value := strconv.Itoa(code)
		if got, want := isRegisteredHTTPStatusCode(value), registered[value]; got != want {
			t.Errorf("isRegisteredHTTPStatusCode(%q) = %t, want %t", value, got, want)
		}
	}
}
