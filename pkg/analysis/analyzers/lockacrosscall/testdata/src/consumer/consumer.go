package consumer

import (
	api "ioapi"
	"sync"
)

type service struct {
	mu sync.Mutex
	rw sync.RWMutex
}

func direct(value *service) {
	value.mu.Lock()
	api.Call()            // want `lifecycle/lock-across-call: ioapi.Call is called while a lock is definitely held`
	api.CallFor[string]() // want `lifecycle/lock-across-call: ioapi.CallFor is called while a lock is definitely held`
	value.mu.Unlock()
}

func deferred(value *service) {
	value.rw.RLock()
	defer value.rw.RUnlock()
	api.Client{}.Call() // want `lifecycle/lock-across-call: ioapi.Client.Call is called while a lock is definitely held`
}

func nestedBranch(value *service, condition bool) {
	value.mu.Lock()
	if condition {
		api.Call() // want `lifecycle/lock-across-call: ioapi.Call is called while a lock is definitely held`
	}
	api.Call() // want `lifecycle/lock-across-call: ioapi.Call is called while a lock is definitely held`
	value.mu.Unlock()
}

func branchDependent(value *service, condition bool) {
	if condition {
		value.mu.Lock()
	}
	api.Call()
}

func branchMayUnlock(value *service, condition bool) {
	value.mu.Lock()
	if condition {
		value.mu.Unlock()
	}
	api.Call()
}

func distinctLocks(first, second *service) {
	first.mu.Lock()
	second.mu.Unlock()
	api.Call() // want `lifecycle/lock-across-call: ioapi.Call is called while a lock is definitely held`
	first.mu.Unlock()
}

func accepted(value *service) {
	api.Call()
	value.mu.Lock()
	value.mu.Unlock()
	api.Call()
	api.Other()
}

func scheduled(value *service) {
	value.mu.Lock()
	api.Other()
	defer api.Call()
	go api.Call()
	value.mu.Unlock()
}

type nested struct {
	service service
}

func unsupportedIdentity(value *nested) {
	value.service.mu.Lock()
	api.Call()
	value.service.mu.Unlock()
}

func callbackLiteral(value *service) func() {
	value.mu.Lock()
	callback := func() { api.Call() }
	value.mu.Unlock()
	return callback
}
