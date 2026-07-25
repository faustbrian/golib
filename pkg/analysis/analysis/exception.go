package analysis

import (
	"fmt"
	"path"
	"strings"
	"time"
)

// ApplyPolicyExceptions filters exact reviewed exceptions and rejects stale
// or expired configuration.
func ApplyPolicyExceptions(
	root string,
	diagnostics []Diagnostic,
	exceptions []PolicyException,
	now time.Time,
) ([]Diagnostic, []PolicyException, error) {
	if len(exceptions) == 0 {
		return diagnostics, nil, nil
	}
	inventory := append([]PolicyException(nil), exceptions...)
	for index := range inventory {
		if inventory[index].Expires == "" {
			continue
		}
		expires, err := time.Parse("2006-01-02", inventory[index].Expires)
		if err != nil {
			return nil, inventory, fmt.Errorf("parse policy exception expiry: %w", err)
		}
		if expires.Before(midnightUTC(now)) {
			return nil, inventory, fmt.Errorf(
				"policy exception for %s expired on %s",
				inventory[index].Rule,
				inventory[index].Expires,
			)
		}
	}

	remaining := make([]Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		filename, err := reportPath(root, diagnostic.Filename)
		if err != nil {
			return nil, inventory, err
		}
		matched := false
		for index := range inventory {
			exception := &inventory[index]
			if exception.Rule != diagnostic.Rule ||
				exception.Package != diagnostic.Package ||
				(exception.Path != "" && exception.Path != filename) {
				continue
			}
			exception.Used = true
			matched = true
			break
		}
		if !matched {
			remaining = append(remaining, diagnostic)
		}
	}
	for _, exception := range inventory {
		if !exception.Used {
			return nil, inventory, fmt.Errorf(
				"stale policy exception for %s in %s",
				exception.Rule,
				exception.Package,
			)
		}
	}

	return remaining, inventory, nil
}

func validExceptionPackage(value string) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		value != "." && !path.IsAbs(value) &&
		path.Clean(value) == value && !strings.Contains(value, "...") &&
		!strings.ContainsAny(value, "*\\")
}

func validExceptionPath(value string) bool {
	return value != "" && value != "." && !path.IsAbs(value) &&
		path.Clean(value) == value &&
		value != ".." && !strings.HasPrefix(value, "../") &&
		!strings.ContainsAny(value, ":\\")
}
