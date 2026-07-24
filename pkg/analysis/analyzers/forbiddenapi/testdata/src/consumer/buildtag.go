//go:build go1.1

package consumer

import "legacy"

func BuildTaggedForbidden() {
	legacy.Old() // want `api/forbidden-call: legacy.Old is forbidden; use modern.New`
}
