package main

import (
	"strings"
	"testing"
)

func TestPrepareStandaloneChangelogCreatesBracketedRelease(t *testing.T) {
	t.Parallel()

	input := []byte("# Changelog\n\n## [Unreleased]\n\n### Added\n\n- API.\n\n[Unreleased]: https://example.test/main\n")
	got, err := prepareStandaloneChangelog(
		input,
		"go-router",
		"adapters/otel/v1.0.0",
		"2026-08-25",
	)
	if err != nil {
		t.Fatalf("prepareStandaloneChangelog() error = %v", err)
	}
	want := "## [Unreleased]\n\n## [1.0.0] - 2026-08-25\n\n### Added"
	if !strings.Contains(string(got), want) {
		t.Fatalf("release heading was not inserted:\n%s", got)
	}
	for _, link := range []string{
		"[Unreleased]: https://github.com/faustbrian/go-router/compare/adapters%2Fotel%2Fv1.0.0...HEAD",
		"[1.0.0]: https://github.com/faustbrian/go-router/releases/tag/adapters%2Fotel%2Fv1.0.0",
	} {
		if !strings.Contains(string(got), link) {
			t.Fatalf("release link %q is missing:\n%s", link, got)
		}
	}
	second, err := prepareStandaloneChangelog(
		got,
		"go-router",
		"adapters/otel/v1.0.0",
		"2026-08-25",
	)
	if err != nil {
		t.Fatalf("second prepareStandaloneChangelog() error = %v", err)
	}
	if string(second) != string(got) {
		t.Fatalf("release preparation is not idempotent:\n%s", second)
	}
}

func TestPrepareStandaloneChangelogCreatesUnbracketedRelease(t *testing.T) {
	t.Parallel()

	input := []byte("# Changelog\n\n## Unreleased\n\n### Fixed\n\n- Defect.\n")
	got, err := prepareStandaloneChangelog(
		input,
		"go-router",
		"v1.0.0",
		"2026-08-25",
	)
	if err != nil {
		t.Fatalf("prepareStandaloneChangelog() error = %v", err)
	}
	want := "## Unreleased\n\n## 1.0.0 - 2026-08-25\n\n### Fixed"
	if !strings.Contains(string(got), want) {
		t.Fatalf("unbracketed release heading was not inserted:\n%s", got)
	}
	if strings.Contains(string(got), "[1.0.0]:") {
		t.Fatalf("unbracketed changelog received reference links:\n%s", got)
	}
}

func TestPrepareStandaloneChangelogRejectsReleaseDateChange(t *testing.T) {
	t.Parallel()

	input := []byte("# Changelog\n\n## Unreleased\n\n## 1.0.0 - 2026-08-24\n")
	_, err := prepareStandaloneChangelog(
		input,
		"go-router",
		"v1.0.0",
		"2026-08-25",
	)
	if err == nil {
		t.Fatal("prepareStandaloneChangelog() error = nil")
	}
}
