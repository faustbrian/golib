package approved

import (
	"ioapi"
	"sync"
)

func call() {
	var mutex sync.Mutex
	mutex.Lock()
	ioapi.Call()
	mutex.Unlock()
}
