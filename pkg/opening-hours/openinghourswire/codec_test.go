package openinghourswire_test

import (
	"testing"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
	"github.com/faustbrian/golib/pkg/opening-hours/openinghourswire"
	wire "github.com/faustbrian/golib/pkg/wire"
)

func TestCodec(t *testing.T) {
	if openinghourswire.WireFormat != wire.Format(openinghourswire.Format) {
		t.Fatalf("WireFormat = %q", openinghourswire.WireFormat)
	}
	codec := openinghourswire.Codec{}
	want, _ := openinghours.NewSchedule(openinghours.Config{Timezone: "UTC"})
	data, err := codec.Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.Decode(data)
	if err != nil || !want.Equal(got) {
		t.Fatalf("round trip error=%v equal=%t", err, want.Equal(got))
	}
	if _, err := codec.Decode([]byte(`{}`)); err == nil {
		t.Fatal("Decode accepted invalid bytes")
	}
}
