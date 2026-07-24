package consumer

import . "resourceapi"

func dotImport() {
	_, _, _ = Open() // want `lifecycle/cleanup-ownership: cleanup result 2 from resourceapi.Open is discarded`
}
