package fanoutunconfigured

func Fanout(values []int) {
	for range values {
		go func() {}()
	}
}
