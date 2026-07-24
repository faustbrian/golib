package media

import "testing"

func TestScaledEncodingMaximumSaturatesAtTheIntegerLimit(t *testing.T) {
	t.Parallel()

	maximum := int(^uint(0) >> 1)
	threshold := maximum / 3
	if got := scaledEncodingMaximum(threshold); got != threshold*3 {
		t.Fatalf("scaled threshold = %d, want %d", got, threshold*3)
	}
	if got := scaledEncodingMaximum(threshold + 1); got != maximum {
		t.Fatalf("scaled overflow = %d, want %d", got, maximum)
	}
}

func TestMultipartNamePercentEncodingEndpoints(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		character byte
		want      bool
	}{
		{character: 0x1f, want: true},
		{character: 0x20, want: false},
		{character: 0x7f, want: true},
		{character: 0x80, want: true},
	} {
		if got := multipartNameNeedsPercentEncoding(test.character); got != test.want {
			t.Fatalf("percent encoding for %#x = %t, want %t", test.character, got, test.want)
		}
	}
}
