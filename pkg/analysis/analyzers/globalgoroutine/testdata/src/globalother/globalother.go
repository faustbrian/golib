package globalother

func work() {}

var callback = func() {
	go work()
}
