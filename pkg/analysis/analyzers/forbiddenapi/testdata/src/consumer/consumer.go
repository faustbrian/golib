package consumer

import legacyalias "legacy"

func Calls(client legacyalias.Client) {
	legacyalias.Old()           // want `api/forbidden-call: legacy.Old is forbidden; use modern.New`
	legacyalias.Generic[int](1) // want `api/forbidden-call: legacy.Generic is forbidden; use modern.Generic`
	client.Call()               // want `api/forbidden-call: legacy.Client.Call is forbidden; use ports.Client.Call`
	client.Current()
	legacyalias.Current()
	legacyalias.GenericPair[int, string](1, "one")
	callback := func() {}
	callback()
	func() {}()
}

type localClient struct{}

func (localClient) Call() {}

func NearMiss(client localClient) {
	client.Call()
}
