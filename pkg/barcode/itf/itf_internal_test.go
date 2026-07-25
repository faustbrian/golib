package itf

import "testing"

func TestEncodeBarsPropagatesWriterErrors(t *testing.T) {
	if _, err := encodeBars("A", defaultQuietZone, defaultHeight); err == nil {
		t.Fatal("encodeBars(invalid payload) succeeded")
	}
}
