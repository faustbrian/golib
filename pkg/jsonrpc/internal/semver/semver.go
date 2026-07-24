package semver

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

var tagPattern = regexp.MustCompile(
	`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
		`(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)` +
		`(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?` +
		`(\+([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$`,
)

var stableTagPattern = regexp.MustCompile(
	`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`,
)

func ValidateTag(tag string) error {
	if !tagPattern.MatchString(tag) {
		return errors.New("tag must be a v-prefixed semantic version")
	}
	return nil
}

func NextStable(tag, part string) (string, error) {
	matches := stableTagPattern.FindStringSubmatch(tag)
	if matches == nil {
		return "", errors.New("current tag must be a stable semantic version")
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])
	switch part {
	case "patch":
		patch++
	case "minor":
		minor++
		patch = 0
	case "major":
		major++
		minor, patch = 0, 0
	default:
		return "", errors.New("release part must be patch, minor, or major")
	}
	return fmt.Sprintf("v%d.%d.%d", major, minor, patch), nil
}
