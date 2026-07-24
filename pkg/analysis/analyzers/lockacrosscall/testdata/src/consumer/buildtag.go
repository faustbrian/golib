//go:build darwin || linux || windows

package consumer

import (
	"ioapi"
	"sync"
)

func buildTagged() {
	var mutex sync.Mutex
	mutex.Lock()
	ioapi.Call() // want `lifecycle/lock-across-call: ioapi.Call is called while a lock is definitely held`
	mutex.Unlock()
}
