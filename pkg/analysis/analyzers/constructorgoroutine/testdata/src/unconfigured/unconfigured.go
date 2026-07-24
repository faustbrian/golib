package unconfigured

func work() {}

func New() {
	go work()
}
