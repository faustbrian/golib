package leafvector

import "testing"

func TestEncodePresentValue(t *testing.T) {
	var value [32]byte
	for index := range value {
		value[index] = byte(index)
	}

	got := EncodePresent(0, value)

	var wantLow Scalar
	copy(wantLow[:16], value[:16])
	wantLow[16] = 1
	var wantHigh Scalar
	copy(wantHigh[:16], value[16:])

	if got.Low != wantLow {
		t.Fatalf("low scalar = %x, want %x", got.Low, wantLow)
	}
	if got.High != wantHigh {
		t.Fatalf("high scalar = %x, want %x", got.High, wantHigh)
	}
}

func TestPresentZeroDiffersFromAbsent(t *testing.T) {
	present := EncodePresent(42, [32]byte{})
	absent := EncodeAbsent(42)

	var wantPresentLow Scalar
	wantPresentLow[16] = 1
	if present.Low != wantPresentLow {
		t.Fatalf("present zero low scalar = %x, want %x", present.Low, wantPresentLow)
	}
	if present.High != (Scalar{}) {
		t.Fatalf("present zero high scalar = %x, want zero", present.High)
	}
	if absent.Low != (Scalar{}) || absent.High != (Scalar{}) {
		t.Fatalf("absent scalars = %x/%x, want zero/zero", absent.Low, absent.High)
	}
	if present == absent {
		t.Fatal("present zero encoding equals absent encoding")
	}
}

func TestSuffixPlacement(t *testing.T) {
	tests := []struct {
		name     string
		suffix   byte
		wantHalf Half
		wantLow  byte
		wantHigh byte
	}{
		{name: "first suffix in C1", suffix: 0, wantHalf: C1, wantLow: 0, wantHigh: 1},
		{name: "last suffix in C1", suffix: 127, wantHalf: C1, wantLow: 254, wantHigh: 255},
		{name: "first suffix in C2", suffix: 128, wantHalf: C2, wantLow: 0, wantHigh: 1},
		{name: "last suffix in C2", suffix: 255, wantHalf: C2, wantLow: 254, wantHigh: 255},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := EncodeAbsent(test.suffix)
			if got.Half != test.wantHalf {
				t.Fatalf("half = %d, want %d", got.Half, test.wantHalf)
			}
			if got.LowIndex != test.wantLow || got.HighIndex != test.wantHigh {
				t.Fatalf(
					"indices = %d/%d, want %d/%d",
					got.LowIndex,
					got.HighIndex,
					test.wantLow,
					test.wantHigh,
				)
			}
		})
	}
}

func TestEverySuffixHasContiguousInRangeIndices(t *testing.T) {
	for suffix := 0; suffix < 256; suffix++ {
		got := EncodeAbsent(byte(suffix))
		if got.HighIndex != got.LowIndex+1 {
			t.Fatalf(
				"suffix %d indices = %d/%d, want contiguous",
				suffix,
				got.LowIndex,
				got.HighIndex,
			)
		}
		wantHalf := C1
		if suffix >= 128 {
			wantHalf = C2
		}
		if got.Half != wantHalf {
			t.Fatalf("suffix %d half = %d, want %d", suffix, got.Half, wantHalf)
		}
	}
}

func TestEncodeStem(t *testing.T) {
	var stem [31]byte
	for index := range stem {
		stem[index] = byte(index + 1)
	}

	got := EncodeStem(stem)
	var want Scalar
	copy(want[:31], stem[:])
	if got != want {
		t.Fatalf("stem scalar = %x, want %x", got, want)
	}
}

func TestStemCommitmentInputPositions(t *testing.T) {
	if ExtensionMarkerIndex != 0 {
		t.Fatalf("extension marker index = %d, want 0", ExtensionMarkerIndex)
	}
	if StemIndex != 1 {
		t.Fatalf("stem index = %d, want 1", StemIndex)
	}
	if C1HashIndex != 2 {
		t.Fatalf("C1 hash index = %d, want 2", C1HashIndex)
	}
	if C2HashIndex != 3 {
		t.Fatalf("C2 hash index = %d, want 3", C2HashIndex)
	}

	var wantMarker Scalar
	wantMarker[0] = 1
	if got := EncodeExtensionMarker(); got != wantMarker {
		t.Fatalf("extension marker = %x, want %x", got, wantMarker)
	}
}
