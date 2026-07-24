//go:build go1.1

package contextstore

import "context"

type buildTagged struct {
	ctx context.Context // want `context/no-stored-context: struct field stores a context lifecycle`
}
