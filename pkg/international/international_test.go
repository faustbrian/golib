package international_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	international "github.com/faustbrian/golib/pkg/international"
)

func TestStatusHasStableTextAndKnownSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status international.Status
		text   string
		known  bool
	}{
		{international.StatusOfficial, "official", true},
		{international.StatusReserved, "reserved", true},
		{international.StatusTransitional, "transitional", true},
		{international.StatusDeleted, "deleted", true},
		{international.StatusUserAssigned, "user-assigned", true},
		{international.StatusHistoric, "historic", true},
		{international.StatusUnknown, "unknown", false},
	}

	for _, test := range tests {
		t.Run(test.text, func(t *testing.T) {
			t.Parallel()
			parsed, err := international.ParseStatus(test.text)
			if err != nil || parsed != test.status {
				t.Fatalf("ParseStatus(%q) = %v, %v", test.text, parsed, err)
			}

			if got := test.status.String(); got != test.text {
				t.Fatalf("String() = %q, want %q", got, test.text)
			}
			if got := test.status.Known(); got != test.known {
				t.Fatalf("Known() = %v, want %v", got, test.known)
			}
		})
	}
}

func TestUnknownStatusValuesRemainUnknown(t *testing.T) {
	t.Parallel()

	status := international.Status(255)
	if got := status.String(); got != "unknown" {
		t.Fatalf("String() = %q, want unknown", got)
	}
	if status.Known() {
		t.Fatal("Known() = true, want false")
	}
}

func TestStatusTextAndJSONUseStableWireSpellings(t *testing.T) {
	t.Parallel()

	status, err := international.ParseStatus("deleted")
	if err != nil || status != international.StatusDeleted {
		t.Fatalf("ParseStatus(deleted) = %v, %v", status, err)
	}
	text, err := status.MarshalText()
	if err != nil || string(text) != "deleted" {
		t.Fatalf("MarshalText() = %q, %v", text, err)
	}
	payload, err := json.Marshal(status)
	if err != nil || string(payload) != `"deleted"` {
		t.Fatalf("MarshalJSON() = %s, %v", payload, err)
	}
	var decoded international.Status
	if err := json.Unmarshal(payload, &decoded); err != nil || decoded != status {
		t.Fatalf("UnmarshalJSON() = %v, %v", decoded, err)
	}
	unchanged := international.StatusOfficial
	for _, input := range []string{`"obsolete"`, `1`, `null`} {
		if err := json.Unmarshal([]byte(input), &unchanged); !errors.Is(err, international.ErrInvalid) ||
			unchanged != international.StatusOfficial {
			t.Fatalf("UnmarshalJSON(%s) = %v, status %v", input, err, unchanged)
		}
	}
	if _, err := international.ParseStatus("obsolete"); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("ParseStatus(obsolete) error = %v", err)
	}
	invalid := international.Status(255)
	if _, err := invalid.MarshalText(); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("invalid MarshalText() error = %v", err)
	}
	if _, err := json.Marshal(invalid); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("invalid MarshalJSON() error = %v", err)
	}
}

func TestProvenanceValidationAndJSONRoundTrip(t *testing.T) {
	t.Parallel()

	retrieved := time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC)
	provenance := international.Provenance{
		Dataset:         "iana-language-subtag-registry",
		Source:          "https://www.iana.org/assignments/language-subtag-registry",
		RetrievedAt:     retrieved,
		UpstreamVersion: "2026-06-15",
		License:         "IANA Terms of Service",
		SHA256:          "20ba9c7f1e09556fd40fb4e69c858b9f4b8b2dd584346486765c97cb3f974b8a",
		Generator:       "international-generate/v1",
		Transformations: []string{"parse RFC 5646 records", "sort by type and subtag"},
	}

	if err := provenance.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	payload, err := json.Marshal(provenance)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded international.Provenance
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !provenance.Equal(decoded) {
		t.Fatalf("round trip = %#v, want %#v", decoded, provenance)
	}
}

func TestProvenanceRejectsIncompleteOrUnsafeMetadata(t *testing.T) {
	t.Parallel()

	valid := international.Provenance{
		Dataset:         "dataset",
		Source:          "https://example.invalid/data",
		RetrievedAt:     time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC),
		UpstreamVersion: "v1",
		License:         "CC0-1.0",
		SHA256:          "20ba9c7f1e09556fd40fb4e69c858b9f4b8b2dd584346486765c97cb3f974b8a",
		Generator:       "generator/v1",
		Transformations: []string{"parse"},
	}

	tests := map[string]func(*international.Provenance){
		"dataset":          func(value *international.Provenance) { value.Dataset = "" },
		"source":           func(value *international.Provenance) { value.Source = "" },
		"retrieval date":   func(value *international.Provenance) { value.RetrievedAt = time.Time{} },
		"upstream version": func(value *international.Provenance) { value.UpstreamVersion = "" },
		"license":          func(value *international.Provenance) { value.License = "" },
		"checksum":         func(value *international.Provenance) { value.SHA256 = "not-a-checksum" },
		"generator":        func(value *international.Provenance) { value.Generator = "" },
		"transformations":  func(value *international.Provenance) { value.Transformations = nil },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			candidate := valid
			mutate(&candidate)
			if err := candidate.Validate(); !errors.Is(err, international.ErrInvalidProvenance) {
				t.Fatalf("Validate() error = %v, want ErrInvalidProvenance", err)
			}
		})
	}
}

