package main

import (
	"errors"
	"strings"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
)

// ErrInvalidBuildInfo reports malformed release metadata.
var ErrInvalidBuildInfo = errors.New("queue-control-plane: invalid build metadata")

func parseBuildInfo(version, commit, builtAt string) (apihttp.BuildInfo, error) {
	version = strings.TrimSpace(version)
	commit = strings.TrimSpace(commit)
	if version == "" || commit == "" ||
		len(version) > controlplane.MaxIdentityBytes ||
		len(commit) > controlplane.MaxIdentityBytes {
		return apihttp.BuildInfo{}, ErrInvalidBuildInfo
	}

	info := apihttp.BuildInfo{Version: version, Commit: commit}
	if builtAt == "" {
		return info, nil
	}
	parsed, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		return apihttp.BuildInfo{}, ErrInvalidBuildInfo
	}
	info.BuiltAt = parsed.UTC()

	return info, nil
}
