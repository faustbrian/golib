package consumer

import . "legacy"

func DotImport() {
	Old() // want `api/forbidden-call: legacy.Old is forbidden; use modern.New`
}
