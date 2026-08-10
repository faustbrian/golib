package httpsignature

import (
	"errors"
	"reflect"
	"testing"
)

func TestDigestPreferencesParseOfficialExamplesAndRoundTrip(t *testing.T) {
	t.Parallel()

	preferences, err := ParseDigestPreferences([]string{"sha-512=3, sha-256=10, unixsum=0"})
	if err != nil {
		t.Fatalf("ParseDigestPreferences() error = %v", err)
	}
	want := []DigestPreference{
		{Algorithm: SHA512, Weight: 3},
		{Algorithm: SHA256, Weight: 10},
		{Algorithm: DigestAlgorithm("unixsum"), Weight: 0},
	}
	if got := preferences.Entries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Entries() = %#v, want %#v", got, want)
	}
	if got := preferences.String(); got != "sha-512=3, sha-256=10, unixsum=0" {
		t.Fatalf("String() = %q", got)
	}

	constructed, err := NewDigestPreferences([]DigestPreference{
		{Algorithm: SHA256, Weight: 1},
		{Algorithm: SHA512, Weight: 10},
	})
	if err != nil {
		t.Fatalf("NewDigestPreferences() error = %v", err)
	}
	if got := constructed.String(); got != "sha-256=1, sha-512=10" {
		t.Fatalf("constructed String() = %q", got)
	}
}

func TestDigestPreferencesRejectInvalidDictionaryMembers(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"sha-256=11",
		"sha-256=-1",
		"sha-256=1.0",
		"sha-256",
		"sha-256=1;foo",
		"sha-256=1, sha-256=2",
	}
	for _, value := range tests {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseDigestPreferences([]string{value}); !errors.Is(err, ErrInvalidDigestPreferences) {
				t.Fatalf("ParseDigestPreferences(%q) error = %v", value, err)
			}
		})
	}

	if _, err := NewDigestPreferences([]DigestPreference{{Algorithm: SHA256, Weight: 1}, {Algorithm: SHA256, Weight: 2}}); !errors.Is(err, ErrInvalidDigestPreferences) {
		t.Fatalf("NewDigestPreferences(duplicate) error = %v", err)
	}
}

func TestDigestPreferencesEntriesAreIndependent(t *testing.T) {
	t.Parallel()

	preferences, err := NewDigestPreferences([]DigestPreference{{Algorithm: SHA256, Weight: 7}})
	if err != nil {
		t.Fatalf("NewDigestPreferences() error = %v", err)
	}
	entries := preferences.Entries()
	entries[0].Weight = 0
	if got := preferences.Entries()[0].Weight; got != 7 {
		t.Fatalf("stored weight = %d", got)
	}
}
