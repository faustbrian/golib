package txunconfigured

import "txapi"

func Unconfigured() {
	tx, _ := txapi.Begin()
	_ = tx
}
