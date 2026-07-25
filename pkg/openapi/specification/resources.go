// Package specification embeds the exact specification resources pinned by
// this module.
package specification

import (
	"embed"
	"fmt"
)

// resources contains the immutable files recorded by manifest.json.
//
//go:embed schemas registries/iana
var resources embed.FS

// Read returns a caller-owned copy of one pinned resource.
func Read(name string) ([]byte, error) {
	value, err := resources.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read pinned specification resource %q: %w", name, err)
	}
	return append([]byte(nil), value...), nil
}
