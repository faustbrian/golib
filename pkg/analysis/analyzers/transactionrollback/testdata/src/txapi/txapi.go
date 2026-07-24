package txapi

type Tx struct{}

func (*Tx) Rollback() error { return nil }
func (*Tx) Commit() error   { return nil }

func Begin() (*Tx, error) { return &Tx{}, nil }

func BeginFor[T any]() (*Tx, error) { return &Tx{}, nil }

func BeginPair[A, B any]() (*Tx, error) { return &Tx{}, nil }

func BadResult() (*Tx, error) { return &Tx{}, nil }

type DB struct{}

func (DB) Begin() (*Tx, error) { return &Tx{}, nil }
