package txconsumer

import api "txapi"

func Missing() error {
	tx, err := api.Begin() // want `lifecycle/transaction-rollback: transaction from txapi.Begin must immediately establish deferred Rollback ownership`
	if err != nil {
		return err
	}
	return tx.Commit()
}

func Good() error {
	tx, err := api.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return tx.Commit()
}

func IgnoredError() {
	tx, _ := api.Begin()
	defer tx.Rollback()
}

func Transfer() (*api.Tx, error) {
	tx, err := api.Begin()
	if err != nil {
		return nil, err
	}
	return tx, nil
}

func Generic() {
	tx, _ := api.BeginFor[string]() // want `lifecycle/transaction-rollback: transaction from txapi.BeginFor must immediately establish deferred Rollback ownership`
	_ = tx
	tx, _ = api.BeginPair[string, int]() // want `lifecycle/transaction-rollback: transaction from txapi.BeginPair must immediately establish deferred Rollback ownership`
}

func Method() {
	tx, _ := (api.DB{}).Begin() // want `lifecycle/transaction-rollback: transaction from txapi.DB.Begin must immediately establish deferred Rollback ownership`
	_ = tx
}

func ConditionalDefer() error {
	tx, err := api.Begin() // want `lifecycle/transaction-rollback: transaction from txapi.Begin must immediately establish deferred Rollback ownership`
	if err != nil {
		return err
	}
	if tx != nil {
		defer tx.Rollback()
	}
	return nil
}

func NonTerminatingGuard() {
	tx, err := api.Begin() // want `lifecycle/transaction-rollback: transaction from txapi.Begin must immediately establish deferred Rollback ownership`
	if err != nil {
		err = nil
	}
	defer tx.Rollback()
}

func HelperDefer() {
	tx, _ := api.Begin() // want `lifecycle/transaction-rollback: transaction from txapi.Begin must immediately establish deferred Rollback ownership`
	defer rollback(tx)
}

func DiscardedTransaction() {
	_, _ = api.Begin() // want `lifecycle/transaction-rollback: transaction from txapi.Begin must immediately establish deferred Rollback ownership`
}

func rollback(tx *api.Tx) { _ = tx.Rollback() }

func DirectReturn() (*api.Tx, error) { return api.Begin() }

func Reassignment() error {
	var tx *api.Tx
	var err error
	tx, err = api.Begin()
	if (nil) != (err) {
		return err
	}
	defer tx.Rollback()
	return nil
}

func WrongGuard() error {
	tx, err := api.Begin() // want `lifecycle/transaction-rollback: transaction from txapi.Begin must immediately establish deferred Rollback ownership`
	if err == nil {
		return nil
	}
	defer tx.Rollback()
	return err
}

type holder struct{ tx *api.Tx }

func UnknownTransfer() {
	var value holder
	value.tx, _ = api.Begin()
}

func UnrelatedCalls() {
	values := make([]int, 0)
	value := func() int { return 0 }()
	_, _ = values, value
}

func ContinueGuard() {
	for {
		tx, err := api.Begin()
		if err != nil {
			continue
		}
		defer tx.Rollback()
		return
	}
}

func BreakGuard() {
	for {
		tx, err := api.Begin()
		if err != nil {
			break
		}
		defer tx.Rollback()
		return
	}
}

func GotoGuard() {
	tx, err := api.Begin()
	if err != nil {
		goto done
	}
	defer tx.Rollback()
done:
	return
}

var lastTx *api.Tx
var lastErr error

func AssignmentLast() {
	lastTx, lastErr = api.Begin() // want `lifecycle/transaction-rollback: transaction from txapi.Begin must immediately establish deferred Rollback ownership`
}

func GuardLast() {
	lastTx, lastErr = api.Begin() // want `lifecycle/transaction-rollback: transaction from txapi.Begin must immediately establish deferred Rollback ownership`
	if lastErr != nil {
		return
	}
}

func OwnershipReturnExpression() (*api.Tx, *api.Tx) {
	tx, _ := api.Begin()
	return (*api.Tx)(nil), tx
}

func OutOfRangePolicy() {
	tx, err := api.BadResult()
	_, _ = tx, err
}
