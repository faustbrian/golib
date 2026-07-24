package sample

func work() {}

var started = func() int {
	go work()
	return 1
}()

func init() {}
