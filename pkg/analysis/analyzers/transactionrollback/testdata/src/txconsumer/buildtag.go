//go:build !windows

package txconsumer

import "txapi"

func Platform() {
	tx, _ := txapi.Begin() // want `lifecycle/transaction-rollback: transaction from txapi.Begin must immediately establish deferred Rollback ownership`
	_ = tx
}
