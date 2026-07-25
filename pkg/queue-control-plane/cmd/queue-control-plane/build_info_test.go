package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
)

func TestParseBuildInfoAcceptsReleaseAndDevelopmentMetadata(t *testing.T) {
	t.Parallel()

	release, err := parseBuildInfo("v1.2.3", "abcdef123456", "2026-07-16T08:30:00+03:00")
	if err != nil {
		t.Fatalf("parseBuildInfo(release) error = %v", err)
	}
	if release.Version != "v1.2.3" || release.Commit != "abcdef123456" ||
		!release.BuiltAt.Equal(time.Date(2026, 7, 16, 5, 30, 0, 0, time.UTC)) ||
		release.BuiltAt.Location() != time.UTC {
		t.Fatalf("release build info = %+v", release)
	}

	development, err := parseBuildInfo("dev", "unknown", "")
	if err != nil || !development.BuiltAt.IsZero() {
		t.Fatalf("parseBuildInfo(development) = (%+v, %v)", development, err)
	}
}

func TestParseBuildInfoRejectsMalformedMetadata(t *testing.T) {
	t.Parallel()

	for _, input := range [][3]string{
		{"", "abcdef", ""},
		{"   ", "abcdef", ""},
		{"v1.0.0", "", ""},
		{"v1.0.0", "   ", ""},
		{strings.Repeat("v", 257), "abcdef", ""},
		{"v1.0.0", strings.Repeat("a", 257), ""},
		{"v1.0.0", "abcdef", "not-a-time"},
	} {
		info, err := parseBuildInfo(input[0], input[1], input[2])
		if info != (apihttp.BuildInfo{}) || !errors.Is(err, ErrInvalidBuildInfo) {
			t.Fatalf("parseBuildInfo(%q, %q, %q) = (%+v, %v)", input[0], input[1], input[2], info, err)
		}
	}
}
