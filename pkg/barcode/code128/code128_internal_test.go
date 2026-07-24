package code128

import "testing"

func TestCodeSetNames(t *testing.T) {
	tests := map[CodeSet]string{
		CodeSetAuto: "", CodeSetA: "A", CodeSetB: "B", CodeSetC: "C", CodeSet(99): "",
	}
	for codeSet, want := range tests {
		if got := codeSetName(codeSet); got != want {
			t.Fatalf("codeSetName(%v) = %q, want %q", codeSet, got, want)
		}
	}
}
