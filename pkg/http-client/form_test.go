package httpclient

import (
	"io"
	"net/url"
	"testing"
)

func TestFormBodySnapshotsCanonicalRepeatedValuesAndReplays(t *testing.T) {
	t.Parallel()

	values := url.Values{
		"z":        {"last"},
		"repeated": {"first", "", "third"},
		"space":    {"two words"},
		"symbols":  {"a&b=c"},
		"omitted":  nil,
	}
	body, err := NewFormBody(values)
	if err != nil {
		t.Fatalf("construct form body: %v", err)
	}
	values.Set("repeated", "mutated")
	values.Set("added", "later")

	want := "repeated=first&repeated=&repeated=third&" +
		"space=two+words&symbols=a%26b%3Dc&z=last"
	if !body.Replayable() || body.ContentType() != "application/x-www-form-urlencoded" ||
		body.ContentLength() != int64(len(want)) {
		t.Fatalf("form metadata = %t, %q, %d", body.Replayable(), body.ContentType(), body.ContentLength())
	}
	for attempt := 0; attempt < 2; attempt++ {
		opened, err := body.Open()
		if err != nil {
			t.Fatalf("open form attempt %d: %v", attempt, err)
		}
		content, readErr := io.ReadAll(opened)
		closeErr := opened.Close()
		if readErr != nil || closeErr != nil || string(content) != want {
			t.Fatalf("form attempt %d = %q, %v, %v", attempt, content, readErr, closeErr)
		}
	}
}

func TestFormBodyRepresentsEmptyFormExplicitly(t *testing.T) {
	t.Parallel()

	body, err := NewFormBody(nil)
	if err != nil {
		t.Fatalf("construct empty form: %v", err)
	}
	if body.ContentLength() != 0 || body.ContentType() != "application/x-www-form-urlencoded" {
		t.Fatalf("empty form metadata = %d, %q", body.ContentLength(), body.ContentType())
	}
	opened, err := body.Open()
	if err != nil {
		t.Fatalf("open empty form: %v", err)
	}
	content, _ := io.ReadAll(opened)
	_ = opened.Close()
	if len(content) != 0 {
		t.Fatalf("empty form content = %q", content)
	}
}
