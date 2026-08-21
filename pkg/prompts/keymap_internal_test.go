package prompts

import (
	"errors"
	"testing"
)

func TestDefaultKeyMapPreservesEveryDeclaredMeaning(t *testing.T) {
	t.Parallel()

	keyMap, err := NewKeyMap()
	if err != nil {
		t.Fatal(err)
	}
	for key := KeyEnter; key < Key(keyCount); key++ {
		want := key
		if key == KeyCtrlC {
			want = KeyEscape
		}
		if got := keyMap.translate(KeyEvent(key)).Key; got != want {
			t.Fatalf("translate(%d) = %d, want %d", key, got, want)
		}
	}
}

func TestKeyMapRebindingRemovesEveryPreviousMeaning(t *testing.T) {
	t.Parallel()

	keyMap, err := NewKeyMap(
		KeyBinding{Input: KeyTab, Meaning: KeyEscape},
		KeyBinding{Input: KeyEnd, Meaning: KeyEnter},
		KeyBinding{Input: KeyHome, Meaning: KeyNewline},
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[Key]Key{
		KeyTab: KeyEscape,
		KeyEscape: KeyIgnored,
		KeyCtrlC: KeyIgnored,
		KeyEnd: KeyEnter,
		KeyEnter: KeyIgnored,
		KeyHome: KeyNewline,
		KeyNewline: KeyIgnored,
		KeyDown: KeyDown,
	}
	for input, want := range tests {
		if got := keyMap.translate(KeyEvent(input)).Key; got != want {
			t.Fatalf("translate(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestKeyMapRejectsAndIgnoresTheExactKeyCountBoundary(t *testing.T) {
	t.Parallel()

	keyMap, err := NewKeyMap()
	if err != nil {
		t.Fatal(err)
	}
	boundary := Key(keyCount)
	if got := keyMap.translate(KeyEvent(boundary)).Key; got != boundary {
		t.Fatalf("translate(keyCount) = %d, want %d", got, boundary)
	}
	if _, err := NewKeyMap(KeyBinding{Input: boundary, Meaning: KeyEnter});
		!errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("NewKeyMap(keyCount) error = %v", err)
	}
}
