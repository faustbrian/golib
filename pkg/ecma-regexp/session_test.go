package ecmascript_test

import (
	"context"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestSessionImplementsGlobalLastIndex(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("a", "g", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	session := ecmascript.NewSession(program)

	first, matched, err := session.Exec(context.Background(), "baac", ecmascript.DefaultMatchOptions().Limits)
	if err != nil || !matched || first.Full().Span().Start.UTF16 != 1 || session.LastIndex() != 2 {
		t.Fatalf("first Exec() = %#v, %t, %v; lastIndex=%d", first, matched, err, session.LastIndex())
	}
	second, matched, err := session.Exec(context.Background(), "baac", ecmascript.DefaultMatchOptions().Limits)
	if err != nil || !matched || second.Full().Span().Start.UTF16 != 2 || session.LastIndex() != 3 {
		t.Fatalf("second Exec() = %#v, %t, %v; lastIndex=%d", second, matched, err, session.LastIndex())
	}
	_, matched, err = session.Exec(context.Background(), "baac", ecmascript.DefaultMatchOptions().Limits)
	if err != nil || matched || session.LastIndex() != 0 {
		t.Fatalf("failed Exec() = _, %t, %v; lastIndex=%d", matched, err, session.LastIndex())
	}
}

func TestSessionImplementsStickyLastIndex(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("a", "y", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	session := ecmascript.NewSession(program)
	_, matched, err := session.Exec(context.Background(), "ba", ecmascript.DefaultMatchOptions().Limits)
	if err != nil || matched || session.LastIndex() != 0 {
		t.Fatalf("Exec(at zero) = _, %t, %v; lastIndex=%d", matched, err, session.LastIndex())
	}
	session.SetLastIndex(1)
	_, matched, err = session.Exec(context.Background(), "ba", ecmascript.DefaultMatchOptions().Limits)
	if err != nil || !matched || session.LastIndex() != 2 {
		t.Fatalf("Exec(at one) = _, %t, %v; lastIndex=%d", matched, err, session.LastIndex())
	}
}

func TestSessionIgnoresLastIndexWithoutGlobalOrSticky(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("a", "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	session := ecmascript.NewSession(program)
	session.SetLastIndex(9)
	result, matched, err := session.Exec(context.Background(), "ba", ecmascript.DefaultMatchOptions().Limits)
	if err != nil || !matched || result.Full().Span().Start.UTF16 != 1 || session.LastIndex() != 9 {
		t.Fatalf("Exec() = %#v, %t, %v; lastIndex=%d", result, matched, err, session.LastIndex())
	}
}
