package server

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadStaticAccessParsesBoundedStrictDocument(t *testing.T) {
	t.Parallel()

	document := `{
		"keys":[{"id":"key-1","key":"secret-1","subject":"operator-1"}],
		"acl":[{
			"id":"view-fleet",
			"subject":"operator-1",
			"tenant":"tenant-1",
			"action":"view",
			"resource_type":"workload",
			"resource_id":"fleet",
			"effect":"allow"
		}]
	}`
	access, err := LoadStaticAccess(strings.NewReader(document), 4*1024)
	if err != nil {
		t.Fatalf("LoadStaticAccess() error = %v", err)
	}
	if access == nil || access.Extractor == nil || access.Authenticator == nil || access.Authorizer == nil {
		t.Fatalf("LoadStaticAccess() = %#v", access)
	}
}

func TestLoadStaticAccessRejectsUnsafeDocumentsWithoutLeaking(t *testing.T) {
	t.Parallel()

	validKey := `"keys":[{"id":"key-1","key":"do-not-leak","subject":"operator-1"}]`
	var typedNil *strings.Reader
	tests := []struct {
		reader io.Reader
		limit  int64
	}{
		{},
		{reader: typedNil, limit: 100},
		{reader: strings.NewReader(`{}`)},
		{reader: strings.NewReader(`{"secret":"do-not-leak"}`), limit: 8},
		{reader: failingReader{}, limit: 100},
		{reader: strings.NewReader(`{`), limit: 100},
		{reader: strings.NewReader(`{"unknown":"do-not-leak"}`), limit: 100},
		{reader: strings.NewReader(`{}` + `{}`), limit: 100},
		{reader: strings.NewReader(`{` + validKey + `,"acl":[{"effect":"maybe"}]}`), limit: 1_000},
		{reader: strings.NewReader(`{"keys":[],"acl":[]}`), limit: 100},
	}
	for _, test := range tests {
		access, err := LoadStaticAccess(test.reader, test.limit)
		if access != nil || !errors.Is(err, ErrInvalidAccessDocument) {
			t.Fatalf("LoadStaticAccess() = (%v, %v)", access, err)
		}
		if strings.Contains(err.Error(), "do-not-leak") {
			t.Fatalf("LoadStaticAccess() leaked source: %v", err)
		}
	}
}

func TestLoadStaticAccessAcceptsExplicitDeny(t *testing.T) {
	t.Parallel()

	document := `{
		"keys":[{"id":"key-1","key":"secret-1","subject":"operator-1"}],
		"acl":[{
			"id":"deny-purge","subject":"operator-1","tenant":"tenant-1",
			"action":"purge","resource_type":"queue","effect":"deny"
		}]
	}`
	if access, err := LoadStaticAccess(strings.NewReader(document), 1_000); err != nil || access == nil {
		t.Fatalf("LoadStaticAccess(deny) = (%v, %v)", access, err)
	}
}

func TestLoadStaticAccessFileReadsBoundedDocument(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "access.json")
	document := `{"keys":[{"id":"key-1","key":"secret-1","subject":"operator-1"}],"acl":[]}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	access, err := LoadStaticAccessFile(path, 1024)
	if err != nil || access == nil {
		t.Fatalf("LoadStaticAccessFile() = (%v, %v), want access and nil", access, err)
	}
}

func TestLoadStaticAccessFileHidesFilesystemDetails(t *testing.T) {
	t.Parallel()

	secretPath := filepath.Join(t.TempDir(), "customer-secret-access.json")
	for _, path := range []string{"", "   ", secretPath} {
		access, err := LoadStaticAccessFile(path, 1024)
		if access != nil || !errors.Is(err, ErrInvalidAccessDocument) {
			t.Fatalf("LoadStaticAccessFile(%q) = (%v, %v), want nil and stable error", path, access, err)
		}
		if err != nil && strings.Contains(err.Error(), secretPath) {
			t.Fatalf("error %q disclosed configured path", err)
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}
