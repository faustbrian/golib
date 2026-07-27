package eventsourcing_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestGoldenCompatibilityCorpusChecksums(t *testing.T) {
	t.Parallel()

	manifest, err := os.ReadFile("testdata/checksums.txt")
	if err != nil {
		t.Fatalf("read golden checksum manifest: %v", err)
	}
	seen := make(map[string]struct{}, 4)
	for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("malformed golden checksum entry")
		}
		if _, duplicate := seen[fields[1]]; duplicate {
			t.Fatalf("duplicate golden checksum path %q", fields[1])
		}
		seen[fields[1]] = struct{}{}
		payload, err := os.ReadFile(fields[1])
		if err != nil {
			t.Fatalf("read golden fixture %q: %v", fields[1], err)
		}
		sum := sha256.Sum256(payload)
		if hex.EncodeToString(sum[:]) != fields[0] {
			t.Fatalf("golden fixture checksum differs for %q", fields[1])
		}
	}
	if len(seen) != 4 {
		t.Fatalf("golden checksum entries = %d, want 4", len(seen))
	}
}
