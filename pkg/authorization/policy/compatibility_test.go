package policy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/faustbrian/golib/pkg/authorization/abac"
	"github.com/faustbrian/golib/pkg/authorization/acl"
	"github.com/faustbrian/golib/pkg/authorization/policy"
	"github.com/faustbrian/golib/pkg/authorization/rbac"
)

func TestVersionOneCompatibilityCorpus(t *testing.T) {
	t.Parallel()

	compiler, err := policy.NewCompiler(map[policy.Model]policy.Decoder{
		policy.ModelACL: acl.Decoder{}, policy.ModelRBAC: rbac.Decoder{},
		policy.ModelABAC: abac.Decoder{},
	})
	if err != nil {
		t.Fatalf("policy.NewCompiler() error = %v", err)
	}
	entries, err := os.ReadDir("testdata/v1")
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("compatibility corpus entries = %d, want 3", len(entries))
	}
	for _, entry := range entries {
		entry := entry
		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()
			encoded, err := os.ReadFile(filepath.Join("testdata/v1", entry.Name()))
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			manifest, err := policy.Decode(encoded)
			if err != nil {
				t.Fatalf("policy.Decode() error = %v", err)
			}
			snapshot, err := compiler.Compile(manifest)
			if err != nil {
				t.Fatalf("Compiler.Compile() error = %v", err)
			}
			if snapshot.Revision() != manifest.Revision || len(snapshot.Policies()) != 1 {
				t.Fatalf("compiled snapshot = revision %d, policies %d", snapshot.Revision(), len(snapshot.Policies()))
			}
			reencoded, err := policy.Encode(manifest)
			if err != nil {
				t.Fatalf("policy.Encode() error = %v", err)
			}
			if _, err := policy.Decode(reencoded); err != nil {
				t.Fatalf("round-trip policy.Decode() error = %v", err)
			}
		})
	}
}