func TestErrorDiagnosticsAreBoundedAndDoNotEchoInput(t *testing.T) {
	t.Parallel()

	err := international.NewParseError("phone", "invalid syntax")
	if !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	if got, want := err.Error(), "international: invalid phone: invalid syntax"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}

	longReason := make([]byte, 1_000)
	for index := range longReason {
		longReason[index] = 'x'
	}
	if got := len(international.NewParseError("postal", string(longReason)).Error()); got > 256 {
		t.Fatalf("diagnostic length = %d, want <= 256", got)
	}

	unicodeReason := "reason: " + string(make([]byte, 0, 240))
	for len(unicodeReason) < 260 {
		unicodeReason += "é"
	}
	diagnostic := international.NewParseError("locale", unicodeReason).Error()
	if !utf8.ValidString(diagnostic) {
		t.Fatalf("Error() returned invalid UTF-8: %q", diagnostic)
	}
	if len(diagnostic) > 256 {
		t.Fatalf("Unicode diagnostic length = %d, want <= 256", len(diagnostic))
	}
}

func TestErrorDiagnosticsRejectUntrustedKindsWithoutPanicking(t *testing.T) {
	t.Parallel()

	secret := strings.Repeat("customer-phone-358401234567", 32)
	diagnostic := international.NewParseError(secret, "invalid syntax").Error()
	if strings.Contains(diagnostic, secret) {
		t.Fatal("diagnostic echoed an untrusted kind")
	}
	if len(diagnostic) > 256 {
		t.Fatalf("diagnostic length = %d, want <= 256", len(diagnostic))
	}
}

func TestDatasetDiffClassifiesCompatibilityRelevantChanges(t *testing.T) {
	t.Parallel()

	before := []international.Record{
		{ID: "AA", Status: international.StatusOfficial, Fingerprint: "one"},
		{ID: "BB", Status: international.StatusOfficial, Fingerprint: "two"},
		{ID: "CC", Status: international.StatusOfficial, Fingerprint: "three"},
		{ID: "DD", Status: international.StatusOfficial, Fingerprint: "four"},
	}
	after := []international.Record{
		{ID: "AA", Status: international.StatusReserved, Fingerprint: "one"},
		{ID: "CC", Status: international.StatusOfficial, Fingerprint: "changed"},
		{ID: "DD", Status: international.StatusOfficial, Fingerprint: "four", AliasOf: "CC"},
		{ID: "EE", Status: international.StatusOfficial, Fingerprint: "five"},
	}

	diff, err := international.DiffRecords(before, after)
	if err != nil {
		t.Fatalf("DiffRecords() error = %v", err)
	}
	if got, want := diff.Added, []string{"EE"}; !equalStrings(got, want) {
		t.Fatalf("Added = %v, want %v", got, want)
	}
	if got, want := diff.Removed, []string{"BB"}; !equalStrings(got, want) {
		t.Fatalf("Removed = %v, want %v", got, want)
	}
	if got, want := diff.StatusChanged, []string{"AA"}; !equalStrings(got, want) {
		t.Fatalf("StatusChanged = %v, want %v", got, want)
	}
	if got, want := diff.MetadataChanged, []string{"CC"}; !equalStrings(got, want) {
		t.Fatalf("MetadataChanged = %v, want %v", got, want)
	}
	if got, want := diff.AliasesChanged, []string{"DD"}; !equalStrings(got, want) {
		t.Fatalf("AliasesChanged = %v, want %v", got, want)
	}
}

func TestDatasetDiffRejectsDuplicateAndUnboundedRecords(t *testing.T) {
	t.Parallel()

	_, err := international.DiffRecords(
		[]international.Record{{ID: "AA"}, {ID: "AA"}},
		nil,
	)
	if !errors.Is(err, international.ErrInvalidDataset) {
		t.Fatalf("duplicate error = %v, want ErrInvalidDataset", err)
	}

	_, err = international.DiffRecords(
		nil,
		[]international.Record{{ID: "AA"}, {ID: "AA"}},
	)
	if !errors.Is(err, international.ErrInvalidDataset) {
		t.Fatalf("updated duplicate error = %v, want ErrInvalidDataset", err)
	}

	records := make([]international.Record, international.MaxDatasetRecords+1)
	_, err = international.DiffRecords(records, nil)
	if !errors.Is(err, international.ErrResourceLimit) {
		t.Fatalf("limit error = %v, want ErrResourceLimit", err)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
