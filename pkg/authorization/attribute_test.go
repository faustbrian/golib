package authorization

import (
	"math"
	"net/netip"
	"testing"
	"time"
)

func TestTypedAttributeValues(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	ip := netip.MustParseAddr("192.0.2.10")

	tests := map[string]struct {
		value Value
		kind  ValueKind
	}{
		"null":       {value: NullValue(), kind: ValueNull},
		"string":     {value: StringValue("owner"), kind: ValueString},
		"bool":       {value: BoolValue(true), kind: ValueBool},
		"integer":    {value: IntValue(42), kind: ValueInt},
		"float":      {value: MustFloatValue(3.5), kind: ValueFloat},
		"time":       {value: TimeValue(now), kind: ValueTime},
		"ip":         {value: IPValue(ip), kind: ValueIP},
		"string set": {value: StringSetValue([]string{"reader", "editor", "reader"}), kind: ValueStringSet},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if tt.value.Kind() != tt.kind {
				t.Errorf("Value.Kind() = %v, want %v", tt.value.Kind(), tt.kind)
			}
		})
	}

	if got, ok := tests["string"].value.String(); !ok || got != "owner" {
		t.Errorf("Value.String() = %q, %v", got, ok)
	}
	if got, ok := tests["bool"].value.Bool(); !ok || !got {
		t.Errorf("Value.Bool() = %v, %v", got, ok)
	}
	if got, ok := tests["integer"].value.Int(); !ok || got != 42 {
		t.Errorf("Value.Int() = %d, %v", got, ok)
	}
	if got, ok := tests["float"].value.Float(); !ok || got != 3.5 {
		t.Errorf("Value.Float() = %f, %v", got, ok)
	}
	if got, ok := tests["time"].value.Time(); !ok || !got.Equal(now) {
		t.Errorf("Value.Time() = %v, %v", got, ok)
	}
	if got, ok := tests["ip"].value.IP(); !ok || got != ip {
		t.Errorf("Value.IP() = %v, %v", got, ok)
	}

	set, ok := tests["string set"].value.StringSet()
	if !ok || len(set) != 2 || set[0] != "editor" || set[1] != "reader" {
		t.Errorf("Value.StringSet() = %v, %v", set, ok)
	}
	set[0] = "mutated"
	setAgain, _ := tests["string set"].value.StringSet()
	if setAgain[0] != "editor" {
		t.Error("Value.StringSet() exposed mutable storage")
	}

	if _, err := FloatValue(math.NaN()); err == nil {
		t.Error("FloatValue(NaN) error = nil")
	}
	if _, err := FloatValue(math.Inf(1)); err == nil {
		t.Error("FloatValue(+Inf) error = nil")
	}
	if _, ok := StringValue("not-a-set").StringSet(); ok {
		t.Error("StringValue().StringSet() ok = true")
	}

	defer func() {
		if recover() == nil {
			t.Error("MustFloatValue(NaN) did not panic")
		}
	}()
	MustFloatValue(math.NaN())
}

func TestValueEqualityAndOrdering(t *testing.T) {
	t.Parallel()

	now := time.Unix(1, 0)
	equalValues := []Value{
		{},
		NullValue(),
		StringValue("value"),
		BoolValue(true),
		IntValue(1),
		MustFloatValue(1),
		TimeValue(now),
		IPValue(netip.MustParseAddr("192.0.2.1")),
		StringSetValue([]string{"one", "two"}),
	}
	for _, value := range equalValues {
		if !value.Equal(value) {
			t.Errorf("Value(%v).Equal(self) = false", value.Kind())
		}
	}
	if StringValue("left").Equal(StringValue("right")) {
		t.Error("different strings are equal")
	}
	if StringValue("value").Equal(IntValue(1)) {
		t.Error("different value kinds are equal")
	}
	invalid := Value{kind: ValueKind(255)}
	if invalid.Equal(invalid) {
		t.Error("invalid values are equal")
	}

	tests := []struct {
		left  Value
		right Value
		want  int
	}{
		{StringValue("a"), StringValue("b"), -1},
		{IntValue(1), IntValue(2), -1},
		{IntValue(2), IntValue(1), 1},
		{MustFloatValue(1), MustFloatValue(1), 0},
		{TimeValue(time.Unix(1, 0)), TimeValue(time.Unix(2, 0)), -1},
	}
	for _, tt := range tests {
		got, ok := tt.left.Compare(tt.right)
		if !ok || got != tt.want {
			t.Errorf("Value.Compare() = %d, %v; want %d, true", got, ok, tt.want)
		}
	}
	if _, ok := BoolValue(true).Compare(BoolValue(false)); ok {
		t.Error("Bool Value.Compare() ok = true")
	}
	if _, ok := IntValue(1).Compare(MustFloatValue(1)); ok {
		t.Error("mixed Value.Compare() ok = true")
	}
}

func TestValueCollectionLength(t *testing.T) {
	t.Parallel()

	length, ok := StringSetValue([]string{"one", "two"}).CollectionLength()
	if !ok || length != 2 {
		t.Errorf("Value.CollectionLength() = %d, %v; want 2, true", length, ok)
	}
	if _, ok := StringValue("value").CollectionLength(); ok {
		t.Error("string Value.CollectionLength() ok = true")
	}
}
