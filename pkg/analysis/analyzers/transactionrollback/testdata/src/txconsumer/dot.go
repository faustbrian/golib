package txconsumer

import . "txapi"

func DotImport() {
	tx, _ := Begin() // want `lifecycle/transaction-rollback: transaction from txapi.Begin must immediately establish deferred Rollback ownership`
	_ = tx
}
