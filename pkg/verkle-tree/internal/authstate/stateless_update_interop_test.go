package authstate

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

const rustTransitionTraceCount = 2_048

func TestStatelessUpdaterMatchesPinnedRustRebuiltTransitions(t *testing.T) {
	contents, err := os.ReadFile("testdata/rust-verkle-transitions.tsv")
	if err != nil {
		t.Fatalf("read Rust transition roots: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	if len(lines) != rustTransitionTraceCount+1 {
		t.Fatalf("fixture rows = %d, want %d", len(lines), rustTransitionTraceCount+1)
	}
	const header = "case\tpre_entries\tupdates\tpre_root_commitment_be\tpost_root_commitment_be"
	if lines[0] != header {
		t.Fatalf("fixture header = %q, want %q", lines[0], header)
	}

	openingLimits := testAuthstateAggregateOpeningLimits()
	openingLimits.MaxQueries = 4_096
	openingLimits.MaxScalarDecodes = 4_096 * backend.VectorWidth
	openingLimits.MaxMSMTerms = 8_192 * backend.VectorWidth
	proofEngine, err := NewProofEngine(context.Background(), openingLimits)
	if err != nil {
		t.Fatalf("new transition proof engine: %v", err)
	}
	updater, err := NewStatelessUpdaterFromProofEngine(
		context.Background(), proofEngine, testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("new transition stateless updater: %v", err)
	}

	for row, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("fixture row %d fields = %d, want 5", row+2, len(fields))
		}
		t.Run(fields[0], func(t *testing.T) {
			entries := decodeRustTransitionEntries(t, row+2, fields[1])
			updates := decodeRustTransitionUpdates(t, row+2, fields[2])
			snapshot := newTestSnapshot(t, entries)
			assertRustTransitionRoot(t, snapshot, row+2, "pre-root", fields[3])

			proof, err := proofEngine.ProveUpdates(
				context.Background(), snapshot, updates,
				topologyProofGenerationLimits(),
			)
			if err != nil {
				t.Fatalf("generate transition proof: %v", err)
			}
			statelessRoot, err := updater.Apply(
				context.Background(), proof, updates,
				topologyProofVerificationLimits(),
				topologyStatelessUpdateLimits(),
			)
			if err != nil {
				t.Fatalf("apply stateless transition: %v", err)
			}
			assertRustBackendRoot(t, statelessRoot, row+2, "post-root", fields[4])

			next, _, err := snapshot.Apply(context.Background(), updates)
			if err != nil {
				t.Fatalf("apply stateful transition: %v", err)
			}
			assertRustTransitionRoot(t, next, row+2, "post-root", fields[4])
		})
	}
}

func decodeRustTransitionEntries(t testing.TB, row int, encoded string) []Entry {
	t.Helper()

	if encoded == "-" {
		return nil
	}
	items := strings.Split(encoded, ",")
	entries := make([]Entry, len(items))
	for index, item := range items {
		parts := strings.Split(item, ":")
		if len(parts) != 2 {
			t.Fatalf("fixture row %d entry %d is malformed", row, index)
		}
		copy(entries[index].Key[:], decodeRustTransitionHex(t, row, "entry key", parts[0]))
		copy(entries[index].Value[:], decodeRustTransitionHex(t, row, "entry value", parts[1]))
	}

	return entries
}

func decodeRustTransitionUpdates(t testing.TB, row int, encoded string) []Update {
	t.Helper()

	items := strings.Split(encoded, ",")
	updates := make([]Update, len(items))
	for index, item := range items {
		parts := strings.Split(item, ":")
		if len(parts) < 2 {
			t.Fatalf("fixture row %d update %d is malformed", row, index)
		}
		var key Key
		copy(key[:], decodeRustTransitionHex(t, row, "update key", parts[1]))
		switch parts[0] {
		case "set":
			if len(parts) != 3 {
				t.Fatalf("fixture row %d Set update %d is malformed", row, index)
			}
			var value Value
			copy(value[:], decodeRustTransitionHex(t, row, "update value", parts[2]))
			updates[index] = Set(key, value)
		case "delete":
			if len(parts) != 2 {
				t.Fatalf("fixture row %d Delete update %d is malformed", row, index)
			}
			updates[index] = Delete(key)
		default:
			t.Fatalf("fixture row %d update %d kind = %q", row, index, parts[0])
		}
	}

	return updates
}

func assertRustTransitionRoot(
	t testing.TB,
	snapshot Snapshot,
	row int,
	field string,
	wantHex string,
) {
	t.Helper()

	root, err := snapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("fixture row %d %s container: %v", row, field, err)
	}
	assertRustBackendRoot(t, root, row, field, wantHex)
}

func assertRustBackendRoot(
	t testing.TB,
	root backend.Root,
	row int,
	field string,
	wantHex string,
) {
	t.Helper()

	got, _, err := root.CommitmentBytes()
	if err != nil {
		t.Fatalf("fixture row %d %s commitment: %v", row, field, err)
	}
	want := decodeRustTransitionHex(t, row, field, wantHex)
	if !bytes.Equal(got[:], want) {
		t.Fatalf("fixture row %d %s = %x, want Rust %x", row, field, got, want)
	}
}

func decodeRustTransitionHex(t testing.TB, row int, field string, encoded string) []byte {
	t.Helper()

	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatalf("fixture row %d %s: %v", row, field, err)
	}
	if len(decoded) != 32 {
		t.Fatalf("fixture row %d %s bytes = %d, want 32", row, field, len(decoded))
	}

	return decoded
}
