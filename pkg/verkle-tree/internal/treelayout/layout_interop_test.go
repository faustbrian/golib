package treelayout

import (
	"context"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestLayoutMatchesPinnedRustPathHints(t *testing.T) {
	t.Parallel()

	encoded, err := os.ReadFile("testdata/rust-verkle-topology.tsv")
	if err != nil {
		t.Fatalf("read Rust topology fixture: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(encoded)), "\n")
	if len(lines) != 10 {
		t.Fatalf("fixture rows = %d, want 10", len(lines))
	}
	const header = "case\tinserted_stems\tquery_stem\tdepth\tstatus\texisting_stem"
	if lines[0] != header {
		t.Fatalf("fixture header = %q, want %q", lines[0], header)
	}

	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			t.Fatalf("fixture row has %d fields: %q", len(fields), line)
		}
		t.Run(fields[0], func(t *testing.T) {
			t.Parallel()

			stems := parseFixtureStems(t, fields[1])
			query := parseFixtureStem(t, fields[2])
			depth, err := strconv.ParseUint(fields[3], 10, 8)
			if err != nil {
				t.Fatalf("parse depth %q: %v", fields[3], err)
			}
			want := Result{
				Match: fixtureMatch(t, fields[4]),
				Depth: uint8(depth),
			}
			if fields[5] != "-" {
				want.Existing = parseFixtureStem(t, fields[5])
			}

			layout, err := Build(context.Background(), stems, testLimits())
			if err != nil {
				t.Fatalf("build fixture layout: %v", err)
			}
			got, err := layout.Lookup(context.Background(), query)
			if err != nil {
				t.Fatalf("lookup fixture stem: %v", err)
			}
			if got != want {
				t.Fatalf("lookup = %#v, want Rust hint %#v", got, want)
			}
		})
	}
}

func parseFixtureStems(t *testing.T, encoded string) []Stem {
	t.Helper()

	if encoded == "-" {
		return nil
	}
	parts := strings.Split(encoded, ",")
	stems := make([]Stem, len(parts))
	for index, part := range parts {
		stems[index] = parseFixtureStem(t, part)
	}

	return stems
}

func parseFixtureStem(t *testing.T, encoded string) Stem {
	t.Helper()

	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode stem %q: %v", encoded, err)
	}
	if len(decoded) != len(Stem{}) {
		t.Fatalf("decoded stem length = %d, want %d", len(decoded), len(Stem{}))
	}
	var stem Stem
	copy(stem[:], decoded)

	return stem
}

func fixtureMatch(t *testing.T, status string) Match {
	t.Helper()

	switch status {
	case "Present":
		return MatchPresentStem
	case "None":
		return MatchMissingChild
	case "DifferentStem":
		return MatchDifferentStem
	default:
		t.Fatalf("unknown Rust path status %q", status)
		return 0
	}
}
