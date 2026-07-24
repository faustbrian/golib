package upc

import (
	"fmt"
	"testing"
)

func TestSupplementParityAndRunConversion(t *testing.T) {
	if _, err := encodeSupplement("1"); err == nil {
		t.Fatal("encodeSupplement(short) succeeded")
	}
	wantPatterns := [2][10]string{
		{
			"0001101", "0011001", "0010011", "0111101", "0100011",
			"0110001", "0101111", "0111011", "0110111", "0001011",
		},
		{
			"0100111", "0110011", "0011011", "0100001", "0011101",
			"0111001", "0000101", "0010001", "0001001", "0010111",
		},
	}
	pattern := func(parity, digit byte) string {
		if parity == 'G' {
			return wantPatterns[1][digit-'0']
		}
		return wantPatterns[0][digit-'0']
	}
	twoDigitParity := [4]string{"LL", "LG", "GL", "GG"}
	for value := 0; value < 100; value++ {
		payload := fmt.Sprintf("%02d", value)
		encoded, err := encodeSupplement(payload)
		parity := twoDigitParity[value%4]
		want := "1011" + pattern(parity[0], payload[0]) +
			"01" + pattern(parity[1], payload[1])
		if err != nil || encoded != want {
			t.Fatalf("encodeSupplement(%s) = (%q, %v), want %q",
				payload, encoded, err, want)
		}
	}
	fiveDigitParity := [10]string{
		"GGLLL", "GLGLL", "GLLGL", "GLLLG", "LGGLL",
		"LLGGL", "LLLGG", "LGLGL", "LGLLG", "LLGLG",
	}
	for _, test := range []struct {
		payload  string
		checksum int
	}{
		{payload: "00000", checksum: 0},
		{payload: "10000", checksum: 3},
		{payload: "01000", checksum: 9},
		{payload: "00100", checksum: 3},
		{payload: "00010", checksum: 9},
		{payload: "00001", checksum: 3},
	} {
		encoded, err := encodeSupplement(test.payload)
		parity := fiveDigitParity[test.checksum]
		want := "1011"
		for index := range test.payload {
			if index > 0 {
				want += "01"
			}
			want += pattern(parity[index], test.payload[index])
		}
		if err != nil || encoded != want {
			t.Fatalf("encodeSupplement(%s) = (%q, %v), want %q",
				test.payload, encoded, err, want)
		}
	}
	converted := moduleRuns("00111001")
	if len(converted) != 4 || converted[0].Dark || converted[0].Width != 2 ||
		!converted[1].Dark || converted[1].Width != 3 {
		t.Fatalf("moduleRuns() = %+v", converted)
	}
}
