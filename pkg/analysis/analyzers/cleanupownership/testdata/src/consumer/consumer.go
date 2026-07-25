package consumer

import resources "resourceapi"

func assignments() {
	resource, cleanup, err := resources.Open()
	_, _, _ = resource, cleanup, err

	resource, _, err = resources.Open() // want `lifecycle/cleanup-ownership: cleanup result 2 from resourceapi.Open is discarded`
	_, _, _ = resource, cleanup, err

	_, cleanup, _ = resources.Open()
	_ = cleanup

	resources.Open() // want `lifecycle/cleanup-ownership: cleanup result 2 from resourceapi.Open is discarded`
	resources.Other()
	go resources.Open()    // want `lifecycle/cleanup-ownership: cleanup result 2 from resourceapi.Open is discarded`
	defer resources.Open() // want `lifecycle/cleanup-ownership: cleanup result 2 from resourceapi.Open is discarded`

	_, _, _ = resources.Other()
}

func declarations() {
	var _, _, _ = resources.Open() // want `lifecycle/cleanup-ownership: cleanup result 2 from resourceapi.Open is discarded`
}

func methodsAndGenerics() {
	manager := resources.Manager{}
	_, _, _ = manager.Open()              // want `lifecycle/cleanup-ownership: cleanup result 2 from resourceapi.Manager.Open is discarded`
	_, _, _ = resources.OpenFor[string]() // want `lifecycle/cleanup-ownership: cleanup result 2 from resourceapi.OpenFor is discarded`
	_, _, _ = resources.OpenPair[string, int]()
}

func forward() (*resources.Resource, func(), error) {
	return resources.Open()
}
