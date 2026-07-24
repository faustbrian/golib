package consumer

import (
	. "ioapi"
	"sync"
)

func dotImport() {
	var mutex sync.Mutex
	mutex.Lock()
	Call() // want `lifecycle/lock-across-call: ioapi.Call is called while a lock is definitely held`
	mutex.Unlock()
}
