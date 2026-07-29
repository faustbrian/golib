package leafvector

import (
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestRustVerkleLeafVectors(t *testing.T) {
	contents, err := os.ReadFile("testdata/rust-verkle-leaf-vectors.tsv")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 6 {
		t.Fatalf("fixture rows = %d, want 6", len(lines))
	}
	const header = "case\tsuffix\tpresent\tvalue\thalf\tlow_index\thigh_index\tlow_scalar_le\thigh_scalar_le"
	if lines[0] != header {
		t.Fatalf("fixture header = %q, want %q", lines[0], header)
	}

	for row, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 9 {
			t.Fatalf("fixture row %d fields = %d, want 9", row+2, len(fields))
		}
		t.Run(fields[0], func(t *testing.T) {
			suffix := fixtureByte(t, "suffix", fields[1])
			var got ValueOpening
			switch fields[2] {
			case "true":
				value := fixtureValue(t, fields[3])
				got = EncodePresent(suffix, value)
			case "false":
				if fields[3] != "-" {
					t.Fatalf("absent value = %q, want -", fields[3])
				}
				got = EncodeAbsent(suffix)
			default:
				t.Fatalf("present = %q, want true or false", fields[2])
			}

			wantHalf := C1
			if fields[4] == "C2" {
				wantHalf = C2
			} else if fields[4] != "C1" {
				t.Fatalf("half = %q, want C1 or C2", fields[4])
			}
			want := ValueOpening{
				Half:      wantHalf,
				LowIndex:  fixtureByte(t, "low index", fields[5]),
				HighIndex: fixtureByte(t, "high index", fields[6]),
				Low:       fixtureScalar(t, "low scalar", fields[7]),
				High:      fixtureScalar(t, "high scalar", fields[8]),
			}
			if got != want {
				t.Fatalf("opening = %+v, want %+v", got, want)
			}
		})
	}
}

func fixtureByte(t *testing.T, field, encoded string) byte {
	t.Helper()
	value, err := strconv.ParseUint(encoded, 10, 8)
	if err != nil {
		t.Fatalf("decode %s: %v", field, err)
	}

	return byte(value)
}

func fixtureValue(t *testing.T, encoded string) [32]byte {
	t.Helper()
	decoded := fixtureBytes(t, "value", encoded)

	return [32]byte(decoded)
}

func fixtureScalar(t *testing.T, field, encoded string) Scalar {
	t.Helper()
	decoded := fixtureBytes(t, field, encoded)

	return Scalar(decoded)
}

func fixtureBytes(t *testing.T, field, encoded string) [32]byte {
	t.Helper()
	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode %s: %v", field, err)
	}
	if len(decoded) != 32 {
		t.Fatalf("%s bytes = %d, want 32", field, len(decoded))
	}

	return [32]byte(decoded)
}
