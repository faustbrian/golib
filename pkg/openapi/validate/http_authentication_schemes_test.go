package validate

import (
	"bytes"
	"encoding/csv"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/specification"
)

func TestRegisteredHTTPAuthenticationSchemesMatchPinnedIANARegistry(t *testing.T) {
	t.Parallel()

	data, err := specification.Read("registries/iana/authschemes.csv")
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	for _, record := range records[1:] {
		if len(record) != 3 {
			t.Fatalf("registry record = %#v", record)
		}
		if !isRegisteredHTTPAuthenticationScheme(record[0]) {
			t.Errorf("registered authentication scheme %q was not recognized", record[0])
		}
	}
	if isRegisteredHTTPAuthenticationScheme("custom") {
		t.Fatal("unregistered authentication scheme was recognized")
	}
}
