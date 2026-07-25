//go:build go1.1

package consumer

import "resourceapi"

func buildTaggedCleanup() {
	_, _, _ = resourceapi.Open() // want `lifecycle/cleanup-ownership: cleanup result 2 from resourceapi.Open is discarded`
}
