package passwordtest

import (
	"bytes"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
)

func TestEntropyAndService(t *testing.T) {
	if _, err := NewEntropy(nil); err == nil {
		t.Fatal("NewEntropy accepted an empty seed")
	}
	entropy, err := NewEntropy([]byte{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	destination := make([]byte, 5)
	if n, err := entropy.Read(destination); err != nil || n != len(destination) || !bytes.Equal(destination, []byte{1, 2, 1, 2, 1}) {
		t.Fatalf("Read = %v, %v, %v", destination, n, err)
	}
	policy := password.DefaultPolicy()
	if _, err := NewService(policy, nil); err == nil {
		t.Fatal("NewService accepted an empty seed")
	}
	service, err := NewService(policy, []byte{1})
	if err != nil || service == nil {
		t.Fatalf("NewService: %v", err)
	}
}
