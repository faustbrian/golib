package prompts

import "testing"

func TestDecimalPreservesFractionalLeadingZeroAndDigitNine(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{"0.1": "0.1", "9": "9"} {
		decimal, err := parseDecimal(input)
		if err != nil || decimal.String() != want {
			t.Fatalf("parseDecimal(%q) = %q, %v", input, decimal.String(), err)
		}
	}
}
