package ksuid

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
)

func TestRemainingKSUIDBoundaries(t *testing.T) {
	var zero ID
	if zero.String() != "" || zero.Inspect().HasTime {
		t.Fatal("zero KSUID state")
	}
	if _, err := zero.MarshalText(); err == nil {
		t.Fatal("zero text must fail")
	}
	if _, err := zero.MarshalBinary(); err == nil {
		t.Fatal("zero binary must fail")
	}
	if data, err := json.Marshal(zero); err != nil || string(data) != "null" {
		t.Fatalf("zero JSON = %s, %v", data, err)
	}
	assigned, _ := Parse("0ujtsYcgvSTl8PAuAdqWYSMnLOv")
	if err := json.Unmarshal([]byte("null"), &assigned); err != nil || !assigned.IsZero() {
		t.Fatalf("JSON null = %s, %v", assigned, err)
	}
	if _, err := NewGenerator(identifier.ClockFunc(func() time.Time {
		return time.Unix(epoch-1, 0)
	}), nil).New(); !errors.Is(err, identifier.ErrInvalid) {
		t.Fatalf("pre-epoch error = %v", err)
	}
	if _, err := NewGenerator(nil, nil).New(); err != nil {
		t.Fatal(err)
	}
}
